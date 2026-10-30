package main

import (
	"fmt"
	"reflect"
	"strings"
	"testing"

	"SlothClashDesktop/internal/desync"
)

func TestDesyncRuleNormalization(t *testing.T) {
	cases := []struct {
		kind, value, route string
		domain, process    string
		code               string
	}{
		// Sites through Desync: the site rules.
		{kind: "suffix", value: "https://Example.ORG/path", route: "desync", domain: "example.org"},
		{kind: "domain", value: "login.example.org:443", route: "desync", domain: "login.example.org"},
		{kind: "suffix", value: "ru", route: "desync", code: "public_suffix"},
		{kind: "suffix", value: "co.uk", route: "desync", code: "public_suffix"},
		{kind: "suffix", value: "music.youtube.com", route: "desync", code: "builtin"},
		{kind: "suffix", value: "*.example.org", route: "desync", code: "wildcard"},
		{kind: "suffix", value: "1.2.3.4", route: "desync", code: "ip"},
		{kind: "suffix", value: "", route: "desync", code: "empty"},
		// VPN and direct: a whole zone and a built-in service's domain are
		// fine for a suffix; an exact domain must still be a name.
		{kind: "suffix", value: "ru", route: "vpn", domain: "ru"},
		{kind: "suffix", value: "co.uk", route: "direct", domain: "co.uk"},
		{kind: "suffix", value: "пример.рф", route: "direct", domain: "xn--e1afmkfd.xn--p1ai"},
		{kind: "suffix", value: "music.youtube.com", route: "vpn", domain: "music.youtube.com"},
		{kind: "domain", value: "ru", route: "vpn", code: "public_suffix"},
		{kind: "domain", value: "bad_label.example", route: "direct", code: "invalid"},
		{kind: "suffix", value: "[::1]", route: "vpn", code: "ip"},
		// Applications.
		{kind: "process", value: "Telegram.exe", route: "vpn", process: "Telegram.exe"},
		{kind: "process", value: "  telegram ", route: "desync", process: "telegram"},
		{kind: "process", value: `C:\Program Files\Telegram Desktop\Telegram.exe`, route: "direct", process: "Telegram.exe"},
		{kind: "process", value: `"/usr/bin/firefox"`, route: "direct", process: "firefox"},
		{kind: "process", value: "Google Chrome Helper", route: "direct", process: "Google Chrome Helper"},
		{kind: "process", value: "a,b.exe", route: "vpn", code: "invalid_process"},
		{kind: "process", value: "x(1).exe", route: "vpn", code: "invalid_process"},
		{kind: "process", value: "..", route: "vpn", code: "invalid_process"},
		{kind: "process", value: "bad\x01.exe", route: "vpn", code: "invalid_process"},
		{kind: "process", value: strings.Repeat("a", 256), route: "vpn", code: "invalid_process"},
		{kind: "process", value: "*.exe", route: "vpn", code: "wildcard"},
		{kind: "process", value: `C:\Apps\`, route: "vpn", code: "empty"},
		// The kind and route are a fixed vocabulary.
		{kind: "regex", value: "example.org", route: "vpn", code: "invalid_kind"},
		{kind: "suffix", value: "example.org", route: "proxy", code: "invalid_route"},
		{kind: "suffix", value: "example.org", route: "", code: "invalid_route"},
	}
	for _, c := range cases {
		got, err := normalizeDesyncRule(DesyncRuleInput{Kind: c.kind, Value: c.value, Route: c.route})
		if c.code != "" {
			if !isDesyncCode(err, c.code) {
				t.Errorf("%s %q %s: err %v, want %s", c.kind, c.value, c.route, err, c.code)
			}
			continue
		}
		want := DesyncCustomEntry{Kind: c.kind, Route: c.route, Domain: c.domain, Process: c.process}
		if err != nil || got != want {
			t.Errorf("%s %q %s: %+v %v, want %+v", c.kind, c.value, c.route, got, err, want)
		}
	}
}

func TestDesyncProcessCompletedForWindows(t *testing.T) {
	for _, c := range []struct{ name, goos, want string }{
		{"telegram", "windows", "telegram.exe"},
		{"Telegram.exe", "windows", "Telegram.exe"},
		{"telegram", "linux", "telegram"},
		{"Google Chrome", "darwin", "Google Chrome"},
	} {
		if got := desyncProcessForOS(c.name, c.goos); got != c.want {
			t.Errorf("%s on %s = %s, want %s", c.name, c.goos, got, c.want)
		}
	}
}

func rule(kind, value, route string) DesyncCustomEntry {
	e, err := normalizeDesyncRule(DesyncRuleInput{Kind: kind, Value: value, Route: route})
	if err != nil {
		panic(err)
	}
	return e
}

func TestDesyncRuleListChecks(t *testing.T) {
	entries, err := addDesyncRule(nil, rule("process", "Telegram.exe", "vpn"), false)
	if err != nil || len(entries) != 1 || !entries[0].Enabled || !validDesyncCustomID(entries[0].ID) {
		t.Fatalf("add: %+v %v", entries, err)
	}
	// One rule per thing matched, whatever its route; process names in
	// any case (the core compares them case-insensitively).
	if _, err := addDesyncRule(entries, rule("process", "TELEGRAM.EXE", "direct"), false); !isDesyncCode(err, "duplicate") {
		t.Fatalf("duplicate process: %v", err)
	}
	entries, _ = addDesyncRule(entries, rule("suffix", "example.org", "vpn"), false)
	if _, err := addDesyncRule(entries, rule("suffix", "example.org", "desync"), false); !isDesyncCode(err, "duplicate") {
		t.Fatalf("duplicate suffix: %v", err)
	}
	// The same name as an exact domain is a different rule.
	if _, err := addDesyncRule(entries, rule("domain", "example.org", "direct"), false); err != nil {
		t.Fatalf("domain next to suffix: %v", err)
	}
	// A Desync-only profile has no VPN.
	if _, err := addDesyncRule(nil, rule("suffix", "example.org", "vpn"), true); !isDesyncCode(err, "no_vpn") {
		t.Fatalf("vpn on Desync-only: %v", err)
	}
	if _, err := addDesyncRule(nil, rule("suffix", "example.org", "direct"), true); err != nil {
		t.Fatalf("direct on Desync-only: %v", err)
	}
	// A popular service already sends its domains through Desync; a VPN or
	// direct rule overrides it.
	service := []DesyncCustomEntry{{ID: "fb01", Service: "facebook", Enabled: true}}
	if _, err := addDesyncRule(service, rule("suffix", "fbcdn.net", "desync"), false); !isDesyncCode(err, "service_overlap") {
		t.Fatalf("Desync rule under a service: %v", err)
	}
	if _, err := addDesyncRule(service, rule("suffix", "fbcdn.net", "vpn"), false); err != nil {
		t.Fatalf("VPN rule under a service: %v", err)
	}
}

func TestDesyncRuleLimits(t *testing.T) {
	var entries []DesyncCustomEntry
	for i := range desyncCustomLimit {
		entries = append(entries, DesyncCustomEntry{ID: fmt.Sprintf("d%d", i), Domain: fmt.Sprintf("site%d.example", i), Kind: "suffix", Route: "desync", Enabled: true})
	}
	// The Desync cap counts the entries through Desync only.
	if _, err := addDesyncRule(entries, rule("process", "chrome.exe", "desync"), false); !isDesyncCode(err, "limit") {
		t.Fatalf("over the Desync cap: %v", err)
	}
	if _, err := addDesyncService(entries, "facebook"); !isDesyncCode(err, "limit") {
		t.Fatalf("service over the Desync cap: %v", err)
	}
	for i := len(entries); i < desyncRuleLimit; i++ {
		entries = append(entries, DesyncCustomEntry{ID: fmt.Sprintf("v%d", i), Domain: fmt.Sprintf("site%d.example", i), Kind: "suffix", Route: "vpn", Enabled: true})
	}
	if got := sanitizeDesyncCustom(entries); len(got) != desyncRuleLimit {
		t.Fatalf("sanitize kept %d of a full list", len(got))
	}
	if _, err := addDesyncRule(entries, rule("suffix", "one-more.example", "direct"), false); !isDesyncCode(err, "rule_limit") {
		t.Fatalf("over the rule cap: %v", err)
	}
	if got := sanitizeDesyncCustom(append(entries, DesyncCustomEntry{ID: "zz", Domain: "one-more.example", Kind: "suffix", Route: "direct", Enabled: true})); len(got) != desyncRuleLimit {
		t.Fatalf("sanitize kept %d past the cap", len(got))
	}
}

func TestDesyncRuleUpdate(t *testing.T) {
	entries := []DesyncCustomEntry{
		{ID: "a1", Domain: "example.org", Kind: "suffix", Route: "desync", Enabled: false},
		{ID: "b2", Service: "facebook", Enabled: true},
		{ID: "c3", Process: "steam.exe", Kind: "process", Route: "direct", Enabled: true},
	}
	next, err := updateDesyncRule(entries, "a1", rule("domain", "www.example.org", "vpn"), false)
	if err != nil {
		t.Fatal(err)
	}
	// In place, its on/off state kept, under a new ID: the old group,
	// calibration state and cached strategy were for what it matched.
	got := next[0]
	if got.ID == "a1" || got.Enabled || got.Kind != "domain" || got.Domain != "www.example.org" || got.Route != "vpn" || next[1] != entries[1] || next[2] != entries[2] {
		t.Fatalf("updated: %+v", next)
	}
	if entries[0].ID != "a1" {
		t.Fatal("the input list was changed")
	}
	// Unchanged: nothing to do, same ID.
	if same, err := updateDesyncRule(entries, "c3", rule("process", "steam.exe", "direct"), false); err != nil || same[2].ID != "c3" {
		t.Fatalf("unchanged: %+v %v", same, err)
	}
	// The rule may keep its own value under another route, but not take
	// another rule's.
	if _, err := updateDesyncRule(entries, "c3", rule("process", "STEAM.exe", "vpn"), false); err != nil {
		t.Fatalf("route change: %v", err)
	}
	if _, err := updateDesyncRule(entries, "a1", rule("process", "steam.exe", "vpn"), false); !isDesyncCode(err, "duplicate") {
		t.Fatalf("taking another rule's value: %v", err)
	}
	if _, err := updateDesyncRule(entries, "b2", rule("suffix", "example.com", "vpn"), false); !isDesyncCode(err, "not_found") {
		t.Fatalf("editing a service: %v", err)
	}
	if _, err := updateDesyncRule(entries, "a1", rule("suffix", "example.org", "vpn"), true); !isDesyncCode(err, "no_vpn") {
		t.Fatalf("vpn on Desync-only: %v", err)
	}
}

// Sites stored before rules had a kind and a route read as suffix rules
// through Desync; a hand-edited entry that does not fit its kind is dropped.
func TestDesyncRuleSanitize(t *testing.T) {
	in := []DesyncCustomEntry{
		{ID: "a1", Domain: "example.org", Enabled: true},                                            // legacy site: kept as suffix/desync
		{ID: "a2", Domain: "ru", Kind: "suffix", Route: "vpn", Enabled: true},                       // kept
		{ID: "a3", Domain: "ru", Kind: "suffix", Route: "desync", Enabled: true},                    // a zone through Desync
		{ID: "a4", Process: "Telegram.exe", Kind: "process", Route: "desync", Enabled: true},        // kept
		{ID: "a5", Process: "telegram.EXE", Kind: "process", Route: "vpn", Enabled: true},           // duplicate
		{ID: "a6", Process: "x.exe", Domain: "x.org", Kind: "process", Route: "vpn", Enabled: true}, // both values
		{ID: "a7", Domain: "example.net", Kind: "regex", Route: "vpn", Enabled: true},               // unknown kind
		{ID: "a8", Domain: "example.net", Kind: "suffix", Route: "proxy", Enabled: true},            // unknown route
		{ID: "a9", Process: "a,b", Kind: "process", Route: "direct", Enabled: true},                 // unsafe name
		{ID: "b1", Domain: "fbcdn.net", Kind: "suffix", Route: "vpn", Enabled: true},                // overrides the service: kept
		{ID: "b2", Domain: "fbcdn.net", Kind: "domain", Route: "desync", Enabled: true},             // under the service
		{ID: "b3", Service: "facebook", Enabled: true},                                              // kept
		{ID: "b4", Service: "x", Route: "vpn", Enabled: true},                                       // a service has no route
	}
	got := sanitizeDesyncCustom(in)
	site := in[0]
	site.Kind, site.Route = "suffix", "desync"
	want := []DesyncCustomEntry{site, in[1], in[3], in[9], in[11]}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("sanitize:\n got %+v\nwant %+v", got, want)
	}
}

func TestDesyncServiceKeepsVPNAndDirectRules(t *testing.T) {
	entries := []DesyncCustomEntry{
		{ID: "a1", Domain: "facebook.com", Kind: "suffix", Route: "desync", Enabled: true},
		{ID: "a2", Domain: "fbcdn.net", Kind: "suffix", Route: "vpn", Enabled: true},
		{ID: "a3", Domain: "m.facebook.com", Kind: "domain", Route: "direct", Enabled: true},
	}
	next, err := addDesyncService(entries, "facebook")
	if err != nil || len(next) != 3 || next[0].ID != "a2" || next[1].ID != "a3" || next[2].Service != "facebook" {
		t.Fatalf("add service: %+v %v", next, err)
	}
}

func TestDesyncRulesThroughTheApp(t *testing.T) {
	withDesyncCoreFeatures(t, "desync", "desync-outbound")
	a := desyncProfileApp(t, desyncPtr(DesyncSettings{Enabled: true, YouTube: true}), desyncPtr(DesyncSettings{Enabled: true}))
	status, err := a.AddDesyncRule(DesyncRuleInput{Kind: "suffix", Value: "example.org", Route: "vpn"})
	if err != nil || len(status.Settings.Custom) != 1 {
		t.Fatalf("add: %+v %v", status.Settings, err)
	}
	id := status.Settings.Custom[0].ID
	status, err = a.UpdateDesyncRule(id, DesyncRuleInput{Kind: "suffix", Value: "example.org", Route: "direct"})
	if err != nil || len(status.Settings.Custom) != 1 || status.Settings.Custom[0].Route != "direct" || status.Settings.Custom[0].ID == id {
		t.Fatalf("update: %+v %v", status.Settings, err)
	}
	if got := currentDesyncSettings().Custom; len(got) != 1 || got[0].Route != "direct" {
		t.Fatalf("live copy: %+v", got)
	}
	if _, err := a.UpdateDesyncRule("nope", DesyncRuleInput{Kind: "suffix", Value: "example.net", Route: "direct"}); !isDesyncCode(err, "not_found") {
		t.Fatalf("unknown id: %v", err)
	}
	if _, err := a.AddDesyncRule(DesyncRuleInput{Kind: "process", Value: "telegram", Route: "vpn"}); err != nil {
		t.Fatal(err)
	}
	if got := currentDesyncSettings().Custom[1]; got.Process != desyncProcessForOS("telegram", desyncGOOS) {
		t.Fatalf("process: %+v", got)
	}

	// The Desync-only profile refuses the VPN route, and a VPN rule
	// stored there anyway (a hand-edited file) is not used.
	a.mu.Lock()
	a.state.Profile.ActiveProfileID = "profile-2"
	a.syncActiveDesyncLocked()
	a.mu.Unlock()
	if _, err := a.AddDesyncRule(DesyncRuleInput{Kind: "suffix", Value: "example.org", Route: "vpn"}); !isDesyncCode(err, "no_vpn") {
		t.Fatalf("vpn on Desync-only: %v", err)
	}
	a.mu.Lock()
	a.profiles[1].Desync.Custom = []DesyncCustomEntry{
		{ID: "v1", Domain: "example.org", Kind: "suffix", Route: "vpn", Enabled: true},
		{ID: "d1", Domain: "example.net", Kind: "suffix", Route: "direct", Enabled: true},
	}
	a.syncActiveDesyncLocked()
	a.mu.Unlock()
	if got := currentDesyncSettings().Custom; len(got) != 1 || got[0].ID != "d1" {
		t.Fatalf("Desync-only rules: %+v", got)
	}

	// Drafts are a Desync-only profile's.
	draft := a.NewDesyncDraft()
	if _, err := a.DraftDesyncAddRule(draft, DesyncRuleInput{Kind: "suffix", Value: "example.org", Route: "vpn"}); !isDesyncCode(err, "no_vpn") {
		t.Fatalf("vpn in a draft: %v", err)
	}
	draft, err = a.DraftDesyncAddRule(draft, DesyncRuleInput{Kind: "process", Value: "Telegram.exe", Route: "desync"})
	if err != nil || len(draft.Custom) != 1 {
		t.Fatalf("draft add: %+v %v", draft, err)
	}
	draft, err = a.DraftDesyncUpdateRule(draft, draft.Custom[0].ID, DesyncRuleInput{Kind: "process", Value: "Telegram.exe", Route: "direct"})
	if err != nil || len(draft.Custom) != 1 || draft.Custom[0].Route != "direct" {
		t.Fatalf("draft update: %+v %v", draft, err)
	}
}

// Only popular services and domain rules through Desync are calibrated; an
// application rule through Desync reports the catalog default it uses.
func TestDesyncCalibrationServesOnlyProbeableRules(t *testing.T) {
	core := newCustomFakeCore(t, func(string, string) bool { return true })
	a, _ := customTestApp(t, core,
		DesyncCustomEntry{ID: "a1", Domain: "example.org", Kind: "suffix", Route: "desync", Enabled: true},
		DesyncCustomEntry{ID: "a2", Domain: "example.net", Kind: "suffix", Route: "vpn", Enabled: true},
		DesyncCustomEntry{ID: "a3", Process: "chrome.exe", Kind: "process", Route: "desync", Enabled: true},
		DesyncCustomEntry{ID: "a4", Process: "steam.exe", Kind: "process", Route: "direct", Enabled: true},
		DesyncCustomEntry{ID: "a5", Service: "facebook", Enabled: true},
	)
	var ids []string
	for _, e := range enabledDesyncCustom() {
		ids = append(ids, e.ID)
	}
	if !reflect.DeepEqual(ids, []string{"a1", "a5"}) {
		t.Fatalf("calibrated: %v", ids)
	}
	fixed := a.markDesyncFixed(1)
	if !reflect.DeepEqual(fixed, map[string]bool{"a3": true}) {
		t.Fatalf("fixed: %v", fixed)
	}
	state := a.GetDesyncAutoStatus().Custom["a3"]
	if state.Phase != "fixed" || state.Candidate != desync.Candidates("custom", desyncCaps)[0].ID || state.Route != "desync" {
		t.Fatalf("fixed state: %+v", state)
	}
	updated := state.Updated
	a.markDesyncFixed(1)
	if again := a.GetDesyncAutoStatus().Custom["a3"]; again.Updated != updated {
		t.Fatal("a steady fixed state was rewritten")
	}
}
