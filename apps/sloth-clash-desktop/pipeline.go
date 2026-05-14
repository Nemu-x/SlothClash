package main

import (
	"fmt"
	"strconv"
	"strings"
	"time"
)

// Pipeline trace — a single, unified way to record what the runtime
// configuration pipeline did. Replaces ad-hoc `debugLog(...)` /
// `appendRuntimeDiag(...)` / `log.Printf(...)` sprinkles with a structured
// record so we can reconstruct a Connect / SetMode / SubscriptionRefresh
// timeline from the JSON debug log alone.
//
// Two flavours:
//
//   - traceEvent(...)        — package-level, no App handle. Used by stateless
//                              pipeline helpers (subscription_fetch.go etc.).
//                              Writes to the debug JSON log only.
//   - (a *App).traceEvent... — App-method version. Same JSON event plus a
//                              short entry into the user-visible runtime
//                              diagnostics ring (Diagnostics export).
//
// Stage names are dotted. Convention:
//
//   pipeline.connect.start
//   pipeline.connect.done
//   pipeline.config.write_yaml
//   pipeline.config.reload
//   pipeline.subscription.cache_hit
//   pipeline.subscription.fetch
//   pipeline.subscription.refresh_bg
//   pipeline.reconnect.trigger
//
// Outcome values are one of:
//
//   ok, fail, skip, cancelled, cache_hit, cache_miss

// pipelineOutcome is the explicit return value of the subscription pipeline.
// Replaces the prior `(bool, error)` returns that conflated "not a full
// profile" with "fetch failed" with "merge template malformed".
type pipelineOutcome int

const (
	// pipelineOK — runtime config.yaml was written from the subscription body.
	pipelineOK pipelineOutcome = iota
	// pipelineCacheMissNoNet — no cached body on disk and synchronous fetch
	// failed (network down, 4xx/5xx). Caller should fall back to the bare
	// provider profile.
	pipelineCacheMissNoNet
	// pipelineNotFullProfile — body parsed, but it is not a Verge-style full
	// config (no rules / groups / providers / dns / tun / sniffer / script).
	// Caller should fall back to the bare provider profile.
	pipelineNotFullProfile
	// pipelineParseFail — body could not be parsed as YAML or base64+YAML.
	pipelineParseFail
	// pipelineTemplateFail — one of the merge templates was malformed.
	pipelineTemplateFail
	// pipelineValidateFail — finalize stage rejected the merged config
	// (e.g. dangling proxy-group reference, missing policy).
	pipelineValidateFail
	// pipelineWriteFail — config.yaml could not be written to disk.
	pipelineWriteFail
)

func (o pipelineOutcome) String() string {
	switch o {
	case pipelineOK:
		return "ok"
	case pipelineCacheMissNoNet:
		return "cache_miss_no_net"
	case pipelineNotFullProfile:
		return "not_full_profile"
	case pipelineParseFail:
		return "parse_fail"
	case pipelineTemplateFail:
		return "template_fail"
	case pipelineValidateFail:
		return "validate_fail"
	case pipelineWriteFail:
		return "write_fail"
	default:
		return "unknown(" + strconv.Itoa(int(o)) + ")"
	}
}

// useBareFallback reports whether this outcome should make Connect fall back
// to writing a bare `proxy-provider: sub1` profile instead of a full one.
// Only the two "soft" outcomes — no cache + no network, or body is a partial
// (proxies-only) subscription — qualify. Hard failures (parse, template,
// validate, write) propagate as errors so the user sees the problem instead
// of silently routing through the fallback profile.
func (o pipelineOutcome) useBareFallback() bool {
	return o == pipelineCacheMissNoNet || o == pipelineNotFullProfile
}

// traceEvent records one stage of the pipeline to the debug JSON log.
// Fields are merged into the payload's `data` field so a single grep can
// reconstruct the full timeline of a Connect.
func traceEvent(stage, outcome string, dur time.Duration, fields map[string]any) {
	data := make(map[string]any, len(fields)+2)
	data["outcome"] = outcome
	if dur > 0 {
		data["dur_ms"] = dur.Milliseconds()
	}
	for k, v := range fields {
		data[k] = v
	}
	debugLog("pipeline", "P1", stage, outcome, data)
}

// traceEvent (method version) — same as traceEvent + also appends a short
// summary into the user-visible runtime diagnostics ring exposed via
// GetRuntimeDiagEvents and the Diagnostics export.
func (a *App) traceEvent(stage, outcome string, dur time.Duration, fields map[string]any) {
	traceEvent(stage, outcome, dur, fields)
	if a == nil {
		return
	}
	a.appendRuntimeDiag(stage, traceShortMessage(outcome, dur, fields))
}

// traceShortMessage builds the human-readable one-liner for the diag ring.
// Keep it terse — the ring shows ~220 lines on Diagnostics export so we
// don't want a wall of JSON per event.
func traceShortMessage(outcome string, dur time.Duration, fields map[string]any) string {
	parts := make([]string, 0, 4)
	parts = append(parts, outcome)
	if dur > 0 {
		parts = append(parts, fmt.Sprintf("%dms", dur.Milliseconds()))
	}
	if e, ok := fields["error"].(string); ok && strings.TrimSpace(e) != "" {
		parts = append(parts, "err="+e)
	}
	if code, ok := fields["http_status"].(int); ok && code > 0 {
		parts = append(parts, fmt.Sprintf("http=%d", code))
	}
	return strings.Join(parts, " ")
}
