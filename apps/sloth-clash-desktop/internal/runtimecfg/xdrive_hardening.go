package runtimecfg

import (
	"net/url"
	"regexp"
	"strings"

	"gopkg.in/yaml.v3"
)

// XDrive (core patch 0004, VLESS over remote storage) brings two config
// surfaces a subscription must not get on a desktop where the core can run under
// the privileged helper service:
//
//   - `listeners[].xdrive-config`: an XDrive inbound binds no socket but polls
//     cloud storage, a reverse tunnel into this machine that no bind address
//     contains. Dropped.
//   - `xdrive-opts.service: local`: a storage folder whose path comes from the
//     subscription. The core refuses it in shipped builds (patch 0016, which
//     also covers proxy-providers the core fetches itself); here such a node
//     keeps its name, so groups and rules that use it stay valid, and becomes
//     `type: reject` — it shows as disabled instead of failing on every dial.
//
// Both are re-applied after the profile's JavaScript override (see
// reassertCriticalRuntimeInvariants): a script must not be able to win.

// mihomoKeyMatches reports whether mihomo's structure decoder (common/structure
// with its default key replacer) could read key as field: an exact match, or a
// case-insensitive one after `_` -> `-`. Security checks must look keys up this
// way, or a subscription hides a field from us by spelling it `Xdrive_Opts`.
func mihomoKeyMatches(key, field string) bool {
	if key == field {
		return true
	}
	return strings.EqualFold(strings.ReplaceAll(key, "_", "-"), strings.ReplaceAll(field, "_", "-"))
}

// mihomoValuesOf returns every value in m whose key the decoder could read as field.
func mihomoValuesOf(m map[string]any, field string) []any {
	var out []any
	for k, v := range m {
		if mihomoKeyMatches(k, field) {
			out = append(out, v)
		}
	}
	return out
}

// mihomoStringOf returns the value the decoder reads for field: the exact key
// if present, else any matching spelling.
func mihomoStringOf(m map[string]any, field string) string {
	if v, ok := m[field].(string); ok {
		return v
	}
	for _, v := range mihomoValuesOf(m, field) {
		if s, ok := v.(string); ok {
			return s
		}
	}
	return ""
}

// applyXDriveHardening drops XDrive listeners and turns local-storage XDrive
// nodes (top-level proxies and inline proxy-provider payloads) into REJECT.
func applyXDriveHardening(m map[string]any) {
	listeners := dropXDriveListeners(m)
	nodes := 0
	if proxies, ok := m["proxies"].([]any); ok {
		nodes += neutralizeLocalXDriveProxies(proxies)
	}
	if providers, ok := m["proxy-providers"].(map[string]any); ok {
		for _, raw := range providers {
			pm, ok := raw.(map[string]any)
			if !ok {
				continue
			}
			// The provider decoder matches keys like the proxy decoder does, so
			// every key it could read as `payload` is checked.
			for k, v := range pm {
				if !mihomoKeyMatches(k, "payload") {
					continue
				}
				if payload, ok := v.([]any); ok {
					nodes += neutralizeLocalXDriveProxies(payload)
				}
			}
		}
	}
	if listeners > 0 || nodes > 0 {
		debugLog("config", "XDRIVE-1", "xdrive_hardening.go:applyXDriveHardening",
			"neutralised XDrive config surface",
			map[string]any{"droppedListeners": listeners, "localNodesRejected": nodes})
	}
}

func dropXDriveListeners(m map[string]any) int {
	listeners, ok := m["listeners"].([]any)
	if !ok {
		return 0
	}
	kept := make([]any, 0, len(listeners))
	for _, it := range listeners {
		if lm, ok := it.(map[string]any); ok && len(mihomoValuesOf(lm, "xdrive-config")) > 0 {
			continue
		}
		kept = append(kept, it)
	}
	dropped := len(listeners) - len(kept)
	if dropped == 0 {
		return 0
	}
	if len(kept) == 0 {
		delete(m, "listeners")
	} else {
		m["listeners"] = kept
	}
	return dropped
}

// neutralizeLocalXDriveProxies replaces, in place, every local-storage XDrive
// proxy with a REJECT proxy of the same name. Returns how many it replaced.
func neutralizeLocalXDriveProxies(proxies []any) int {
	n := 0
	for i, it := range proxies {
		pm, ok := it.(map[string]any)
		if !ok || !isLocalXDriveProxy(pm) {
			continue
		}
		proxies[i] = map[string]any{"name": mihomoStringOf(pm, "name"), "type": "reject"}
		n++
	}
	return n
}

