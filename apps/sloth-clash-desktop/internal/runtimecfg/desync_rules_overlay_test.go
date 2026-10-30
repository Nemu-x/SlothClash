package runtimecfg

import (
	"encoding/json"
	"reflect"
	"testing"

	"SlothClashDesktop/internal/desync"
	"SlothClashDesktop/internal/desync/desynctest"
)

func overlayGroups(doc map[string]any) map[string]map[string]any {
	out := map[string]map[string]any{}
	for _, raw := range desyncSlice(doc["proxy-groups"]) {
		g := raw.(map[string]any)
		out[g["name"].(string)] = g
	}
	return out
}

func TestDesyncVPNTarget(t *testing.T) {
	node := map[string]any{"name": "node-1", "type": "ss"}
	selector := func(name string, members ...any) map[string]any {
		return map[string]any{"name": name, "type": "select", "proxies": members}
	}
	cases := []struct {
		name    string
		rules   []any
		groups  []any
		proxies []any
		want    string
	}{
		{"match group", []any{"DOMAIN,a.example,DIRECT", "MATCH,Proxy"}, []any{selector("Auto", "node-1"), selector("Proxy", "Auto", "DIRECT")}, []any{node}, "Proxy"},
		{"match proxy", []any{"MATCH, node-1"}, nil, []any{node}, "node-1"},
		// A catch-all of DIRECT (split profiles): the first visible group
		// that reaches a server.
		{"match direct", []any{"MATCH,DIRECT"}, []any{
			map[string]any{"name": "Hidden", "type": "select", "hidden": true, "proxies": []any{"node-1"}},
			selector("Direct only", "DIRECT", "REJECT"),
			selector("Proxy", "node-1"),
		}, []any{node}, "Proxy"},
		{"provider group", []any{"MATCH,DIRECT"}, []any{map[string]any{"name": "From provider", "type": "url-test", "use": []any{"p"}}}, nil, "From provider"},
		{"unknown match target", []any{"MATCH,Gone"}, []any{selector("Proxy", "node-1")}, []any{node}, "Proxy"},
		{"nothing", []any{"MATCH,DIRECT"}, nil, nil, ""},
	}
	for _, c := range cases {
		if got := desyncVPNTarget(c.rules, c.groups, c.proxies); got != c.want {
			t.Errorf("%s: %q, want %q", c.name, got, c.want)
		}
	}
}

func TestDesyncRulesOverlay(t *testing.T) {
	profile := []any{"DOMAIN-SUFFIX,example.org,Proxy", "MATCH,Proxy"}
	doc := map[string]any{
		"proxies":      []any{map[string]any{"name": "node-1", "type": "ss"}},
		"proxy-groups": []any{map[string]any{"name": "Proxy", "type": "select", "proxies": []any{"node-1", "DIRECT"}}},
		"rules":        append([]any(nil), profile...),
	}
	settings := DesyncSettings{Enabled: true, YouTube: true, Caps: desynctest.Windows(), Custom: []DesyncCustomRoute{
		{ID: "p1", Kind: DesyncKindProcess, Process: "Telegram.exe", Target: DesyncTargetVPN},
		{ID: "s1", Kind: DesyncKindSuffix, Domain: "example.org", Target: DesyncTargetAuto},
		{ID: "d1", Kind: DesyncKindDomain, Domain: "example.org", Target: DesyncTargetDirect},
		{ID: "v1", Kind: DesyncKindSuffix, Domain: "www.youtube.com", Target: DesyncTargetVPN},
		{ID: "p2", Kind: DesyncKindProcess, Process: "chrome.exe", Target: DesyncTargetAuto},
	}}
	applyDesyncOverlay(doc, settings, nil, nil)
	groups := overlayGroups(doc)
	if g := groups[desync.CustomRoute("p1")]; !reflect.DeepEqual(g["proxies"], []string{"Proxy"}) || g["hidden"] != true {
		t.Fatalf("VPN group: %v", g)
	}
	if g := groups[desync.CustomRoute("d1")]; !reflect.DeepEqual(g["proxies"], []string{"DIRECT"}) {
		t.Fatalf("direct group: %v", g)
	}
	first := desync.Candidates("custom", desynctest.Windows())[0]
	if g := groups[desync.CustomRoute("p2")]; !reflect.DeepEqual(g["proxies"], []string{first.Name(desync.CustomRoute("p2")), "PASS"}) {
		t.Fatalf("application group: %v", g)
	}
	rules := desyncSlice(doc["rules"])
	youtube := desync.Rules(desync.YouTubeDomains, desync.YouTubeRoute)
	want := []any{
		// The deepest domain first; at equal depth the exact domain
		// before the suffix.
		"DOMAIN-SUFFIX,www.youtube.com," + desync.CustomRoute("v1"),
		"DOMAIN,example.org," + desync.CustomRoute("d1"),
		desync.MatchRules("DOMAIN-SUFFIX", "example.org", desync.CustomRoute("s1"))[0],
		desync.MatchRules("DOMAIN-SUFFIX", "example.org", desync.CustomRoute("s1"))[1],
	}
	for _, r := range youtube {
		want = append(want, r)
	}
	want = append(want,
		"PROCESS-NAME,Telegram.exe,"+desync.CustomRoute("p1"),
		desync.MatchRules("PROCESS-NAME", "chrome.exe", desync.CustomRoute("p2"))[0],
		desync.MatchRules("PROCESS-NAME", "chrome.exe", desync.CustomRoute("p2"))[1],
	)
	want = append(want, profile...)
	if !reflect.DeepEqual(rules, want) {
		t.Fatalf("rules:\n got %v\nwant %v", rules, want)
	}
	// A second pass (after a user script) replaces its own output.
	before, _ := json.Marshal(doc)
	applyDesyncOverlay(doc, settings, nil, nil)
	if after, _ := json.Marshal(doc); string(before) != string(after) {
		t.Fatal("rules overlay is not idempotent")
	}
	applyDesyncOverlay(doc, DesyncSettings{}, nil, nil)
	if !reflect.DeepEqual(doc["rules"], profile) || len(desyncSlice(doc["proxy-groups"])) != 1 || len(desyncSlice(doc["proxies"])) != 1 {
		t.Fatalf("disable did not restore the profile: %v", doc)
	}
}

// Without a main group (a Desync-only profile, where the API refuses VPN
// rules anyway) a VPN rule falls through to the profile's rules.
func TestDesyncVPNRuleWithoutMainGroupPasses(t *testing.T) {
	doc := desyncOnlyBase()
	applyDesyncOverlay(doc, DesyncSettings{Enabled: true, Caps: desynctest.Windows(), Custom: []DesyncCustomRoute{
		{ID: "v1", Kind: DesyncKindSuffix, Domain: "example.org", Target: DesyncTargetVPN},
	}}, nil, nil)
	if g := overlayGroups(doc)[desync.CustomRoute("v1")]; !reflect.DeepEqual(g["proxies"], []string{"PASS"}) {
		t.Fatalf("group: %v", g)
	}
}
