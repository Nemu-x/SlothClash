package main

import "testing"

func TestSmokeMergeTemplateWithProviders(t *testing.T) {
	t.Parallel()
	base := map[string]any{
		"proxy-providers": map[string]any{
			"sub1": map[string]any{"type": "http"},
		},
		"proxy-groups": []any{
			map[string]any{
				"name": "MainGroup",
				"type": "select",
				"use":  []any{"sub1"},
			},
		},
		"rules": []any{
			"MATCH,MainGroup",
		},
	}
	tpl := `
prepend:
  rules:
    - "DOMAIN,example.invalid,MainGroup"
    - "AND,((IP-CIDR,203.0.113.0/24),(DST-PORT,22)),MainGroup"
append:
  proxy-groups:
    - name: Manual
      type: select
      proxies: [MainGroup, DIRECT]
`
	if err := applyProfileMergeTemplate(base, tpl); err != nil {
		t.Fatalf("applyProfileMergeTemplate failed: %v", err)
	}
	if err := finalizeRuntimeConfigPipeline(base, t.TempDir(), 7890, 9090, "secret", "tun", true, true); err != nil {
		t.Fatalf("finalizeRuntimeConfigPipeline failed: %v", err)
	}
}

func TestSmokeRejectsInvalidRulePolicyFromMergeTemplate(t *testing.T) {
	t.Parallel()
	base := map[string]any{
		"proxy-providers": map[string]any{
			"sub1": map[string]any{"type": "http"},
		},
		"proxy-groups": []any{
			map[string]any{
				"name": "MainGroup",
				"type": "select",
				"use":  []any{"sub1"},
			},
		},
		"rules": []any{
			"MATCH,MainGroup",
		},
	}
	tpl := `
prepend:
  rules:
    - "AND,((IP-CIDR,203.0.113.0/24),(DST-PORT,22)),ESP"
`
	if err := applyProfileMergeTemplate(base, tpl); err != nil {
		t.Fatalf("applyProfileMergeTemplate failed: %v", err)
	}
	if err := finalizeRuntimeConfigPipeline(base, t.TempDir(), 7890, 9090, "secret", "tun", true, true); err == nil {
		t.Fatalf("expected finalizeRuntimeConfigPipeline to reject unknown rule policy")
	}
}

func TestSmokeRejectsUnknownProxyProviderInProxyGroup(t *testing.T) {
	t.Parallel()
	cfg := map[string]any{
		"proxy-providers": map[string]any{
			"sub1": map[string]any{"type": "http"},
		},
		"proxy-groups": []any{
			map[string]any{
				"name": "Auto",
				"type": "url-test",
				"use":  []any{"sub2"},
			},
		},
		"rules": []any{"MATCH,Auto"},
	}
	if err := validateProxyGroupRefs(cfg); err == nil {
		t.Fatalf("expected validateProxyGroupRefs to fail on unknown provider")
	}
}
