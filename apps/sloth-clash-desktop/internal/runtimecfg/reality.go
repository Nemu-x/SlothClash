package runtimecfg

import (
	"fmt"
	"strconv"
	"strings"
)

// NormalizeRealityMLKEM returns auto|always|never; anything else is auto.
func NormalizeRealityMLKEM(v string) string {
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
	switch NormalizeRealityMLKEM(v) {
	case "always":
		return "on"
	case "never":
		return "off"
	}
	return "auto"
}

// ParseRealityClientVersion validates "x.y.z" with each part 0..255 (the core
// stores it in three bytes of the ClientHello) and returns it canonicalised.
// "" is valid and means "core default".
func ParseRealityClientVersion(v string) (string, error) {
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

// applyRealityOverlay writes the two REALITY keys. reality-mlkem is always
// written (the user's mode, auto by default). reality-client-version comes
// from the user, else from the subscription when valid, else it is omitted so
// the core uses its default. A malformed subscription value is dropped: the
// patched core rejects the whole config on it.
func applyRealityOverlay(m map[string]any, r RealitySettings) {
	m["reality-mlkem"] = coreRealityMLKEM(r.MLKEM)

	version, _ := ParseRealityClientVersion(r.ClientVersion)
	if version == "" {
		if raw, ok := m["reality-client-version"]; ok {
			version, _ = ParseRealityClientVersion(fmt.Sprint(raw))
		}
	}
	if version == "" {
		delete(m, "reality-client-version")
		return
	}
	m["reality-client-version"] = version
}
