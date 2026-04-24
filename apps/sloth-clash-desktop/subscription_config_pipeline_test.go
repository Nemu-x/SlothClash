package main

import (
	"os"
	"path/filepath"
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

// Regression: subscriptions that bake trailing whitespace into a proxy name
// (and reference the same whitespace-sensitive string from a proxy-group) must
// round-trip verbatim. Earlier versions trimmed only the group reference,
// causing Mihomo to report "'<name>' not found" on preflight.
func TestNormalizeProxyGroupRefsPreservesTrailingWhitespaceVerbatim(t *testing.T) {
	t.Parallel()
	const proxyName = "🇦🇪 Intermark.Global [vless - grpc] " // trailing space on purpose
	m := map[string]any{
		"proxy-providers": map[string]any{
			"sub1": map[string]any{"type": "http"},
		},
		"proxies": []any{
			map[string]any{"name": proxyName, "type": "vless"},
			map[string]any{"name": "Clean Proxy", "type": "vless"},
		},
		"proxy-groups": []any{
			map[string]any{
				"name":    "PROXY",
				"type":    "select",
				"proxies": []any{proxyName, "Clean Proxy"},
			},
			map[string]any{
				"name":    "♻️ Automatic",
				"type":    "url-test",
				"proxies": []any{proxyName, "Clean Proxy"},
			},
		},
	}
	normalizeProxyGroupRefs(m)
	groups := m["proxy-groups"].([]any)
	for _, g := range groups {
		gm := g.(map[string]any)
		got := gm["proxies"].([]any)
		if len(got) != 2 {
			t.Fatalf("group %q: expected 2 proxies kept, got %d: %#v", gm["name"], len(got), got)
		}
		if got[0].(string) != proxyName {
			t.Fatalf("group %q: trailing whitespace stripped from reference; got %q want %q", gm["name"], got[0], proxyName)
		}
	}
	if err := validateProxyGroupRefs(m); err != nil {
		t.Fatalf("validateProxyGroupRefs should accept verbatim trailing-space reference; got: %v", err)
	}
}

func TestValidateProxyGroupRefsRejectsRealMismatch(t *testing.T) {
	t.Parallel()
	m := map[string]any{
		"proxies": []any{
			map[string]any{"name": "Real Proxy", "type": "vless"},
		},
		"proxy-groups": []any{
			map[string]any{
				"name":    "PROXY",
				"type":    "select",
				"proxies": []any{"Real Proxy", "Ghost Proxy"},
			},
		},
	}
	if err := validateProxyGroupRefs(m); err == nil {
		t.Fatalf("expected error for real unknown proxy reference")
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

// Transient subscription fetch failures (TUN up-race, origin 5xx, captive
// portal, VPN flap) must not regress a working full profile into Sloth's
// bare `sub1 + Manual` fallback. The on-disk subscription body cache exists
// exactly for this: tryWriteMergedFullProfile writes it after every good
// fetch and reads it back when the HTTP call fails.
func TestSubscriptionBodyCacheRoundtrip(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	body := []byte("proxies:\n  - {name: X}\nproxy-groups:\n  - {name: Y, type: select}\n")

	// Missing cache returns nil — clean slate on first ever connect.
	if got := readSubscriptionBodyCache(dir); got != nil {
		t.Fatalf("cache must be nil before any write; got %d bytes", len(got))
	}

	writeSubscriptionBodyCache(dir, body)

	path := subscriptionBodyCachePath(dir)
	if _, err := os.Stat(path); err != nil {
		t.Fatalf("expected cache file at %s, got err=%v", path, err)
	}
	got := readSubscriptionBodyCache(dir)
	if string(got) != string(body) {
		t.Fatalf("cache readback mismatch: got %q, want %q", got, body)
	}
}

// An empty or blank body must never overwrite a good cache entry — otherwise
// a subscription server that briefly returns 200 with empty content would
// poison the fallback and force us into bare Manual.
func TestSubscriptionBodyCacheIgnoresEmptyWrites(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	good := []byte("proxy-groups: [{name: Main, type: select, proxies: [DIRECT]}]\nrules: [MATCH,Main]\n")
	writeSubscriptionBodyCache(dir, good)

	writeSubscriptionBodyCache(dir, nil)
	writeSubscriptionBodyCache(dir, []byte{})

	if got := string(readSubscriptionBodyCache(dir)); got != string(good) {
		t.Fatalf("empty writes must not clobber cache: got %q", got)
	}

	// Sanity: cache file lives next to the runtime config.yaml so it travels
	// with the per-profile dataDir (wiped when a profile is removed, reused
	// on reconnect).
	if filepath.Base(subscriptionBodyCachePath(dir)) == "" {
		t.Fatalf("cache path must be a concrete filename, got empty base")
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

