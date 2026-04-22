package main

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

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

const tunDefaultDNSYAML = `dns:
  enable: true
  listen: 127.0.0.1:0
  ipv6: true
  respect-rules: true
  enhanced-mode: fake-ip
  fake-ip-range: 198.18.0.1/16
  use-hosts: true
  default-nameserver:
    - 1.1.1.1
    - 8.8.8.8
  nameserver:
    - https://1.1.1.1/dns-query
    - tls://8.8.8.8:853
`

func fetchSubscriptionBody(ctx context.Context, rawURL string) ([]byte, error) {
	norm, err := normalizeSubscriptionURL(rawURL)
	if err != nil {
		return nil, err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, norm, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("User-Agent", "clash.meta/mihomo; SlothClash/1.0")

	client := &http.Client{
		Timeout: 50 * time.Second,
		CheckRedirect: func(_ *http.Request, via []*http.Request) error {
			if len(via) >= 12 {
				return errors.New("too many redirects")
			}
			return nil
		},
	}

	resp, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		b, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
		return nil, fmt.Errorf("subscription HTTP %d: %s", resp.StatusCode, strings.TrimSpace(string(b)))
	}
	return io.ReadAll(io.LimitReader(resp.Body, 6<<20))
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

func mergeTunFromYAMLString(m map[string]any, fragment string) {
	var wrap map[string]any
	if err := yaml.Unmarshal([]byte(fragment), &wrap); err != nil {
		return
	}
	if t, ok := wrap["tun"].(map[string]any); ok {
		m["tun"] = t
	}
}

func ensureDefaultDNSForTun(m map[string]any) {
	raw, hasDNS := m["dns"]
	var dns map[string]any
	if hasDNS {
		if dm, ok := raw.(map[string]any); ok {
			dns = dm
		}
	}
	if dns == nil {
		var wrap map[string]any
		if err := yaml.Unmarshal([]byte(tunDefaultDNSYAML), &wrap); err != nil {
			return
		}
		if d, ok := wrap["dns"].(map[string]any); ok {
			m["dns"] = d
		}
		return
	}

	// Clash Verge-like behavior: keep user DNS settings, only fill missing TUN-critical keys.
	if _, ok := dns["enable"]; !ok {
		dns["enable"] = true
	}
	if _, ok := dns["enhanced-mode"]; !ok {
		dns["enhanced-mode"] = "fake-ip"
	}
	if mode, _ := dns["enhanced-mode"].(string); strings.TrimSpace(strings.ToLower(mode)) == "fake-ip" {
		if _, ok := dns["fake-ip-range"]; !ok {
			dns["fake-ip-range"] = "198.18.0.1/16"
		}
	}
	if _, ok := dns["ipv6"]; !ok {
		if topIPv6, has := m["ipv6"].(bool); has {
			dns["ipv6"] = topIPv6
		} else {
			dns["ipv6"] = true
		}
	}
	// Mihomo requires proxy-server-nameserver when respect-rules is enabled.
	// Keep Verge-like non-destructive behavior: only fill it when missing/empty.
	respectRules := false
	switch v := dns["respect-rules"].(type) {
	case bool:
		respectRules = v
	case string:
		s := strings.ToLower(strings.TrimSpace(v))
		respectRules = s == "true" || s == "1" || s == "yes" || s == "on"
	}
	if respectRules {
		repair := true
		switch vv := dns["proxy-server-nameserver"].(type) {
		case []any:
			repair = len(vv) == 0
		case []string:
			repair = len(vv) == 0
		case string:
			repair = strings.TrimSpace(vv) == ""
		}
		if repair {
			if dnsDefault, ok := dns["default-nameserver"].([]any); ok && len(dnsDefault) > 0 {
				dns["proxy-server-nameserver"] = append([]any(nil), dnsDefault...)
			} else if dnsDefaultS, ok := dns["default-nameserver"].([]string); ok && len(dnsDefaultS) > 0 {
				out := make([]any, 0, len(dnsDefaultS))
				for _, s := range dnsDefaultS {
					out = append(out, s)
				}
				dns["proxy-server-nameserver"] = out
			} else {
				dns["proxy-server-nameserver"] = []any{"1.1.1.1", "8.8.8.8"}
			}
		}
	}
	m["dns"] = dns
}

func ensureTunOverlayForTraffic(m map[string]any, traffic string) {
	if strings.TrimSpace(traffic) != "tun" {
		if raw, ok := m["tun"].(map[string]any); ok {
			raw["enable"] = false
			m["tun"] = raw
			return
		}
		m["tun"] = map[string]any{"enable": false}
		return
	}

	rawTun, has := m["tun"].(map[string]any)
	if !has || rawTun == nil {
		mergeTunFromYAMLString(m, tunBlockForTraffic("tun"))
		if t, ok := m["tun"].(map[string]any); ok {
			t["enable"] = true
			m["tun"] = t
		}
		return
	}

	// Keep upstream/profile knobs, only ensure TUN is on.
	rawTun["enable"] = true
	m["tun"] = rawTun
}

func ensureRealtimeRoutingDefaults(m map[string]any) {
	if _, ok := m["find-process-mode"]; !ok {
		m["find-process-mode"] = "strict"
	}
	rawSniffer, ok := m["sniffer"]
	if !ok || rawSniffer == nil {
		m["sniffer"] = map[string]any{
			"enable":        true,
			"parse-pure-ip": true,
			"sniff": map[string]any{
				"TLS":  map[string]any{"ports": []any{"443", "8443"}},
				"HTTP": map[string]any{"ports": []any{"80", "8080-8880"}, "override-destination": true},
				"QUIC": map[string]any{"ports": []any{"443", "8443"}},
			},
		}
		return
	}
	sm, ok := rawSniffer.(map[string]any)
	if !ok {
		return
	}
	if _, ok := sm["enable"]; !ok {
		sm["enable"] = true
	}
	if _, ok := sm["parse-pure-ip"]; !ok {
		sm["parse-pure-ip"] = true
	}
	raw, ok := sm["sniff"]
	if !ok {
		sm["sniff"] = map[string]any{
			"TLS":  map[string]any{"ports": []any{"443", "8443"}},
			"HTTP": map[string]any{"ports": []any{"80", "8080-8880"}, "override-destination": true},
			"QUIC": map[string]any{"ports": []any{"443", "8443"}},
		}
	} else if sniffMap, ok := raw.(map[string]any); ok {
		if httpRaw, ok := sniffMap["HTTP"].(map[string]any); ok {
			if _, has := httpRaw["override-destination"]; !has {
				httpRaw["override-destination"] = true
			}
			sniffMap["HTTP"] = httpRaw
		}
		sm["sniff"] = sniffMap
	}
	m["sniffer"] = sm
}

func overlaySlothRuntimeOnMap(m map[string]any, mixedPort, ctrlPort int, secret, traffic string, withExternalController bool) {
	m["mixed-port"] = mixedPort
	m["socks-port"] = 0
	m["port"] = 0

	if withExternalController && ctrlPort > 0 {
		m["external-controller"] = fmt.Sprintf("127.0.0.1:%d", ctrlPort)
	} else {
		delete(m, "external-controller")
	}
	m["secret"] = secret
	m["allow-lan"] = false

	// Keep DNS invariants valid regardless of traffic mode.
	ensureDefaultDNSForTun(m)

	if strings.TrimSpace(traffic) == "tun" {
		ensureTunOverlayForTraffic(m, "tun")
	} else {
		ensureTunOverlayForTraffic(m, "proxy")
	}
	ensureRealtimeRoutingDefaults(m)
}

// ensureGlobalProxyGroup prepends a GLOBAL selector when missing so PATCH mode global +
// PUT /proxies/GLOBAL works (many published profiles omit an explicit GLOBAL group).
func ensureGlobalProxyGroup(m map[string]any) {
	raw, ok := m["proxy-groups"]
	if !ok || raw == nil {
		return
	}
	arr, ok := raw.([]any)
	if !ok || len(arr) == 0 {
		return
	}
	for _, g := range arr {
		gm, ok := g.(map[string]any)
		if !ok {
			continue
		}
		name, _ := gm["name"].(string)
		if strings.EqualFold(strings.TrimSpace(name), "GLOBAL") {
			return
		}
	}

	seen := map[string]bool{"DIRECT": true, "REJECT": true}
	outNames := []string{"DIRECT", "REJECT"}
	for _, g := range arr {
		gm, ok := g.(map[string]any)
		if !ok {
			continue
		}
		n, _ := gm["name"].(string)
		n = strings.TrimSpace(n)
		if n == "" || strings.EqualFold(n, "GLOBAL") {
			continue
		}
		if !seen[n] {
			seen[n] = true
			outNames = append(outNames, n)
		}
	}

	global := map[string]any{
		"name":    "GLOBAL",
		"type":    "select",
		"proxies": outNames,
	}
	m["proxy-groups"] = append([]any{global}, arr...)
}

func validateRulePoliciesExist(m map[string]any) error {
	known := map[string]bool{
		"DIRECT":      true,
		"REJECT":      true,
		"REJECT-DROP": true,
		"PASS":        true,
		"GLOBAL":      true,
	}
	if groups, ok := m["proxy-groups"].([]any); ok {
		for _, g := range groups {
			gm, ok := g.(map[string]any)
			if !ok {
				continue
			}
			name, _ := gm["name"].(string)
			name = strings.TrimSpace(name)
			if name != "" {
				known[name] = true
			}
		}
	}
	rules, ok := m["rules"].([]any)
	if !ok {
		return nil
	}
	for idx, r := range rules {
		line, ok := r.(string)
		if !ok {
			continue
		}
		policy := extractRulePolicyToken(line)
		if policy == "" {
			continue
		}
		if !known[policy] {
			return fmt.Errorf(
				"rules[%d] references unknown policy %q in rule %q",
				idx,
				policy,
				strings.TrimSpace(line),
			)
		}
	}
	return nil
}

func splitRuleCSV(rule string) []string {
	s := strings.TrimSpace(rule)
	if s == "" {
		return nil
	}
	out := make([]string, 0, 8)
	var b strings.Builder
	depth := 0
	for _, ch := range s {
		switch ch {
		case '(':
			depth++
			b.WriteRune(ch)
		case ')':
			if depth > 0 {
				depth--
			}
			b.WriteRune(ch)
		case ',':
			if depth == 0 {
				out = append(out, strings.TrimSpace(b.String()))
				b.Reset()
				continue
			}
			b.WriteRune(ch)
		default:
			b.WriteRune(ch)
		}
	}
	if b.Len() > 0 {
		out = append(out, strings.TrimSpace(b.String()))
	}
	return out
}

func isRuleOptionToken(token string) bool {
	t := strings.ToLower(strings.TrimSpace(token))
	return t == "no-resolve" || strings.HasPrefix(t, "src=") || strings.HasPrefix(t, "dst=")
}

func extractRulePolicyToken(rule string) string {
	parts := splitRuleCSV(rule)
	if len(parts) < 2 {
		return ""
	}
	// Skip rule type + payload; last non-option token is expected outbound policy.
	for i := len(parts) - 1; i >= 2; i-- {
		token := strings.TrimSpace(parts[i])
		if token == "" || isRuleOptionToken(token) {
			continue
		}
		return token
	}
	// MATCH,DIRECT-like rules have no payload section.
	if len(parts) == 2 {
		head := strings.ToUpper(strings.TrimSpace(parts[0]))
		if head == "MATCH" || head == "FINAL" {
			return strings.TrimSpace(parts[1])
		}
	}
	return ""
}

// normalizeProxyGroupRefs keeps proxy-group references valid after template merges.
// It sanitizes both:
//   - `use`: only existing proxy-providers remain
//   - `proxies`: only valid proxy/group/provider/builtin names remain
// This mirrors Verge-like cleanup to reduce parse/runtime surprises.
func normalizeProxyGroupRefs(m map[string]any) {
	rawProviders, ok := m["proxy-providers"]
	if !ok || rawProviders == nil {
		return
	}
	providerMap, ok := rawProviders.(map[string]any)
	if !ok || len(providerMap) == 0 {
		return
	}
	providerNames := make([]string, 0, len(providerMap))
	providerSet := make(map[string]bool, len(providerMap))
	for name := range providerMap {
		n := strings.TrimSpace(name)
		if n == "" {
			continue
		}
		providerSet[n] = true
		providerNames = append(providerNames, n)
	}
	if len(providerNames) == 0 {
		return
	}

	proxySet := map[string]bool{}
	if rawProxies, ok := m["proxies"].([]any); ok {
		for _, it := range rawProxies {
			switch v := it.(type) {
			case map[string]any:
				if n, _ := v["name"].(string); strings.TrimSpace(n) != "" {
					proxySet[strings.TrimSpace(n)] = true
				}
			case string:
				if strings.TrimSpace(v) != "" {
					proxySet[strings.TrimSpace(v)] = true
				}
			}
		}
	}

	groups, ok := m["proxy-groups"].([]any)
	if !ok || len(groups) == 0 {
		return
	}
	groupSet := map[string]bool{}
	for _, g := range groups {
		gm, ok := g.(map[string]any)
		if !ok {
			continue
		}
		if n, _ := gm["name"].(string); strings.TrimSpace(n) != "" {
			groupSet[strings.TrimSpace(n)] = true
		}
	}
	allowed := map[string]bool{
		"DIRECT":      true,
		"REJECT":      true,
		"REJECT-DROP": true,
		"PASS":        true,
	}
	for name := range providerSet {
		allowed[name] = true
	}
	for name := range proxySet {
		allowed[name] = true
	}
	for name := range groupSet {
		allowed[name] = true
	}

	for i := range groups {
		gm, ok := groups[i].(map[string]any)
		if !ok {
			continue
		}
		hasValidProvider := false
		useRaw, hasUse := gm["use"]
		if !hasUse || useRaw == nil {
		} else if useArr, ok := useRaw.([]any); ok {
			filtered := make([]any, 0, len(useArr))
			for _, item := range useArr {
				s, ok := item.(string)
				if !ok {
					continue
				}
				n := strings.TrimSpace(s)
				if n == "" {
					continue
				}
				if providerSet[n] {
					filtered = append(filtered, n)
					hasValidProvider = true
				}
			}
			if len(filtered) == 0 {
				// Keep provider-backed group valid instead of failing startup.
				for _, p := range providerNames {
					filtered = append(filtered, p)
				}
				hasValidProvider = len(filtered) > 0
			}
			gm["use"] = filtered
		}

		if proxiesRaw, hasProxies := gm["proxies"]; hasProxies && proxiesRaw != nil {
			if proxiesArr, ok := proxiesRaw.([]any); ok {
				out := make([]any, 0, len(proxiesArr))
				for _, item := range proxiesArr {
					s, ok := item.(string)
					if !ok {
						continue
					}
					n := strings.TrimSpace(s)
					if n == "" {
						continue
					}
					if allowed[n] {
						out = append(out, n)
					}
				}
				if len(out) == 0 {
					if hasValidProvider {
						out = append(out, "DIRECT")
					} else if allowed["DIRECT"] {
						out = append(out, "DIRECT")
					}
				}
				gm["proxies"] = out
			}
		}
		groups[i] = gm
	}
	m["proxy-groups"] = groups
}

func validateDNSInvariants(m map[string]any) error {
	// Self-heal before validating so template/profile edge cases do not break connect.
	ensureDefaultDNSForTun(m)
	dns, ok := m["dns"].(map[string]any)
	if !ok || dns == nil {
		return nil
	}
	respectRules := false
	switch v := dns["respect-rules"].(type) {
	case bool:
		respectRules = v
	case string:
		s := strings.ToLower(strings.TrimSpace(v))
		respectRules = s == "true" || s == "1" || s == "yes" || s == "on"
	}
	if respectRules {
		switch v := dns["proxy-server-nameserver"].(type) {
		case []any:
			if len(v) > 0 {
				return nil
			}
		case []string:
			if len(v) > 0 {
				return nil
			}
		case string:
			if strings.TrimSpace(v) != "" {
				return nil
			}
		}
		// Final hard fallback: force defaults and accept.
		dns["proxy-server-nameserver"] = []any{"1.1.1.1", "8.8.8.8"}
		m["dns"] = dns
	}
	return nil
}

func validateProxyGroupRefs(m map[string]any) error {
	providerSet := map[string]bool{}
	if providers, ok := m["proxy-providers"].(map[string]any); ok {
		for k := range providers {
			if strings.TrimSpace(k) != "" {
				providerSet[strings.TrimSpace(k)] = true
			}
		}
	}
	allowed := map[string]bool{
		"DIRECT":      true,
		"REJECT":      true,
		"REJECT-DROP": true,
		"PASS":        true,
	}
	if proxies, ok := m["proxies"].([]any); ok {
		for _, it := range proxies {
			switch v := it.(type) {
			case map[string]any:
				if n, _ := v["name"].(string); strings.TrimSpace(n) != "" {
					allowed[strings.TrimSpace(n)] = true
				}
			case string:
				if strings.TrimSpace(v) != "" {
					allowed[strings.TrimSpace(v)] = true
				}
			}
		}
	}
	if groups, ok := m["proxy-groups"].([]any); ok {
		for _, g := range groups {
			if gm, ok := g.(map[string]any); ok {
				if n, _ := gm["name"].(string); strings.TrimSpace(n) != "" {
					allowed[strings.TrimSpace(n)] = true
				}
			}
		}
		for idx, g := range groups {
			gm, ok := g.(map[string]any)
			if !ok {
				continue
			}
			name, _ := gm["name"].(string)
			name = strings.TrimSpace(name)
			if useArr, ok := gm["use"].([]any); ok {
				for _, u := range useArr {
					if s, ok := u.(string); ok && strings.TrimSpace(s) != "" && !providerSet[strings.TrimSpace(s)] {
						return fmt.Errorf("proxy-groups[%d] %q references unknown provider %q", idx, name, strings.TrimSpace(s))
					}
				}
			}
			if pArr, ok := gm["proxies"].([]any); ok {
				for _, p := range pArr {
					if s, ok := p.(string); ok && strings.TrimSpace(s) != "" && !allowed[strings.TrimSpace(s)] {
						return fmt.Errorf("proxy-groups[%d] %q references unknown proxy/group %q", idx, name, strings.TrimSpace(s))
					}
				}
			}
		}
	}
	return nil
}

func validateFinalConfigSemantics(m map[string]any) error {
	if err := validateProxyGroupRefs(m); err != nil {
		return err
	}
	if err := validateRulePoliciesExist(m); err != nil {
		return err
	}
	if err := validateDNSInvariants(m); err != nil {
		return err
	}
	return nil
}

func cleanupUnusedProxyProviders(m map[string]any) {
	providers, ok := m["proxy-providers"].(map[string]any)
	if !ok || len(providers) == 0 {
		return
	}
	used := map[string]bool{}
	if groups, ok := m["proxy-groups"].([]any); ok {
		for _, g := range groups {
			gm, ok := g.(map[string]any)
			if !ok {
				continue
			}
			if arr, ok := gm["use"].([]any); ok {
				for _, it := range arr {
					if s, ok := it.(string); ok && strings.TrimSpace(s) != "" {
						used[strings.TrimSpace(s)] = true
					}
				}
			}
		}
	}
	for k := range providers {
		if !used[k] {
			delete(providers, k)
		}
	}
	m["proxy-providers"] = providers
}

// finalizeRuntimeConfigPipeline applies the same staged normalization pipeline used for every
// generated/edited config before persistence and preflight:
// 1) rules ordering
// 2) proxy-group reference cleanup
// 3) fallback-group pruning
// 4) runtime overlay (ports/secret/tun)
// 5) semantic validation
// 6) geodata fallback injection
func finalizeRuntimeConfigPipeline(
	m map[string]any,
	dataDir string,
	mixedPort, ctrlPort int,
	secret, traffic string,
	withExternalController bool,
) error {
	if fixed, ok := normalizeEscapedUnicodeStrings(m).(map[string]any); ok {
		for k := range m {
			delete(m, k)
		}
		for k, v := range fixed {
			m[k] = v
		}
	}
	normalizeRulesMatchLast(m)
	normalizeProxyGroupRefs(m)
	pruneFallbackAutoManualIfCustom(m)
	cleanupUnusedProxyProviders(m)
	overlaySlothRuntimeOnMap(m, mixedPort, ctrlPort, secret, traffic, withExternalController)
	if err := validateFinalConfigSemantics(m); err != nil {
		return err
	}
	mergeBundledGeoIfMissing(m, dataDir)
	return nil
}

func safeGroupNameForRules(groups []any) string {
	best := ""
	for _, g := range groups {
		gm, ok := g.(map[string]any)
		if !ok {
			continue
		}
		name, _ := gm["name"].(string)
		name = strings.TrimSpace(name)
		if name == "" {
			continue
		}
		up := strings.ToUpper(name)
		if up == "DIRECT" || up == "REJECT" || up == "REJECT-DROP" || up == "PASS" || up == "GLOBAL" {
			continue
		}
		typ, _ := gm["type"].(string)
		t := strings.ToLower(strings.ReplaceAll(strings.TrimSpace(typ), "-", ""))
		if t == "urltest" || t == "fallback" || t == "loadbalance" {
			return name
		}
		if best == "" {
			best = name
		}
	}
	return best
}

func rewriteMatchRuleTarget(m map[string]any, from, to string) {
	rules, ok := m["rules"].([]any)
	if !ok || len(rules) == 0 {
		return
	}
	from = strings.TrimSpace(from)
	to = strings.TrimSpace(to)
	if from == "" || to == "" || strings.EqualFold(from, to) {
		return
	}
	for i, r := range rules {
		line, ok := r.(string)
		if !ok {
			continue
		}
		parts := strings.Split(line, ",")
		if len(parts) < 2 {
			continue
		}
		if !strings.EqualFold(strings.TrimSpace(parts[0]), "MATCH") {
			continue
		}
		policyIdx := len(parts) - 1
		last := strings.TrimSpace(parts[policyIdx])
		if strings.EqualFold(last, "no-resolve") && len(parts) >= 3 {
			policyIdx = len(parts) - 2
			last = strings.TrimSpace(parts[policyIdx])
		}
		if !strings.EqualFold(last, from) {
			continue
		}
		parts[policyIdx] = to
		rules[i] = strings.Join(parts, ",")
	}
	m["rules"] = rules
}

// pruneFallbackAutoManualIfCustom removes built-in fallback groups once profile/template
// already defines real groups. This keeps output closer to Verge behavior and avoids stale
// fallback routing references leaking into final config.
func pruneFallbackAutoManualIfCustom(m map[string]any) {
	rawGroups, ok := m["proxy-groups"].([]any)
	if !ok || len(rawGroups) == 0 {
		return
	}
	isDefaultAuto := func(gm map[string]any) bool {
		name, _ := gm["name"].(string)
		typ, _ := gm["type"].(string)
		return strings.EqualFold(strings.TrimSpace(name), "Auto") &&
			strings.EqualFold(strings.TrimSpace(typ), "url-test")
	}
	isDefaultManual := func(gm map[string]any) bool {
		name, _ := gm["name"].(string)
		typ, _ := gm["type"].(string)
		return strings.EqualFold(strings.TrimSpace(name), "Manual") &&
			strings.EqualFold(strings.TrimSpace(typ), "select")
	}

	hasCustom := false
	autoIdx := -1
	manualIdx := -1
	for i, g := range rawGroups {
		gm, ok := g.(map[string]any)
		if !ok {
			continue
		}
		switch {
		case isDefaultAuto(gm):
			autoIdx = i
		case isDefaultManual(gm):
			manualIdx = i
		default:
			hasCustom = true
		}
	}
	if !hasCustom {
		return
	}
	if autoIdx < 0 && manualIdx < 0 {
		return
	}

	filtered := make([]any, 0, len(rawGroups))
	for i, g := range rawGroups {
		if i == autoIdx || i == manualIdx {
			continue
		}
		filtered = append(filtered, g)
	}
	if len(filtered) == 0 {
		return
	}
	repl := safeGroupNameForRules(filtered)
	if repl != "" && autoIdx >= 0 {
		rewriteMatchRuleTarget(m, "Auto", repl)
	}
	m["proxy-groups"] = filtered
}

// normalizeRulesMatchLast ensures terminal MATCH rules are placed last.
// If MATCH appears earlier, appended user rules become unreachable.
func normalizeRulesMatchLast(m map[string]any) {
	rules, ok := m["rules"].([]any)
	if !ok || len(rules) == 0 {
		return
	}
	nonMatch := make([]any, 0, len(rules))
	match := make([]any, 0, 2)
	for _, it := range rules {
		s, ok := it.(string)
		if !ok {
			nonMatch = append(nonMatch, it)
			continue
		}
		parts := strings.Split(s, ",")
		head := ""
		if len(parts) > 0 {
			head = strings.ToUpper(strings.TrimSpace(parts[0]))
		}
		if head == "MATCH" {
			match = append(match, it)
			continue
		}
		nonMatch = append(nonMatch, it)
	}
	if len(match) == 0 {
		return
	}
	out := append(nonMatch, match...)
	m["rules"] = out
}

func mergeBundledGeoIfMissing(m map[string]any, dataDir string) {
	if _, has := m["geoip"]; has {
		return
	}
	if _, has := m["geosite"]; has {
		return
	}
	if _, has := m["geox-url"]; has {
		return
	}
	geoDir := filepath.Join(dataDir, "geo")
	geoIP := filepath.Join(geoDir, "geoip.dat")
	if _, err := os.Stat(geoIP); err != nil {
		return
	}
	m["geoip"] = filepath.ToSlash(geoIP)
	m["geo-auto-update"] = false
	if gs := filepath.Join(geoDir, "geosite.dat"); fileExists(gs) {
		m["geosite"] = filepath.ToSlash(gs)
	}
}

func fileExists(p string) bool {
	_, err := os.Stat(p)
	return err == nil
}

func tryWriteMergedFullProfile(dataDir, subURL, extendTemplate, proxyTemplate, rulesTemplate string, ctrlPort, mixedPort int, secret, traffic string, withExternalController bool) (bool, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 55*time.Second)
	defer cancel()

	b, err := fetchSubscriptionBody(ctx, subURL)
	if err != nil || len(bytes.TrimSpace(b)) == 0 {
		return false, nil
	}
	doc, err := parseClashDocToMap(b)
	if err != nil || doc == nil {
		return false, nil
	}
	if !subscriptionDocIsFullProfile(doc) {
		return false, nil
	}

	if err := applyProfileMergeTemplate(doc, extendTemplate); err != nil {
		return false, err
	}
	if err := applyProfileMergeTemplate(doc, proxyTemplate); err != nil {
		return false, err
	}
	if err := applyProfileMergeTemplate(doc, rulesTemplate); err != nil {
		return false, err
	}

	if err := finalizeRuntimeConfigPipeline(
		doc,
		dataDir,
		mixedPort,
		ctrlPort,
		secret,
		traffic,
		withExternalController,
	); err != nil {
		return false, err
	}

	out, err := marshalRuntimeYAML(doc)
	if err != nil {
		return false, err
	}
	cfgPath := filepath.Join(dataDir, "config.yaml")
	if err := os.WriteFile(cfgPath, out, 0o644); err != nil {
		return false, err
	}
	return true, nil
}
