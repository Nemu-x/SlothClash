package main

import (
	"fmt"
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
//                              pipeline helpers; internal/runtimecfg traces
//                              through it too (SetLogSinks, runtime_config.go).
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