// isLocalXDriveProxy over-matches on purpose: any spelling the decoder could
// pick counts.
func isLocalXDriveProxy(pm map[string]any) bool {
	xdrive := false
	for _, v := range mihomoValuesOf(pm, "network") {
		if s, ok := v.(string); ok && strings.EqualFold(strings.TrimSpace(s), "xdrive") {
			xdrive = true
		}
	}
	if !xdrive {
		return false
	}
	for _, v := range mihomoValuesOf(pm, "xdrive-opts") {
		opts, ok := v.(map[string]any)
		if !ok {
			continue
		}
		for _, s := range mihomoValuesOf(opts, "service") {
			if str, ok := s.(string); ok && strings.EqualFold(strings.TrimSpace(str), "local") {
				return true
			}
		}
	}
	return false
}

// xdriveRedacted replaces an XDrive credential in read-only views.
const xdriveRedacted = "[redacted]"

// redactXDriveCredentials returns a copy of a config map with XDrive storage
// credentials hidden: `xdrive-opts.secrets` (OAuth client id / secret / refresh
// token), `xdrive-opts.template.auth`, and in the template operations
// (put/get/list/delete) credential-looking headers, query parameters and
// bodies. They grant access to the operator's storage account, not to one node.
// For read-only views only: editors must keep the real values, or a save would
// write the placeholders back. m is never modified; changed is false (and m
// itself returned) when it carries no XDrive credentials.
func redactXDriveCredentials(m map[string]any) (out map[string]any, changed bool) {
	out = m
	clone := func() {
		if !changed {
			out = shallowCopyMap(m)
			changed = true
		}
	}
	if proxies, ok := m["proxies"].([]any); ok {
		if red, changed := redactXDriveProxyList(proxies); changed {
			clone()
			out["proxies"] = red
		}
	}
	if providers, ok := m["proxy-providers"].(map[string]any); ok {
		var newProviders map[string]any
		for name, raw := range providers {
			pm, ok := raw.(map[string]any)
			if !ok {
				continue
			}
			var newProvider map[string]any
			for k, v := range pm {
				payload, ok := v.([]any)
				if !ok || !mihomoKeyMatches(k, "payload") {
					continue
				}
				if red, changed := redactXDriveProxyList(payload); changed {
					if newProvider == nil {
						newProvider = shallowCopyMap(pm)
					}
					newProvider[k] = red
				}
			}
			if newProvider != nil {
				if newProviders == nil {
					newProviders = shallowCopyMap(providers)
				}
				newProviders[name] = newProvider
			}
		}
		if newProviders != nil {
			clone()
			out["proxy-providers"] = newProviders
		}
	}
	return out, changed
}

// RedactXDriveCredentialsYAML is redactXDriveCredentials for a generated
// config document. Text without any XDrive proxy comes back byte for byte; a
// redacted one is re-encoded with the runtime config marshaller, so its layout
// matches what the app writes.
func RedactXDriveCredentialsYAML(text string) string {
	if !strings.Contains(strings.ToLower(text), "xdrive") {
		return text
	}
	var m map[string]any
	if err := yaml.Unmarshal([]byte(text), &m); err != nil || m == nil {
		return text
	}
	red, changed := redactXDriveCredentials(m)
	if !changed {
		return text
	}
	out, err := MarshalYAML(red)
	if err != nil {
		return text
	}
	return string(out)
}

func redactXDriveProxyList(proxies []any) ([]any, bool) {
	var out []any
	for i, it := range proxies {
		pm, ok := it.(map[string]any)
		if !ok {
			continue
		}
		red, changed := redactXDriveProxy(pm)
		if !changed {
			continue
		}
		if out == nil {
			out = append([]any(nil), proxies...)
		}
		out[i] = red
	}
	return out, out != nil
}

func redactXDriveProxy(pm map[string]any) (map[string]any, bool) {
	var out map[string]any
	for k, v := range pm {
		opts, ok := v.(map[string]any)
		if !ok || !mihomoKeyMatches(k, "xdrive-opts") {
			continue
		}
		red, changed := redactXDriveOpts(opts)
		if !changed {
			continue
		}
		if out == nil {
			out = shallowCopyMap(pm)
		}
		out[k] = red
	}
	return out, out != nil
}

func redactXDriveOpts(opts map[string]any) (map[string]any, bool) {
	out := shallowCopyMap(opts)
	changed := false
	for k, v := range opts {
		switch {
		case mihomoKeyMatches(k, "secrets"):
			if list, ok := v.([]any); ok {
				red := make([]any, len(list))
				for i := range red {
					red[i] = xdriveRedacted
				}
				out[k] = red
				changed = true
			}
		case mihomoKeyMatches(k, "template"):
			tm, ok := v.(map[string]any)
			if !ok {
				continue
			}
			tOut := shallowCopyMap(tm)
			tChanged := false
			for tk, tv := range tm {
				if mihomoKeyMatches(tk, "auth") {
					tOut[tk] = redactLeaves(tv)
					tChanged = true
					continue
				}
				for _, op := range []string{"put", "get", "list", "delete"} {
					if !mihomoKeyMatches(tk, op) {
						continue
					}
					if om, ok := tv.(map[string]any); ok {
						if red, opChanged := redactXDriveTemplateOp(om); opChanged {
							tOut[tk] = red
							tChanged = true
						}
					}
				}
			}
			if tChanged {
				out[k] = tOut
				changed = true
			}
		}
	}
	return out, changed
}

