package main

import (
	"strings"
	"testing"
)

func TestRuntimeSmokeFinalizerPrunesUnknownRefsAndProviders(t *testing.T) {
	t.Parallel()
	cfg := map[string]any{
		"proxy-providers": map[string]any{
			"used-provider":   map[string]any{"type": "http"},
			"unused-provider": map[string]any{"type": "http"},
		},
		"proxy-groups": []any{
			map[string]any{
				"name":    "Main",
				"type":    "select",
				"use":     []any{"used-provider"},
				"proxies": []any{"UNKNOWN_NODE", "UNKNOWN_GROUP"},
			},
		},
		"rules": []any{"MATCH,Main"},
	}

	if err := finalizeRuntimeConfigPipeline(cfg, t.TempDir(), 7890, 9090, "secret", "tun", true, false); err != nil {
		t.Fatalf("finalizeRuntimeConfigPipeline failed: %v", err)
	}

	providers, _ := cfg["proxy-providers"].(map[string]any)
	if len(providers) != 1 || providers["used-provider"] == nil {
		t.Fatalf("expected only used provider to remain, got: %#v", providers)
	}

	groups, _ := cfg["proxy-groups"].([]any)
	main, _ := groups[0].(map[string]any)
	proxies, _ := main["proxies"].([]any)
	if len(proxies) != 1 || strings.TrimSpace(toString(proxies[0])) != "DIRECT" {
		t.Fatalf("expected unknown refs to fallback to DIRECT, got: %#v", proxies)
	}
}

func TestRuntimeSmokeFinalizerMovesMatchToEnd(t *testing.T) {
	t.Parallel()
	cfg := map[string]any{
		"proxy-groups": []any{
			map[string]any{"name": "Main", "type": "select", "proxies": []any{"DIRECT"}},
		},
		"rules": []any{
			"MATCH,Main",
			"DOMAIN-SUFFIX,example.com,Main",
		},
	}

	if err := finalizeRuntimeConfigPipeline(cfg, t.TempDir(), 7890, 9090, "secret", "proxy", true, false); err != nil {
		t.Fatalf("finalizeRuntimeConfigPipeline failed: %v", err)
	}

	rules, _ := cfg["rules"].([]any)
	if len(rules) < 2 {
		t.Fatalf("expected at least two rules after finalize, got: %#v", rules)
	}
	last, _ := rules[len(rules)-1].(string)
	if !strings.HasPrefix(strings.ToUpper(strings.TrimSpace(last)), "MATCH,") {
		t.Fatalf("expected MATCH rule to be last, got last rule: %q", last)
	}
}

// Post-v0.3.1 (verge-rev alignment): the generated YAML reflects the caller's
// enableTun argument verbatim. This mirrors enhance::tun::use_tun — YAML always
// tells the truth about what the user wants so PUT /configs?force=true can be
// idempotent across reloads. Hardening knobs (stack/auto-route/dns-hijack) are
// present regardless of enable so Mihomo has everything it needs when the
// eventual Connect brings TUN up.
func TestRuntimeSmokeTunAndProxyTrafficOverlays(t *testing.T) {
	t.Parallel()
	base := map[string]any{
		"proxy-groups": []any{
			map[string]any{"name": "Main", "type": "select", "proxies": []any{"DIRECT"}},
		},
		"dns": map[string]any{
			"respect-rules": true,
		},
		"rules": []any{"MATCH,Main"},
	}

	tunConnectedCfg := cloneMap(base)
	if err := finalizeRuntimeConfigPipeline(tunConnectedCfg, t.TempDir(), 7890, 9090, "secret", "tun", true, true); err != nil {
		t.Fatalf("finalizeRuntimeConfigPipeline(tun, enableTun=true) failed: %v", err)
	}
	if !tunEnabled(tunConnectedCfg) {
		t.Fatalf("enableTun=true must yield tun.enable=true in YAML (verge-rev parity)")
	}
	if !hasTunHardening(tunConnectedCfg) {
		t.Fatalf("tun block must carry stack/auto-route hardening when enabled")
	}
	if !hasProxyServerNameserver(tunConnectedCfg) {
		t.Fatalf("tun enabled mode should repair dns.proxy-server-nameserver")
	}

	tunIdleCfg := cloneMap(base)
	if err := finalizeRuntimeConfigPipeline(tunIdleCfg, t.TempDir(), 7890, 9090, "secret", "tun", true, false); err != nil {
		t.Fatalf("finalizeRuntimeConfigPipeline(tun, enableTun=false) failed: %v", err)
	}
	if tunEnabled(tunIdleCfg) {
		t.Fatalf("enableTun=false must yield tun.enable=false even when saved traffic is tun")
	}
	if !hasTunHardening(tunIdleCfg) {
		t.Fatalf("tun hardening must stay in YAML so the first Connect brings wintun up without a second reload")
	}

	proxyCfg := cloneMap(base)
	if err := finalizeRuntimeConfigPipeline(proxyCfg, t.TempDir(), 7890, 9090, "secret", "proxy", true, false); err != nil {
		t.Fatalf("finalizeRuntimeConfigPipeline(proxy) failed: %v", err)
	}
	if tunEnabled(proxyCfg) {
		t.Fatalf("proxy mode must not have tun.enable=true in YAML")
	}
	if !hasTunHardening(proxyCfg) {
		t.Fatalf("tun hardening must still be present in proxy profile (SetTrafficMode→tun must not need a second reload)")
	}
}

