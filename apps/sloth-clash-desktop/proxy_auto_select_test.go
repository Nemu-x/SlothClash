package main

import (
	"embed"
	"strings"
	"testing"
)

func newAppForProxyPickTest(t *testing.T) *App {
	t.Helper()
	var b embed.FS
	return NewApp(b)
}

// Restoring the per-profile sticky pick is the ONLY thing our Connect path
// does automatically now. No anchor derivation, no first-safe fallback.
// If the user manually picked "MainGroup" before, and it still exists in
// /proxies, we re-surface it on the next connect so the Proxies screen
// keeps highlighting the user's own choice.
func TestRestoreStickyGroupPicksPersistedChoice(t *testing.T) {
	t.Parallel()
	a := newAppForProxyPickTest(t)
	a.profiles = []Profile{
		{ID: "p1", Name: "subA", Type: "subscription", LastGoodGroup: "MainGroup"},
	}
	a.state.Profile.Profiles = a.profiles
	a.state.Profile.ActiveProfileID = "p1"
	a.state.Proxy.Groups = []ProxyGroup{
		{Name: "♻️ Automatic", Type: "url-test", Proxies: []string{"n1"}},
		{Name: "MainGroup", Type: "select", Proxies: []string{"n1"}},
		{Name: "ESP", Type: "select", Proxies: []string{"n1"}},
	}
	a.state.Proxy.ActiveGroup = ""

	a.restoreStickyGroupLocked()

	if got := a.state.Proxy.ActiveGroup; got != "MainGroup" {
		t.Fatalf("ActiveGroup = %q, want MainGroup (sticky restore)", got)
	}
}

// No sticky on profile → ActiveGroup stays empty. This is the first-ever
// connect case: UI shows "—" until the user clicks a group.
func TestRestoreStickyGroupLeavesActiveGroupEmptyWhenNoSticky(t *testing.T) {
	t.Parallel()
	a := newAppForProxyPickTest(t)
	a.profiles = []Profile{
		{ID: "p1", Name: "subA", Type: "subscription"},
	}
	a.state.Profile.Profiles = a.profiles
	a.state.Profile.ActiveProfileID = "p1"
	a.state.Proxy.Groups = []ProxyGroup{
		{Name: "Auto", Type: "url-test", Proxies: []string{"n1"}},
		{Name: "Manual", Type: "select", Proxies: []string{"n1"}},
	}
	a.state.Proxy.ActiveGroup = ""

	a.restoreStickyGroupLocked()

	if got := a.state.Proxy.ActiveGroup; got != "" {
		t.Fatalf("ActiveGroup = %q, want \"\" (no sticky, no auto-pick)", got)
	}
}

// Subscription dropped the sticky group → don't force it. Leave ActiveGroup
// as-is (empty or whatever the mode-switch code already set).
func TestRestoreStickyGroupDropsMissingSticky(t *testing.T) {
	t.Parallel()
	a := newAppForProxyPickTest(t)
	a.profiles = []Profile{
		{ID: "p1", Name: "subA", Type: "subscription", LastGoodGroup: "MainGroup"},
	}
	a.state.Profile.Profiles = a.profiles
	a.state.Profile.ActiveProfileID = "p1"
	a.state.Proxy.Groups = []ProxyGroup{
		{Name: "Auto", Type: "url-test", Proxies: []string{"n1"}},
		{Name: "Manual", Type: "select", Proxies: []string{"n1"}},
	}
	a.state.Proxy.ActiveGroup = ""

	a.restoreStickyGroupLocked()

	if got := a.state.Proxy.ActiveGroup; got != "" {
		t.Fatalf("ActiveGroup = %q, want \"\" (MainGroup missing, no fallback)", got)
	}
}

// Built-in policy tokens (GLOBAL / DIRECT / REJECT / PASS) must not be
// restored even if they somehow made it into LastGoodGroup.
func TestRestoreStickyGroupFiltersBuiltinTokens(t *testing.T) {
	t.Parallel()
	for _, tok := range []string{"GLOBAL", "DIRECT", "REJECT", "PASS"} {
		tok := tok
		t.Run(tok, func(t *testing.T) {
			t.Parallel()
			a := newAppForProxyPickTest(t)
			a.profiles = []Profile{
				{ID: "p1", Name: "subA", Type: "subscription", LastGoodGroup: tok},
			}
			a.state.Profile.Profiles = a.profiles
			a.state.Profile.ActiveProfileID = "p1"
			a.state.Proxy.Groups = []ProxyGroup{
				{Name: tok, Type: "selector", Proxies: []string{"Manual"}},
				{Name: "Manual", Type: "select", Proxies: []string{"n1"}},
			}
			a.state.Proxy.ActiveGroup = ""

			a.restoreStickyGroupLocked()

			if got := a.state.Proxy.ActiveGroup; got != "" {
				t.Fatalf("ActiveGroup = %q, want \"\" (built-in %s must not be surfaced)", got, tok)
			}
		})
	}
}

