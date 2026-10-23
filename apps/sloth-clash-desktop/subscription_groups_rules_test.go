package main

import (
	"strings"
	"testing"
)

func groupNames(m map[string]any) []string {
	var out []string
	for _, g := range m["proxy-groups"].([]any) {
		out = append(out, g.(map[string]any)["name"].(string))
	}
	return out
}

func ssProxy(name string) map[string]any {
	return map[string]any{"name": name, "type": "ss", "server": "192.0.2.1", "port": 443, "cipher": "aes-128-gcm", "password": "p"}
}

// A subscription's own url-test group called Auto is not the synthetic
// fallback when another group or a rule uses it (found by the Citrus compat
// suite: such a subscription failed generation with "references unknown
// proxy/group Auto").
func TestPruneKeepsSubscriptionAutoGroupInUse(t *testing.T) {
	withConnectionPrefs(t, ConnectionSettings{})
	for name, rules := range map[string][]any{
		"member of a group": {"MATCH,Proxy"},
		"target of a rule":  {"DOMAIN,a.example,Auto", "MATCH,Other"},
	} {
		m := map[string]any{
			"proxies": []any{ssProxy("a"), ssProxy("b")},
			"proxy-groups": []any{
				map[string]any{"name": "Auto", "type": "url-test", "proxies": []any{"a", "b"}},
				map[string]any{"name": "Proxy", "type": "select", "proxies": []any{"a", "Auto"}},
				map[string]any{"name": "Other", "type": "select", "proxies": []any{"b"}},
			},
			"rules": rules,
		}
		if name == "target of a rule" {
			m["proxy-groups"].([]any)[1].(map[string]any)["proxies"] = []any{"a"}
		}
		if err := finalizeRuntimeConfigPipeline(m, t.TempDir(), 7890, 9090, "s", "proxy", false, false); err != nil {
			t.Fatalf("%s: %v", name, err)
		}
		if got := strings.Join(groupNames(m), ","); !strings.Contains(got, "Auto") {
			t.Errorf("%s: the subscription's Auto group was pruned: %s", name, got)
		}
	}
}

// The synthetic Auto/Manual pair of a proxies-only subscription is still
// pruned once real groups exist, with MATCH moved to a real group.
func TestPruneStillDropsSyntheticAutoManual(t *testing.T) {
	withConnectionPrefs(t, ConnectionSettings{})
	m := map[string]any{
		"proxies": []any{ssProxy("a")},
		"proxy-providers": map[string]any{
			"sub1": map[string]any{"type": "inline", "payload": []any{ssProxy("p1")}},
		},
		"proxy-groups": []any{
			map[string]any{"name": "Auto", "type": "url-test", "use": []any{"sub1"}, "url": "http://cp.cloudflare.com/generate_204", "interval": 300},
			map[string]any{"name": "Manual", "type": "select", "proxies": []any{"Auto"}, "use": []any{"sub1"}},
			map[string]any{"name": "Real", "type": "select", "proxies": []any{"a"}},
		},
		"rules": []any{"MATCH,Manual"},
	}
	if err := finalizeRuntimeConfigPipeline(m, t.TempDir(), 7890, 9090, "s", "proxy", false, false); err != nil {
		t.Fatal(err)
	}
	if got := strings.Join(groupNames(m), ","); got != "Real" {
		t.Errorf("groups = %s, want only Real", got)
	}
	if rules := m["rules"].([]any); rules[len(rules)-1] != "MATCH,Real" {
		t.Errorf("MATCH not moved to the real group: %v", rules)
	}
}

func TestSubRuleTargetsSubRules(t *testing.T) {
	base := func(rule string) map[string]any {
		return map[string]any{
			"proxies":   []any{ssProxy("a")},
			"sub-rules": map[string]any{"udp-only": []any{"NETWORK,UDP,a", "MATCH,DIRECT"}},
			"rules":     []any{rule, "MATCH,DIRECT"},
		}
	}
	if err := validateRulePoliciesExist(base("SUB-RULE,(NETWORK,UDP),udp-only")); err != nil {
		t.Fatalf("a SUB-RULE naming a sub-rules block must pass: %v", err)
	}
	if err := validateRulePoliciesExist(base("SUB-RULE,(NETWORK,UDP),missing")); err == nil || !strings.Contains(err.Error(), "sub-rule") {
		t.Fatalf("an unknown sub-rules block must fail naming it, got %v", err)
	}
	// A proxy or group name is not a sub-rules block.
	if err := validateRulePoliciesExist(base("SUB-RULE,(NETWORK,UDP),a")); err == nil {
		t.Fatal("SUB-RULE targeting a proxy must fail")
	}
}
