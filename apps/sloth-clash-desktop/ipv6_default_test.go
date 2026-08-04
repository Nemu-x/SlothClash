package main

import "testing"

// IPv6 leak guard (architecture/ipv6.md). The master IPv6 switch defaults ON
// (verge parity: verge's template ships top-level `ipv6: true`). With it OFF,
// mihomo does not process IPv6, the TUN installs no v6 route, and native IPv6
// bypasses the tunnel → blocked sites leak. These tests lock the coherent
// states so a future change can't silently reopen the hole.

func withConnectionPrefs(t *testing.T, c ConnectionSettings) {
	t.Helper()
	prefsMu.Lock()
	prev := prefsCurrent
	prefsCurrent = DesktopPrefs{Connection: c}
	prefsMu.Unlock()
	t.Cleanup(func() {
		prefsMu.Lock()
		prefsCurrent = prev
		prefsMu.Unlock()
	})
}

func boolPtr(b bool) *bool { return &b }

// Default (never-touched) prefs must yield top-level ipv6:true + dns.ipv6:true,
// with a v6 fake-ip pool present, for BOTH a subscription that ships its own dns
// block and one that ships none (the early-return branch in ensureDefaultDNSForTun).
func TestIPv6DefaultsOnAndCoherent(t *testing.T) {
	cases := map[string]func() map[string]any{
		"with dns block": representativeFullProfile,
		"without dns block": func() map[string]any {
			m := representativeFullProfile()
			delete(m, "dns")
			return m
		},
	}
	for name, build := range cases {
		t.Run(name, func(t *testing.T) {
			withConnectionPrefs(t, ConnectionSettings{}) // nil DNSIPv6 → default ON
			m := build()
			if err := finalizeRuntimeConfigPipeline(m, t.TempDir(), 54333, 9097, "secret", "tun", true, true); err != nil {
				t.Fatalf("pipeline error: %v", err)
			}
			if m["ipv6"] != true {
				t.Errorf("top-level ipv6 = %v, want true (default ON — verge parity, no v6 leak)", m["ipv6"])
			}
			d := dnsMap(t, m)
			if d["ipv6"] != true {
				t.Errorf("dns.ipv6 = %v, want true (mirrors top-level)", d["ipv6"])
			}
			// A v6 fake pool is mandatory once fake-ip runs with ipv6:true, or AAAA
			// never gets a fake address and IPv6 resolution fails outright.
			if v, ok := d["fake-ip-range6"].(string); !ok || v == "" {
				t.Errorf("dns.fake-ip-range6 = %v, want a non-empty v6 pool", d["fake-ip-range6"])
			}
		})
	}
}

// The user can still switch IPv6 OFF for a broken/half-working v6 path; both
// flags must go false together (never the incoherent middle that once broke
// assets.msn.com).
func TestIPv6DisabledIsCoherent(t *testing.T) {
	withConnectionPrefs(t, ConnectionSettings{DNSIPv6: boolPtr(false)})
	m := representativeFullProfile()
	if err := finalizeRuntimeConfigPipeline(m, t.TempDir(), 54333, 9097, "secret", "tun", true, true); err != nil {
		t.Fatalf("pipeline error: %v", err)
	}
	if m["ipv6"] != false {
		t.Errorf("top-level ipv6 = %v, want false (user opted out)", m["ipv6"])
	}
	if d := dnsMap(t, m); d["ipv6"] != false {
		t.Errorf("dns.ipv6 = %v, want false (mirrors top-level)", d["ipv6"])
	}
}
