package main

import (
	"testing"
)

// representativeFullProfile models a real provider subscription (like the
// 2026-06-12 case): proxies + groups + rules + a custom dns block + a tun block
// + sniffer + geo fields. dns.listen is intentionally ABSENT so we exercise our
// default. This is the input to the parity audit (config-divergence-audit).
func representativeFullProfile() map[string]any {
	return map[string]any{
		"mode":          "rule",
		"ipv6":          true,
		"tcp-concurrent": true,
		"unified-delay": true,
		// sub-provided geo cadence — must be preserved (not overridden).
		"geodata-mode":    true,
		"geo-auto-update": true,
		"geo-update-interval": 72,
		"dns": map[string]any{
			"enable":        true,
			"enhanced-mode": "fake-ip",
			"fake-ip-range": "198.18.0.1/16",
			"respect-rules": true,
			"ipv6":          true,
			"use-hosts":     true,
			"fake-ip-filter": []any{"+.ru", "*.lan"},
			"nameserver":     []any{"https://1.1.1.1/dns-query", "https://77.88.8.8/dns-query"},
			"proxy-server-nameserver": []any{"1.1.1.1", "8.8.8.8", "77.88.8.8"},
			"nameserver-policy": map[string]any{
				"+.ru": []any{"https://77.88.8.8/dns-query"},
			},
			// NOTE: no "listen" — exercises our default.
		},
		"tun": map[string]any{
			"stack":                 "mixed",
			"auto-route":            true,
			"auto-detect-interface": true,
			"strict-route":          true,
			"dns-hijack":            []any{"any:53"},
			"enable":                false,
		},
		"sniffer": map[string]any{"enable": true},
		"proxies": []any{
			map[string]any{"name": "n1", "type": "ss", "server": "s.example.com", "port": 443, "cipher": "aes-128-gcm", "password": "pw"},
		},
		"proxy-groups": []any{
			map[string]any{"name": "PROXY", "type": "select", "proxies": []any{"n1", "DIRECT"}},
		},
		"rules": []any{"MATCH,PROXY"},
	}
}

func dnsMap(t *testing.T, m map[string]any) map[string]any {
	t.Helper()
	d, ok := m["dns"].(map[string]any)
	if !ok {
		t.Fatalf("dns block missing/wrong type: %#v", m["dns"])
	}
	return d
}

// Parity invariants for a subscription that ships its own dns/tun blocks, run
// through the real finalizer pipeline (TUN mode). These lock the reconciled
// verge-parity behavior so a future change can't silently drift.
func TestConfigParitySubscriptionBlocksPreserved(t *testing.T) {
	m := representativeFullProfile()
	if err := finalizeRuntimeConfigPipeline(m, t.TempDir(), 54333, 9097, "secret", "tun", true, true); err != nil {
		t.Fatalf("pipeline error: %v", err)
	}

	d := dnsMap(t, m)

	// DNS listen: default :1053 (all interfaces, real fixed port — verge parity).
	// Loopback breaks TUN dns-hijack; an ephemeral :0 listen did not work for users.
	if d["listen"] != ":1053" {
		t.Errorf("dns.listen = %v, want :1053", d["listen"])
	}
	// Subscription DNS fields preserved (NOT clobbered by our overlay).
	if _, ok := d["proxy-server-nameserver"]; !ok {
		t.Error("proxy-server-nameserver dropped (must be preserved from subscription)")
	}
	if _, ok := d["nameserver-policy"]; !ok {
		t.Error("nameserver-policy dropped (must be preserved)")
	}
	if ff, ok := d["fake-ip-filter"].([]any); !ok || len(ff) == 0 {
		t.Error("fake-ip-filter dropped (must be preserved)")
	}
	if d["enhanced-mode"] != "fake-ip" {
		t.Errorf("enhanced-mode = %v, want fake-ip (preserved)", d["enhanced-mode"])
	}
	if d["respect-rules"] != true {
		t.Errorf("respect-rules = %v, want true (preserved)", d["respect-rules"])
	}

	// TUN: subscription block preserved; only enable is overwritten to intent.
	tun, ok := m["tun"].(map[string]any)
	if !ok {
		t.Fatalf("tun block missing: %#v", m["tun"])
	}
	if tun["stack"] != "mixed" {
		t.Errorf("tun.stack = %v, want mixed (preserved from subscription)", tun["stack"])
	}
	if tun["enable"] != true {
		t.Errorf("tun.enable = %v, want true (overwritten to traffic intent)", tun["enable"])
	}

	// geo cadence from subscription preserved (we only default when ABSENT).
	if m["geo-auto-update"] != true {
		t.Errorf("geo-auto-update = %v, want true (subscription-provided, preserved)", m["geo-auto-update"])
	}

	// Sloth runtime overlay (intentional, documented divergences from verge):
	if m["mixed-port"] != 54333 {
		t.Errorf("mixed-port = %v, want 54333 (we use a random free port by design)", m["mixed-port"])
	}
	if m["socks-port"] != 0 || m["port"] != 0 {
		t.Errorf("socks-port/port = %v/%v, want 0/0 (disabled)", m["socks-port"], m["port"])
	}
	if m["allow-lan"] != false {
		t.Errorf("allow-lan = %v, want false", m["allow-lan"])
	}
	prof, ok := m["profile"].(map[string]any)
	if !ok || prof["store-selected"] != true {
		t.Errorf("profile.store-selected missing/false: %#v", m["profile"])
	}

	// Routing payload intact.
	if px, ok := m["proxies"].([]any); !ok || len(px) != 1 {
		t.Errorf("proxies not preserved: %#v", m["proxies"])
	}
}
