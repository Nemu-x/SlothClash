package main

import (
	"reflect"
	"testing"
)

func chainTestConfig(groups ...map[string]any) map[string]any {
	gs := []any{map[string]any{"name": "SEL", "type": "select", "proxies": []any{"p1", "p2"}}}
	for _, g := range groups {
		gs = append(gs, g)
	}
	return map[string]any{
		"proxies": []any{
			map[string]any{"name": "p1", "type": "ss"},
			map[string]any{"name": "p2", "type": "ss"},
			map[string]any{"name": "p3", "type": "ss"},
		},
		"proxy-groups": gs,
	}
}

func chain(name string, hops ...any) map[string]any {
	return map[string]any{"name": name, "type": "chain", "proxies": hops}
}

func groupByName(m map[string]any, name string) map[string]any {
	for _, g := range m["proxy-groups"].([]any) {
		if gm := g.(map[string]any); gm["name"] == name {
			return gm
		}
	}
	return nil
}

func isRejectSelector(g map[string]any) bool {
	return g != nil && g["type"] == "select" && reflect.DeepEqual(g["proxies"], []any{"REJECT"})
}

func TestNeutralizeBrokenChainGroups(t *testing.T) {
	cases := map[string]struct {
		group  map[string]any
		broken bool
	}{
		"valid":              {chain("C", "p1", "p2", "p3"), false},
		"group hop":          {chain("C", "SEL", "p3"), false},
		"builtin first":      {chain("C", "DIRECT", "p1"), false},
		"unknown hop":        {chain("C", "p1", "nope"), true},
		"one hop":            {chain("C", "p1"), true},
		"self":               {chain("C", "p1", "C"), true},
		"duplicate":          {chain("C", "p1", "p2", "p1"), true},
		"builtin later":      {chain("C", "p1", "DIRECT"), true},
		"non-string hop":     {chain("C", "p1", 7), true},
		"use is unsupported": {map[string]any{"name": "C", "type": "chain", "proxies": []any{"p1", "p2"}, "use": []any{"x"}}, true},
	}
	for name, c := range cases {
		m := chainTestConfig(c.group)
		before := deepCopyValue(c.group)
		neutralizeBrokenChainGroups(m)
		g := groupByName(m, "C")
		if c.broken && !isRejectSelector(g) {
			t.Errorf("%s: want a REJECT selector, got %#v", name, g)
		}
		if !c.broken && !reflect.DeepEqual(g, before) {
			t.Errorf("%s: a valid chain must be left exactly as written, got %#v", name, g)
		}
	}
}

func TestNeutralizeBrokenChainCascades(t *testing.T) {
	m := chainTestConfig(
		chain("BROKEN", "p1", "missing"),
		chain("THROUGH", "p3", "BROKEN"), // later hop is the broken chain: core would refuse
		chain("FROM", "BROKEN", "p3"),    // first hop may be anything that exists
	)
	neutralizeBrokenChainGroups(m)
	if !isRejectSelector(groupByName(m, "BROKEN")) || !isRejectSelector(groupByName(m, "THROUGH")) {
		t.Fatalf("broken chain and the chain through it must both route to REJECT: %#v", m["proxy-groups"])
	}
	if g := groupByName(m, "FROM"); g["type"] != "chain" {
		t.Fatalf("a chain starting at a neutralised group stays a chain: %#v", g)
	}
}

// The generic group cleanup must never shorten a chain, and a broken chain must
// not fail the rest of the subscription.
func TestChainGroupsThroughPipeline(t *testing.T) {
	withConnectionPrefs(t, ConnectionSettings{})
	m := chainTestConfig(chain("GOOD", "p1", "SEL", "p3"), chain("BAD", "p1", "gone"))
	m["rules"] = []any{"DOMAIN,a.example,BAD", "MATCH,GOOD"}
	if err := finalizeRuntimeConfigPipeline(m, t.TempDir(), 54333, 9097, "secret", "tun", true, true); err != nil {
		t.Fatalf("pipeline: %v", err)
	}
	if got := groupByName(m, "GOOD")["proxies"]; !reflect.DeepEqual(got, []any{"p1", "SEL", "p3"}) {
		t.Errorf("GOOD hops = %#v, want exactly p1, SEL, p3", got)
	}
	if !isRejectSelector(groupByName(m, "BAD")) {
		t.Errorf("BAD = %#v, want a REJECT selector", groupByName(m, "BAD"))
	}
}

func deepCopyValue(v any) any {
	switch t := v.(type) {
	case map[string]any:
		out := make(map[string]any, len(t))
		for k, val := range t {
			out[k] = deepCopyValue(val)
		}
		return out
	case []any:
		out := make([]any, len(t))
		for i, val := range t {
			out[i] = deepCopyValue(val)
		}
		return out
	default:
		return v
	}
}
