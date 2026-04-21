package main

import "testing"

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

