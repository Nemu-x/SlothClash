package main

import (
	"reflect"
	"strings"
	"testing"
)

func asStrings(t *testing.T, v any) []string {
	t.Helper()
	list, ok := v.([]any)
	if !ok {
		t.Fatalf("expected []any, got %T (%v)", v, v)
	}
	out := make([]string, 0, len(list))
	for _, e := range list {
		s, ok := e.(string)
		if !ok {
			t.Fatalf("expected string element, got %T", e)
		}
		out = append(out, s)
	}
	return out
}

func TestApplyCorpVpnOverlay_NoopWhenInactive(t *testing.T) {
	// A full-tunnel / empty split must leave the config completely untouched so
	// config parity holds when the feature is off.
	m := map[string]any{
		"tun": map[string]any{"enable": true},
		"dns": map[string]any{"enhanced-mode": "fake-ip"},
	}
	before := map[string]any{
		"tun": map[string]any{"enable": true},
		"dns": map[string]any{"enhanced-mode": "fake-ip"},
	}
	applyCorpVpnOverlay(m, corpVpnSplit{}) // no routes
	if !reflect.DeepEqual(m, before) {
		t.Fatalf("inactive overlay mutated config:\n got %#v\nwant %#v", m, before)
	}

	// Full tunnel (DNS but no routes) is also inactive.
	applyCorpVpnOverlay(m, corpVpnSplit{DNSServers: []string{"10.0.0.1"}, DNSDomains: []string{"corp"}})
	if !reflect.DeepEqual(m, before) {
		t.Fatalf("full-tunnel overlay mutated config:\n got %#v\nwant %#v", m, before)
	}
}

func TestApplyCorpVpnOverlay_InjectsRoutesDNSAndFakeIPFilter(t *testing.T) {
	m := map[string]any{
		"tun": map[string]any{"enable": true},
		"dns": map[string]any{"enhanced-mode": "fake-ip"},
	}
	split := corpVpnSplit{
		Routes:     []string{"10.0.0.0/8", "172.16.0.0/12"},
		DNSServers: []string{"10.16.32.100"},
		DNSDomains: []string{"corp.example"},
		Tundev:     "utun4",
		Gateway:    "vpn.corp.example",
	}
	applyCorpVpnOverlay(m, split)

	tun := m["tun"].(map[string]any)
	if got := asStrings(t, tun["route-exclude-address"]); !reflect.DeepEqual(got, []string{"10.0.0.0/8", "172.16.0.0/12"}) {
		t.Fatalf("route-exclude-address = %v", got)
	}

	dns := m["dns"].(map[string]any)
	policy, ok := dns["nameserver-policy"].(map[string]any)
	if !ok {
		t.Fatalf("nameserver-policy missing: %#v", dns["nameserver-policy"])
	}
	// Resolver bound to the corp tunnel interface so the DNS dial egresses it.
	for _, key := range []string{"corp.example", "+.corp.example"} {
		got := asStrings(t, policy[key])
		if !reflect.DeepEqual(got, []string{"10.16.32.100#utun4"}) {
			t.Fatalf("nameserver-policy[%q] = %v, want interface-bound", key, got)
		}
	}

	// fake-ip-filter must include the corp domain (as +.domain) so it resolves real.
	filter := asStrings(t, dns["fake-ip-filter"])
	found := false
	gwFiltered := false
	for _, p := range filter {
		if p == "+.corp.example" {
			found = true
		}
		if p == "vpn.corp.example" {
			gwFiltered = true
		}
	}
	if !found {
		t.Fatalf("fake-ip-filter missing +.corp.example: %v", filter)
	}
	if !gwFiltered {
		t.Fatalf("gateway must be in fake-ip-filter for a real IP: %v", filter)
	}

	// The gateway must NOT be sent to the corp resolver (deadlock) — it gets an
	// exact-match public-DNS override that wins over the +.domain wildcard.
	gw := asStrings(t, policy["vpn.corp.example"])
	for _, ns := range gw {
		if strings.Contains(ns, "10.16.32.100") {
			t.Fatalf("gateway pinned to corp resolver (deadlock): %v", gw)
		}
	}
	if len(gw) == 0 {
		t.Fatalf("gateway must have a public-DNS override in nameserver-policy")
	}
}

func TestApplyCorpVpnOverlay_PreservesAndDedups(t *testing.T) {
	m := map[string]any{
		"tun": map[string]any{
			"enable":                true,
			"route-exclude-address": []any{"192.168.0.0/16", "10.0.0.0/8"},
		},
		"dns": map[string]any{
			"fake-ip-filter": []any{"*.lan", "+.corp.example"},
			"nameserver-policy": map[string]any{
				"corp.example": []any{"9.9.9.9"}, // pre-existing: must NOT be clobbered
			},
		},
	}
	split := corpVpnSplit{
		Routes:     []string{"10.0.0.0/8", "172.16.0.0/12"}, // 10.0.0.0/8 already present
		DNSServers: []string{"10.16.32.100"},
		DNSDomains: []string{"corp.example"},
	}
	applyCorpVpnOverlay(m, split)

	tun := m["tun"].(map[string]any)
	got := asStrings(t, tun["route-exclude-address"])
	want := []string{"192.168.0.0/16", "10.0.0.0/8", "172.16.0.0/12"} // deduped, order preserved
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("route-exclude-address = %v, want %v", got, want)
	}

	dns := m["dns"].(map[string]any)
	// pre-existing policy for the bare domain preserved; wildcard added.
	policy := dns["nameserver-policy"].(map[string]any)
	if got := asStrings(t, policy["corp.example"]); !reflect.DeepEqual(got, []string{"9.9.9.9"}) {
		t.Fatalf("existing nameserver-policy[corp.example] clobbered: %v", got)
	}
	if got := asStrings(t, policy["+.corp.example"]); !reflect.DeepEqual(got, []string{"10.16.32.100"}) {
		t.Fatalf("nameserver-policy[+.corp.example] = %v", got)
	}

	// fake-ip-filter deduped (was already present once).
	filter := asStrings(t, dns["fake-ip-filter"])
	count := 0
	for _, p := range filter {
		if p == "+.corp.example" {
			count++
		}
	}
	if count != 1 {
		t.Fatalf("fake-ip-filter has %d copies of +.corp.example, want 1: %v", count, filter)
	}
}