// Profile memory is per-profile. Profile A's MainGroup must not bleed into
// profile B, even if they happen to share group names.
func TestRestoreStickyGroupIsPerProfile(t *testing.T) {
	t.Parallel()
	a := newAppForProxyPickTest(t)
	a.profiles = []Profile{
		{ID: "p1", Name: "subA", Type: "subscription", LastGoodGroup: "MainGroup"},
		{ID: "p2", Name: "subB", Type: "subscription"},
	}
	a.state.Profile.Profiles = a.profiles
	a.state.Profile.ActiveProfileID = "p2"
	a.state.Proxy.Groups = []ProxyGroup{
		{Name: "MainGroup", Type: "select", Proxies: []string{"n1"}},
		{Name: "UK", Type: "select", Proxies: []string{"n1"}},
	}

	a.restoreStickyGroupLocked()

	if got := a.state.Proxy.ActiveGroup; got != "" {
		t.Fatalf("ActiveGroup = %q, want \"\" (profile p2 has no sticky)", got)
	}
}

// Sticky restore must be idempotent — calling it repeatedly with the same
// inputs converges on the same ActiveGroup without flipping.
func TestRestoreStickyGroupIsIdempotent(t *testing.T) {
	t.Parallel()
	a := newAppForProxyPickTest(t)
	a.profiles = []Profile{
		{ID: "p1", Name: "subA", Type: "subscription", LastGoodGroup: "MainGroup"},
	}
	a.state.Profile.Profiles = a.profiles
	a.state.Profile.ActiveProfileID = "p1"
	a.state.Proxy.Groups = []ProxyGroup{
		{Name: "MainGroup", Type: "select", Proxies: []string{"n1"}},
		{Name: "ESP", Type: "select", Proxies: []string{"n1"}},
	}

	for i := 0; i < 5; i++ {
		a.restoreStickyGroupLocked()
		if got := a.state.Proxy.ActiveGroup; got != "MainGroup" {
			t.Fatalf("run %d: ActiveGroup = %q, want MainGroup", i, got)
		}
	}
}

// SelectProxyGroup is the ONLY writer of sticky. It must update both the
// session state and the persisted profile field so subsequent connects /
// app restarts can rehydrate the pick.
func TestSelectProxyGroupPersistsOntoActiveProfile(t *testing.T) {
	tmp := t.TempDir()
	t.Setenv("APPDATA", tmp)
	t.Setenv("XDG_CONFIG_HOME", tmp)
	t.Setenv("HOME", tmp)

	a := newAppForProxyPickTest(t)
	a.profiles = []Profile{
		{ID: "p1", Name: "subA", Type: "subscription"},
	}
	a.state.Profile.Profiles = a.profiles
	a.state.Profile.ActiveProfileID = "p1"
	a.state.Proxy.Groups = []ProxyGroup{
		{Name: "MainGroup", Type: "select", Proxies: []string{"n1"}},
	}

	if _, err := a.SelectProxyGroup("MainGroup"); err != nil {
		t.Fatalf("SelectProxyGroup: %v", err)
	}
	if got := a.profiles[0].LastGoodGroup; got != "MainGroup" {
		t.Fatalf("profile.LastGoodGroup = %q, want MainGroup (must persist)", got)
	}
	if got := a.state.Proxy.LastGoodGroup; got != "MainGroup" {
		t.Fatalf("state.Proxy.LastGoodGroup = %q, want MainGroup", got)
	}
	if got := a.state.Proxy.ActiveGroup; got != "MainGroup" {
		t.Fatalf("state.Proxy.ActiveGroup = %q, want MainGroup", got)
	}
}

// Defensive: DIRECT / REJECT group names must be rejected by SelectProxyGroup
// because they aren't meaningful targets for manual routing.
func TestSelectProxyGroupRejectsBuiltins(t *testing.T) {
	t.Parallel()
	a := newAppForProxyPickTest(t)
	for _, name := range []string{"DIRECT", "REJECT", "direct", "reject"} {
		if _, err := a.SelectProxyGroup(name); err == nil {
			t.Fatalf("SelectProxyGroup(%q) must error", name)
		}
	}
	if _, err := a.SelectProxyGroup(""); err == nil {
		t.Fatalf("SelectProxyGroup(\"\") must error")
	}
}

// Used to guard against accidental regression: the restore helper must
// match group names case-insensitively but surface the /proxies-spelled
// name (emoji / CJK-preserved) because that's what React keys against.
func TestRestoreStickyGroupSurfacesExactGroupSpelling(t *testing.T) {
	t.Parallel()
	a := newAppForProxyPickTest(t)
	a.profiles = []Profile{
		{ID: "p1", Name: "subA", Type: "subscription", LastGoodGroup: "maingroup"},
	}
	a.state.Profile.Profiles = a.profiles
	a.state.Profile.ActiveProfileID = "p1"
	a.state.Proxy.Groups = []ProxyGroup{
		{Name: "MainGroup", Type: "select", Proxies: []string{"n1"}},
	}

	a.restoreStickyGroupLocked()

	if got := a.state.Proxy.ActiveGroup; got != "MainGroup" {
		t.Fatalf("ActiveGroup = %q, want MainGroup (exact group spelling preserved)", got)
	}
	// Defensive trim check so test failures surface trailing whitespace clearly.
	if strings.TrimSpace(a.state.Proxy.ActiveGroup) != a.state.Proxy.ActiveGroup {
		t.Fatalf("ActiveGroup %q has leading/trailing whitespace", a.state.Proxy.ActiveGroup)
	}
}
