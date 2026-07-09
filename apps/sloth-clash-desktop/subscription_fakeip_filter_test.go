package main

import "testing"

// A subscription without any dns block must still get the fake-ip safety filter,
// otherwise captive-portal probes / NTP / *.lan resolve to 198.18.x and the OS
// looks broken (audit finding TO1 / CP-A).
func TestEnsureDefaultDNSForTun_FillsFakeIPFilterWhenNoDNSBlock(t *testing.T) {
	m := map[string]any{}
	ensureDefaultDNSForTun(m)

	dns, ok := m["dns"].(map[string]any)
	if !ok {
		t.Fatalf("expected a dns block, got %T", m["dns"])
	}
	filter, ok := dns["fake-ip-filter"].([]any)
	if !ok || len(filter) == 0 {
		t.Fatalf("expected a non-empty fake-ip-filter, got %#v", dns["fake-ip-filter"])
	}
	if !containsAny(filter, "www.msftconnecttest.com") {
		t.Errorf("captive-portal probe missing from filter: %#v", filter)
	}
	if !containsAny(filter, "*.lan") {
		t.Errorf("*.lan missing from filter: %#v", filter)
	}
	if got, _ := dns["fake-ip-filter-mode"].(string); got != "blacklist" {
		t.Errorf("fake-ip-filter-mode = %q, want blacklist", got)
	}
}

// A subscription that ships dns but omits fake-ip-filter gets it topped up.
func TestEnsureDefaultDNSForTun_FillsFakeIPFilterWhenPartialDNS(t *testing.T) {
	m := map[string]any{
		"dns": map[string]any{
			"enable":        true,
			"enhanced-mode": "fake-ip",
		},
	}
	ensureDefaultDNSForTun(m)

	dns := m["dns"].(map[string]any)
	filter, ok := dns["fake-ip-filter"].([]any)
	if !ok || len(filter) == 0 {
		t.Fatalf("expected fake-ip-filter to be filled, got %#v", dns["fake-ip-filter"])
	}
	if got, _ := dns["fake-ip-filter-mode"].(string); got != "blacklist" {
		t.Errorf("fake-ip-filter-mode = %q, want blacklist", got)
	}
}

// A subscription's OWN fake-ip-filter must never be clobbered (verge-like:
// fill only what is missing).
func TestEnsureDefaultDNSForTun_KeepsSubscriptionFakeIPFilter(t *testing.T) {
	m := map[string]any{
		"dns": map[string]any{
			"enable":         true,
			"enhanced-mode":  "fake-ip",
			"fake-ip-filter": []any{"custom.example"},
		},
	}
	ensureDefaultDNSForTun(m)

	filter := m["dns"].(map[string]any)["fake-ip-filter"].([]any)
	if len(filter) != 1 || filter[0] != "custom.example" {
		t.Fatalf("subscription filter was overwritten: %#v", filter)
	}
}

// Non-fake-ip (redir-host) mode must not gain a fake-ip filter.
func TestEnsureDefaultDNSForTun_NoFilterOutsideFakeIP(t *testing.T) {
	m := map[string]any{
		"dns": map[string]any{
			"enable":        true,
			"enhanced-mode": "redir-host",
		},
	}
	ensureDefaultDNSForTun(m)

	if _, present := m["dns"].(map[string]any)["fake-ip-filter"]; present {
		t.Fatal("fake-ip-filter must not be added in redir-host mode")
	}
}

func containsAny(list []any, want string) bool {
	for _, v := range list {
		if s, ok := v.(string); ok && s == want {
			return true
		}
	}
	return false
}