// redactXDriveTemplateOp hides credential headers, credential query values and
// any body that names a credential-looking field (the body format is free, so
// it is hidden whole rather than picked apart).
func redactXDriveTemplateOp(op map[string]any) (map[string]any, bool) {
	out := shallowCopyMap(op)
	changed := false
	for k, v := range op {
		switch {
		case mihomoKeyMatches(k, "headers"):
			hm, ok := v.(map[string]any)
			if !ok {
				continue
			}
			hOut := shallowCopyMap(hm)
			hChanged := false
			for name, hv := range hm {
				if hv != nil && isCredentialName(name) {
					hOut[name] = xdriveRedacted
					hChanged = true
				}
			}
			if hChanged {
				out[k] = hOut
				changed = true
			}
		case mihomoKeyMatches(k, "url"):
			if s, ok := v.(string); ok {
				if red := RedactCredentialQuery(s); red != s {
					out[k] = red
					changed = true
				}
			}
		case mihomoKeyMatches(k, "body"):
			if s, ok := v.(string); ok && bodyNamesCredential(s) {
				out[k] = xdriveRedacted
				changed = true
			}
		}
	}
	return out, changed
}

// redactLeaves hides every scalar under an auth block but keeps its shape and
// the auth scheme (`type: static`) readable.
func redactLeaves(v any) any {
	switch t := v.(type) {
	case nil:
		return nil
	case map[string]any:
		out := make(map[string]any, len(t))
		for k, val := range t {
			if s, ok := val.(string); ok && mihomoKeyMatches(k, "type") {
				out[k] = s
				continue
			}
			out[k] = redactLeaves(val)
		}
		return out
	case []any:
		out := make([]any, len(t))
		for i, val := range t {
			out[i] = redactLeaves(val)
		}
		return out
	default:
		return xdriveRedacted
	}
}

var (
	credentialNames = map[string]bool{"authorization": true, "proxy-authorization": true, "auth": true, "sig": true, "signature": true}
	credentialParts = []string{"token", "secret", "key", "cookie", "password", "passwd", "signature", "credential"}
	// A form `name=`, JSON `"name":` or any other `name:` / `name=` in a body.
	bodyFieldRe = regexp.MustCompile(`([A-Za-z0-9_.%-]+)"?\s*[:=]`)
	// A query parameter inside a URL: separator, name, `=`, value.
	queryParamRe = regexp.MustCompile(`([?&])([^=&#\s"']+)=([^&#\s"']*)`)
)

// isCredentialName reports header / query / field names that carry
// credentials: auth headers, cookies, and *token* / *secret* / *key* / ….
func isCredentialName(name string) bool {
	n := strings.ToLower(strings.TrimSpace(name))
	if credentialNames[n] {
		return true
	}
	for _, part := range credentialParts {
		if strings.Contains(n, part) {
			return true
		}
	}
	return false
}

func bodyNamesCredential(body string) bool {
	for _, mm := range bodyFieldRe.FindAllStringSubmatch(body, -1) {
		if isCredentialName(queryUnescapeLoose(mm[1])) {
			return true
		}
	}
	return false
}

// RedactCredentialQuery replaces the values of credential-looking query
// parameters in every URL of s; everything else is kept as written. Used for
// XDrive template URLs and for log text shown in the UI (the core logs
// transport errors with the full request URL).
func RedactCredentialQuery(s string) string {
	if !strings.ContainsAny(s, "?&") {
		return s
	}
	return queryParamRe.ReplaceAllStringFunc(s, func(match string) string {
		mm := queryParamRe.FindStringSubmatch(match)
		if mm == nil || !isCredentialName(queryUnescapeLoose(mm[2])) || mm[3] == "" {
			return match
		}
		return mm[1] + mm[2] + "=" + xdriveRedacted
	})
}

// queryUnescapeLoose percent-decodes a name for the credential check and falls
// back to the raw text when it is not valid escaping.
func queryUnescapeLoose(s string) string {
	if u, err := url.QueryUnescape(s); err == nil {
		return u
	}
	return s
}

func shallowCopyMap(m map[string]any) map[string]any {
	out := make(map[string]any, len(m))
	for k, v := range m {
		out[k] = v
	}
	return out
}