func TestRuntimeSmokePreservesSnifferAndUDPProxyFields(t *testing.T) {
	t.Parallel()
	cfg := map[string]any{
		"proxy-groups": []any{
			map[string]any{"name": "Main", "type": "select", "proxies": []any{"udp-node", "DIRECT"}},
		},
		"proxies": []any{
			map[string]any{
				"name":   "udp-node",
				"type":   "vless",
				"server": "example.test",
				"port":   443,
				"uuid":   "00000000-0000-0000-0000-000000000000",
				"udp":    true,
			},
		},
		"sniffer": map[string]any{
			"enable": true,
			"sniff": map[string]any{
				"QUIC": map[string]any{
					"ports": []any{443, "8443"},
				},
			},
		},
		"rules": []any{"MATCH,Main"},
	}
	if err := finalizeRuntimeConfigPipeline(cfg, t.TempDir(), 7890, 9090, "secret", "tun", true, true); err != nil {
		t.Fatalf("finalizeRuntimeConfigPipeline failed: %v", err)
	}

	proxies, _ := cfg["proxies"].([]any)
	first, _ := proxies[0].(map[string]any)
	udp, _ := first["udp"].(bool)
	if !udp {
		t.Fatalf("expected udp=true to be preserved on proxy node")
	}

	sniffer, _ := cfg["sniffer"].(map[string]any)
	sniff, _ := sniffer["sniff"].(map[string]any)
	quic, _ := sniff["QUIC"].(map[string]any)
	ports, _ := quic["ports"].([]any)
	if len(ports) == 0 {
		t.Fatalf("expected sniffer QUIC ports to remain present")
	}
}

func hasProxyServerNameserver(m map[string]any) bool {
	dns, _ := m["dns"].(map[string]any)
	if dns == nil {
		return false
	}
	switch v := dns["proxy-server-nameserver"].(type) {
	case []any:
		return len(v) > 0
	case []string:
		return len(v) > 0
	case string:
		return strings.TrimSpace(v) != ""
	default:
		return false
	}
}

func tunEnabled(m map[string]any) bool {
	tun, _ := m["tun"].(map[string]any)
	if tun == nil {
		return false
	}
	enabled, _ := tun["enable"].(bool)
	return enabled
}

// hasTunHardening asserts the TUN block in generated YAML carries the knobs
// that make wintun/system-stack bring-up reliable when API flips enable=true.
func hasTunHardening(m map[string]any) bool {
	tun, _ := m["tun"].(map[string]any)
	if tun == nil {
		return false
	}
	stack, _ := tun["stack"].(string)
	if strings.TrimSpace(stack) == "" {
		return false
	}
	if _, ok := tun["auto-route"]; !ok {
		return false
	}
	if _, ok := tun["dns-hijack"]; !ok {
		return false
	}
	return true
}

func toString(v any) string {
	if s, ok := v.(string); ok {
		return s
	}
	return ""
}

func cloneMap(src map[string]any) map[string]any {
	out := make(map[string]any, len(src))
	for k, v := range src {
		out[k] = deepClone(v)
	}
	return out
}

func deepClone(v any) any {
	switch x := v.(type) {
	case map[string]any:
		m := make(map[string]any, len(x))
		for k, vv := range x {
			m[k] = deepClone(vv)
		}
		return m
	case []any:
		a := make([]any, len(x))
		for i := range x {
			a[i] = deepClone(x[i])
		}
		return a
	default:
		return x
	}
}
