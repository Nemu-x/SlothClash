package main

import (
	"bytes"
	"errors"
	"sort"
	"strconv"
	"strings"

	"SlothClashDesktop/sharelink"

	"gopkg.in/yaml.v3"
)

// canonicalRuntimeKeyOrder is the top-level key layout used by
// clash-verge-rev's generated configs. Go's yaml.v3 marshals
// map[string]any alphabetically, which produced configs where `secret`
// and `sniffer` ended up *below* the `rules:` block — technically valid,
// but visually surprising for anyone diffing against an upstream verge-rev
// config. Reordering only kicks in at the top level; nested maps (tun.*,
// dns.*, sniffer.*) keep yaml.v3's default alphabetical ordering, which is
// fine for the operational use case (humans look at section position, not
// inner-field position).
//
// Keys not present in this list float to the end of the document while
// preserving their relative order — that way custom subscription fields
// never disappear or get reshuffled.
var canonicalRuntimeKeyOrder = []string{
	// Network listeners / inbound first — the most operationally important.
	"mixed-port",
	"socks-port",
	"port",
	"redir-port",
	"tproxy-port",
	"inbound-tfo",
	"inbound-mptcp",
	// Global flags.
	"ipv6",
	"allow-lan",
	"bind-address",
	"lan-allowed-ips",
	"lan-disallowed-ips",
	"authentication",
	"skip-auth-prefixes",
	"mode",
	"log-level",
	"clash-for-android",
	"iptables",
	"etag-support",
	"interface-name",
	"routing-mark",
	"keep-alive-idle",
	"keep-alive-interval",
	"disable-keep-alive",
	"tcp-keep-alive",
	"tcp-concurrent",
	"unified-delay",
	"find-process-mode",
	"global-client-fingerprint",
	"global-ua",
	// Controller / API.
	"external-controller",
	"external-controller-tls",
	"external-controller-pipe",
	"external-controller-unix",
	"external-controller-cors",
	"external-doh-server",
	"external-ui",
	"external-ui-name",
	"external-ui-url",
	"secret",
	// Profile / sniffer / tls / tun / dns / ntp — system-level blocks.
	"profile",
	"ntp",
	"sniffer",
	"tls",
	"tun",
	"dns",
	// Geo — geo-update-interval is the cadence sibling of geo-auto-update;
	// pre-0.4.1 it was missing from the canonical list and ended up below
	// `rules:` in the generated YAML.
	"geodata-mode",
	"geodata-loader",
	"geosite-matcher",
	"geo-auto-update",
	"geo-update-interval",
	"geox-url",
	"geoip",
	"geosite",
	"hosts",
	"experimental",
	// Inbound listener APIs (mihomo's modern way to expose multiple inbounds
	// without bouncing the global `mixed-port`). These go between hosts and
	// the routing payload because they are "what the core listens on" rather
	// than "what it routes to".
	"listeners",
	"tunnels",
	"ss-config",
	"vmess-config",
	"tuic-server",
	// Routing payload — providers, proxies, groups, rule-providers, rules
	// at the very bottom because they are the longest blocks.
	"proxy-providers",
	"proxies",
	"proxy-groups",
	"rule-providers",
	"rules",
	"sub-rules",
	"script",
}

