package main

import (
	"fmt"
	"strconv"
	"strings"
)

// RealitySettings is the REALITY handshake policy. It reaches the core as two
// top-level config keys read by core patch 0003 (docs/core-patches.md):
//
//	reality-mlkem: auto | on | off      — offer the X25519MLKEM768 key share
//	reality-client-version: "x.y.z"     — client version Xray minClientVer/maxClientVer gate on
//
// Why it exists: Xray servers before 2025 drop a hybrid ClientHello, from
// 26.9.8 on they reject a classic one, and none announce which they are. Auto
// lets the core learn per server; Always/Never are manual overrides for a
// network where learning is not wanted. An unpatched core ignores both keys.
type RealitySettings struct {
	MLKEM         string `json:"mlkem,omitempty"`         // "" = auto; auto|always|never
	ClientVersion string `json:"clientVersion,omitempty"` // "" = subscription value, else core default
}

// normalizeRealityMLKEM returns auto|always|never; anything else is auto.
func normalizeRealityMLKEM(v string) string {
	switch strings.ToLower(strings.TrimSpace(v)) {
	case "always", "on":
		return "always"
	case "never", "off":
		return "never"
	default:
		return "auto"
	}
}

// coreRealityMLKEM maps the app value to the core key value.
func coreRealityMLKEM(v string) string {
	switch normalizeRealityMLKEM(v) {
	case "always":
		return "on"
	case "never":
		return "off"
	}
	return "auto"
}

// parseRealityClientVersion validates "x.y.z" with each part 0..255 (the core
// stores it in three bytes of the ClientHello) and returns it canonicalised.
// "" is valid and means "core default".
func parseRealityClientVersion(v string) (string, error) {
	v = strings.TrimPrefix(strings.TrimSpace(v), "v")
	if v == "" {
		return "", nil
	}
	parts := strings.Split(v, ".")
	if len(parts) != 3 {
		return "", fmt.Errorf("REALITY client version %q: expected x.y.z", v)
	}
	for i, p := range parts {
		n, err := strconv.ParseUint(p, 10, 8)
		if err != nil {
			return "", fmt.Errorf("REALITY client version %q: each part must be a number from 0 to 255", v)
		}
		parts[i] = strconv.FormatUint(n, 10)
	}
	return strings.Join(parts, "."), nil
}

// SetRealitySettings stores the policy and reloads the running core, so new
// connections use it right away (open connections keep their handshake).
func (a *App) SetRealitySettings(next RealitySettings) (DesktopPrefs, error) {
	version, err := parseRealityClientVersion(next.ClientVersion)
	if err != nil {
		return currentDesktopPrefs(), err
	}
	next.ClientVersion = version
	next.MLKEM = normalizeRealityMLKEM(next.MLKEM)
	if next.MLKEM == "auto" {
		next.MLKEM = ""
	}

	prefsMu.Lock()
	prefsCurrent.Reality = next
	snapshot := prefsCurrent
	savePrefsBestEffort(snapshot)
	prefsMu.Unlock()

	a.triggerRuntimeReloadForPrefs()
	return snapshot, nil
}

// applyRealityOverlay writes the two REALITY keys. reality-mlkem is always
// written (the user's mode, auto by default). reality-client-version comes
// from the user, else from the subscription when valid, else it is omitted so
// the core uses its default. A malformed subscription value is dropped: the
// patched core rejects the whole config on it.
func applyRealityOverlay(m map[string]any, r RealitySettings) {
	m["reality-mlkem"] = coreRealityMLKEM(r.MLKEM)

	version, _ := parseRealityClientVersion(r.ClientVersion)
	if version == "" {
		if raw, ok := m["reality-client-version"]; ok {
			version, _ = parseRealityClientVersion(fmt.Sprint(raw))
		}
	}
	if version == "" {
		delete(m, "reality-client-version")
		return
	}
	m["reality-client-version"] = version
}
