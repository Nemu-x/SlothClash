package main

import (
	"bytes"
	"errors"
	"strconv"
	"strings"

	"gopkg.in/yaml.v3"
)

func decodeUnicodeEscapes(s string) string {
	if strings.IndexByte(s, '\\') < 0 {
		return s
	}
	r := []rune(s)
	out := make([]rune, 0, len(r))
	for i := 0; i < len(r); i++ {
		if r[i] != '\\' || i+1 >= len(r) {
			out = append(out, r[i])
			continue
		}
		switch r[i+1] {
		case 'U':
			if i+9 < len(r) {
				hex := string(r[i+2 : i+10])
				cp, err := strconv.ParseUint(hex, 16, 32)
				if err == nil && cp <= 0x10FFFF {
					out = append(out, rune(cp))
					i += 9
					continue
				}
			}
		case 'u':
			if i+5 < len(r) {
				hex := string(r[i+2 : i+6])
				cp, err := strconv.ParseUint(hex, 16, 32)
				if err == nil && cp <= 0x10FFFF {
					out = append(out, rune(cp))
					i += 5
					continue
				}
			}
		}
		out = append(out, r[i])
	}
	return string(out)
}

func normalizeEscapedUnicodeStrings(v any) any {
	switch t := v.(type) {
	case string:
		return decodeUnicodeEscapes(t)
	case []any:
		out := make([]any, len(t))
		for i := range t {
			out[i] = normalizeEscapedUnicodeStrings(t[i])
		}
		return out
	case map[string]any:
		out := make(map[string]any, len(t))
		for k, vv := range t {
			out[k] = normalizeEscapedUnicodeStrings(vv)
		}
		return out
	default:
		return v
	}
}

func marshalRuntimeYAML(v any) ([]byte, error) {
	b, err := yaml.Marshal(v)
	if err != nil {
		return nil, err
	}
	// yaml.v3 escapes many non-ASCII runes as \UXXXXXXXX; decode for user-facing config readability.
	return []byte(decodeUnicodeEscapes(string(b))), nil
}

func parseClashDocToMap(b []byte) (map[string]any, error) {
	b = bytes.TrimSpace(b)
	if len(b) == 0 {
		return nil, errors.New("empty subscription body")
	}
	b = bytes.TrimPrefix(b, []byte{0xEF, 0xBB, 0xBF})

	var m map[string]any
	err := yaml.Unmarshal(b, &m)
	if err == nil && len(m) > 0 {
		return m, nil
	}
	if dec, derr := decodeBase64Flexible(strings.TrimSpace(string(b))); derr == nil && len(dec) > 0 {
		dec = bytes.TrimSpace(dec)
		dec = bytes.TrimPrefix(dec, []byte{0xEF, 0xBB, 0xBF})
		var m2 map[string]any
		if err2 := yaml.Unmarshal(dec, &m2); err2 == nil && len(m2) > 0 {
			return m2, nil
		}
	}
	if err != nil {
		return nil, err
	}
	return nil, errors.New("invalid clash yaml mapping")
}

// subscriptionDocIsFullProfile reports whether the downloaded document should be used as the
// main mihomo config (Verge-style full profile) instead of Sloth's minimal proxy-provider wrapper.
func subscriptionDocIsFullProfile(m map[string]any) bool {
	if m == nil {
		return false
	}
	// Wider full-profile heuristic (Verge-like): many real-world subscriptions do not carry
	// inline `rules`, but are still full configs with groups/providers/dns/tun/script blocks.
	for _, k := range []string{
		"rule-providers",
		"rules",
		"proxy-groups",
		"proxy-providers",
		"dns",
		"tun",
		"sniffer",
		"script",
	} {
		if v, ok := m[k]; ok && v != nil {
			switch vv := v.(type) {
			case []any:
				if len(vv) > 0 {
					return true
				}
			case map[string]any:
				if len(vv) > 0 {
					return true
				}
			default:
				return true
			}
		}
	}
	return false
}
