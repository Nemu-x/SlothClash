package main

import (
	"strings"
	"testing"
)

func TestSubscriptionDocIsFullProfileHeuristics(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name string
		in   map[string]any
		want bool
	}{
		{
			name: "rules only",
			in:   map[string]any{"rules": []any{"MATCH,DIRECT"}},
			want: true,
		},
		{
			name: "proxy groups only",
			in: map[string]any{
				"proxy-groups": []any{
					map[string]any{"name": "Main", "type": "select", "proxies": []any{"DIRECT"}},
				},
			},
			want: true,
		},
		{
			name: "dns only",
			in:   map[string]any{"dns": map[string]any{"enable": true}},
			want: true,
		},
		{
			name: "empty mapping",
			in:   map[string]any{},
			want: false,
		},
	}
	for _, tc := range cases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			got := subscriptionDocIsFullProfile(tc.in)
			if got != tc.want {
				t.Fatalf("subscriptionDocIsFullProfile() = %v, want %v", got, tc.want)
			}
		})
	}
}

func TestNormalizeProxyGroupRefsPrunesUnknownProxies(t *testing.T) {
	t.Parallel()
	m := map[string]any{
		"proxy-providers": map[string]any{
			"sub1": map[string]any{"type": "http"},
		},
		"proxy-groups": []any{
			map[string]any{
				"name": "Main",
				"type": "select",
				"use":  []any{"sub1"},
				"proxies": []any{
					"UNKNOWN_A",
					"DIRECT",
					"UNKNOWN_B",
				},
			},
		},
	}
	normalizeProxyGroupRefs(m)
	groups := m["proxy-groups"].([]any)
	g := groups[0].(map[string]any)
	got := g["proxies"].([]any)
	if len(got) != 1 || got[0] != "DIRECT" {
		t.Fatalf("normalized proxies = %#v, want [DIRECT]", got)
	}
}

func TestValidateRulePoliciesExistWithTrailingOptions(t *testing.T) {
	t.Parallel()
	m := map[string]any{
		"proxy-groups": []any{
			map[string]any{"name": "MainGroup", "type": "select", "proxies": []any{"DIRECT"}},
		},
		"rules": []any{
			"GEOIP,private,DIRECT,no-resolve",
			"MATCH,MainGroup",
		},
	}
	if err := validateRulePoliciesExist(m); err != nil {
		t.Fatalf("validateRulePoliciesExist should accept valid trailing options, got: %v", err)
	}
}

func TestValidateRulePoliciesExistRejectsUnknownPolicy(t *testing.T) {
	t.Parallel()
	m := map[string]any{
		"proxy-groups": []any{
			map[string]any{"name": "MainGroup", "type": "select", "proxies": []any{"DIRECT"}},
		},
		"rules": []any{
			"DOMAIN,www.vdsina.com,MainGroup",
			"AND,((IP-CIDR,136.244.104.123/32),(DST-PORT,22)),ESP",
		},
	}
	err := validateRulePoliciesExist(m)
	if err == nil {
		t.Fatalf("validateRulePoliciesExist should reject unknown policy")
	}
	if got := err.Error(); got == "" || !containsAll(got, []string{"unknown policy", "ESP"}) {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestExtractRulePolicyToken(t *testing.T) {
	t.Parallel()
	cases := []struct {
		in   string
		want string
	}{
		{in: "MATCH,MainGroup", want: "MainGroup"},
		{in: "GEOIP,private,DIRECT,no-resolve", want: "DIRECT"},
		{in: "AND,((IP-CIDR,136.244.104.123/32),(DST-PORT,22)),ESP", want: "ESP"},
		{in: "NETWORK,tcp", want: ""},
	}
	for _, tc := range cases {
		if got := extractRulePolicyToken(tc.in); got != tc.want {
			t.Fatalf("extractRulePolicyToken(%q)=%q want %q", tc.in, got, tc.want)
		}
	}
}

func TestNormalizeEscapedUnicodeStrings(t *testing.T) {
	t.Parallel()
	in := map[string]any{
		"proxies": []any{
			"\\U0001F996 Dinosaur",
			"\\u0052U-Node",
		},
	}
	outAny := normalizeEscapedUnicodeStrings(in)
	out, ok := outAny.(map[string]any)
	if !ok {
		t.Fatalf("unexpected type: %T", outAny)
	}
	arr, _ := out["proxies"].([]any)
	if len(arr) != 2 {
		t.Fatalf("unexpected proxies len: %d", len(arr))
	}
	if got := arr[0].(string); !strings.Contains(got, "🦖") {
		t.Fatalf("expected decoded emoji, got %q", got)
	}
	if got := arr[1].(string); got != "RU-Node" {
		t.Fatalf("expected decoded \\u sequence, got %q", got)
	}
}

func containsAll(s string, parts []string) bool {
	for _, p := range parts {
		if !strings.Contains(s, p) {
			return false
		}
	}
	return true
}