// reorderTopLevelMapping rewrites the top-level key sequence of a
// MappingNode in place so it matches canonicalRuntimeKeyOrder.
// Unknown keys are appended at the end in their original relative order
// (stable sort) so subscription-supplied fields we do not know about are
// never lost or shuffled relative to each other.
func reorderTopLevelMapping(n *yaml.Node) {
	if n == nil {
		return
	}
	if n.Kind == yaml.DocumentNode && len(n.Content) > 0 {
		reorderTopLevelMapping(n.Content[0])
		return
	}
	if n.Kind != yaml.MappingNode || len(n.Content) < 2 {
		return
	}
	rank := make(map[string]int, len(canonicalRuntimeKeyOrder))
	for i, k := range canonicalRuntimeKeyOrder {
		rank[k] = i
	}
	unknownRank := len(canonicalRuntimeKeyOrder) + 1
	type pair struct{ key, val *yaml.Node }
	pairs := make([]pair, 0, len(n.Content)/2)
	for i := 0; i+1 < len(n.Content); i += 2 {
		pairs = append(pairs, pair{key: n.Content[i], val: n.Content[i+1]})
	}
	sort.SliceStable(pairs, func(i, j int) bool {
		ri, ok := rank[pairs[i].key.Value]
		if !ok {
			ri = unknownRank
		}
		rj, ok := rank[pairs[j].key.Value]
		if !ok {
			rj = unknownRank
		}
		return ri < rj
	})
	n.Content = n.Content[:0]
	for _, p := range pairs {
		n.Content = append(n.Content, p.key, p.val)
	}
}

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
	// Encode the map into a yaml.Node tree first, then reorder the top-level
	// keys to match clash-verge-rev's canonical layout. yaml.v3 marshals
	// map[string]any alphabetically which would otherwise put `secret` and
	// `sniffer` after `rules` and confuse anyone diffing against an upstream
	// config.
	var doc yaml.Node
	if err := doc.Encode(v); err != nil {
		return nil, err
	}
	reorderTopLevelMapping(&doc)
	b, err := yaml.Marshal(&doc)
	if err != nil {
		return nil, err
	}
	// yaml.v3 escapes many non-ASCII runes as \UXXXXXXXX; decode for user-facing config readability.
	return []byte(decodeUnicodeEscapes(string(b))), nil
}

func parseClashDocToMap(b []byte) (map[string]any, error) {
	m, _, err := parseClashDocToMapReport(b)
	return m, err
}

// parseClashDocToMapReport is parseClashDocToMap plus the list of share-link
// lines it could NOT convert (unsupported scheme / malformed). Import paths use
// it to report "imported N, skipped M" instead of silently dropping nodes. The
// skipped list is only populated on the share-link branch; Clash-YAML / base64
// inputs return nil (no per-line concept).
func parseClashDocToMapReport(b []byte) (map[string]any, []string, error) {
	b = bytes.TrimSpace(b)
	if len(b) == 0 {
		return nil, nil, errors.New("empty subscription body")
	}
	b = bytes.TrimPrefix(b, []byte{0xEF, 0xBB, 0xBF})

	var m map[string]any
	err := yaml.Unmarshal(b, &m)
	if err == nil && len(m) > 0 {
		return m, nil, nil
	}
	if dec, derr := decodeBase64Flexible(strings.TrimSpace(string(b))); derr == nil && len(dec) > 0 {
		dec = bytes.TrimSpace(dec)
		dec = bytes.TrimPrefix(dec, []byte{0xEF, 0xBB, 0xBF})
		var m2 map[string]any
		if err2 := yaml.Unmarshal(dec, &m2); err2 == nil && len(m2) > 0 {
			return m2, nil, nil
		}
	}
	// Not Clash YAML (plain or base64). Try a V2Ray-style share-link list
	// (vless://, vmess://, ss://, trojan://, hysteria2://, tuic://) — single
	// link, newline list, or base64 envelope — and synthesize a runnable config.
	if doc, skipped, ok := shareLinksToClashMap(string(b)); ok {
		return doc, skipped, nil
	}
	if err != nil {
		return nil, nil, err
	}
	return nil, nil, errors.New("invalid clash yaml mapping")
}

// shareLinksToClashMap converts proxy share links into a minimal but complete
// Clash/mihomo config map: the parsed proxies, a default `PROXY` select group
// (all nodes + DIRECT), and a catch-all rule. Returns ok=false if no supported
// link is found. Having proxy-groups + rules makes the result a "full profile"
// so the normal merge pipeline handles ports/TUN/overlay uniformly.
func shareLinksToClashMap(text string) (map[string]any, []string, bool) {
	proxies, skipped := sharelink.ParseMany(text)
	if len(proxies) == 0 {
		if dec := sharelink.DecodeBase64Block(text); dec != "" {
			proxies, skipped = sharelink.ParseMany(dec)
		}
	}
	if len(proxies) == 0 {
		return nil, skipped, false
	}
	rawProxies := make([]any, 0, len(proxies))
	groupProxies := make([]any, 0, len(proxies)+1)
	for _, p := range proxies {
		rawProxies = append(rawProxies, map[string]any(p))
		groupProxies = append(groupProxies, p["name"])
	}
	groupProxies = append(groupProxies, "DIRECT")
	return map[string]any{
		"proxies": rawProxies,
		"proxy-groups": []any{
			map[string]any{"name": "PROXY", "type": "select", "proxies": groupProxies},
		},
		"rules": []any{"MATCH,PROXY"},
	}, skipped, true
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
