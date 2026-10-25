package runtimecfg

import "strconv"

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
	// pipelineBodyTooLarge — no cached body and the fetched subscription
	// exceeds its size cap. Hard failure: the bare fallback would hide why the
	// profile lost its groups and rules, and retrying cannot help.
	pipelineBodyTooLarge
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
	case pipelineBodyTooLarge:
		return "body_too_large"
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
