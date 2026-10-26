package main

import (
	"encoding/json"
	"errors"
	"os"
	"strings"
	"testing"
	"time"
)

func TestCreateDesyncOnlyProfile(t *testing.T) {
	isolateAppDataRoot(t)
	a := &App{}
	a.state.Mode.Current = "global"
	state, err := a.CreateDesyncOnlyProfile("  ")
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	if len(state.Profile.Profiles) != 1 {
		t.Fatalf("profiles = %+v", state.Profile.Profiles)
	}
	p := state.Profile.Profiles[0]
	if p.Kind != profileKindDesyncOnly || p.Type != "local" || p.Name != "Desync only" || p.URL != "" || p.AutoUpdateEnabled {
		t.Fatalf("created profile = %+v", p)
	}
	if !validProfileID(p.ID) {
		t.Fatalf("id %q is not a well-formed profile id", p.ID)
	}
	// The first profile becomes active, and with it Rule mode.
	if state.Profile.ActiveProfileID != p.ID || state.Mode.Current != "rule" {
		t.Fatalf("active=%q mode=%q", state.Profile.ActiveProfileID, state.Mode.Current)
	}
	if !profileHasLocalConfig(p) {
		t.Fatal("a Desync-only profile must pass the no-URL guards")
	}

	// Persisted with its kind; a second one in the same second gets its own ID.
	path, err := profilesStorePath()
	if err != nil {
		t.Fatal(err)
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(raw), `"kind": "desync-only"`) {
		t.Fatalf("profiles.json lacks the kind:\n%s", raw)
	}
	state, err = a.CreateDesyncOnlyProfile("Second")
	if err != nil {
		t.Fatal(err)
	}
	if len(state.Profile.Profiles) != 2 || state.Profile.Profiles[0].ID == state.Profile.Profiles[1].ID {
		t.Fatalf("ids collide: %+v", state.Profile.Profiles)
	}
	// Not the first profile: the active one stays.
	if state.Profile.ActiveProfileID != p.ID {
		t.Fatalf("active moved to %q", state.Profile.ActiveProfileID)
	}
}

// Legacy profiles carry no kind and serialize exactly as before.
func TestProfileKindOmittedForLegacyProfiles(t *testing.T) {
	raw, err := json.Marshal(Profile{ID: "profile-1", Name: "sub", Type: "subscription", URL: "https://example.com/s"})
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(raw), "kind") {
		t.Fatalf("legacy profile serializes a kind: %s", raw)
	}
	if profileIsDesyncOnly(Profile{Type: "local"}) {
		t.Fatal("a profile without a kind is not Desync-only")
	}
}

func TestNewProfileIDLockedSkipsTakenIDs(t *testing.T) {
	now := time.Date(2026, 10, 26, 12, 0, 0, 0, time.UTC)
	a := &App{profiles: []Profile{{ID: "profile-20261026120000"}, {ID: "profile-20261026120001"}}}
	if got := a.newProfileIDLocked(now); got != "profile-20261026120002" {
		t.Fatalf("id = %q", got)
	}
}

// Every writer of a field a Desync-only profile does not have refuses a
// value; clearing (empty) stays accepted so generic save paths work.
func TestDesyncOnlyProfileRejectsSubscriptionFields(t *testing.T) {
	isolateAppDataRoot(t)
	const id = "profile-1"
	a := &App{profiles: []Profile{{ID: id, Name: "d", Type: "local", Kind: profileKindDesyncOnly}}}
	a.state.Profile.Profiles = a.profiles
	pair, err := a.GenerateAgeKeyPair("x25519")
	if err != nil {
		t.Fatal(err)
	}

	refused := map[string]func() error{
		"url":         func() error { _, err := a.UpdateProfileInfo(id, "d", "https://example.com/s"); return err },
		"age key":     func() error { _, err := a.SetProfileAgeSecretKey(id, pair.SecretKey); return err },
		"extend":      func() error { _, err := a.SetProfileMergeTemplate(id, "prepend: {}"); return err },
		"proxy":       func() error { _, err := a.SetProfileProxyTemplate(id, "append: {}"); return err },
		"rules":       func() error { _, err := a.SetProfileRulesTemplate(id, "append: {}"); return err },
		"script":      func() error { _, err := a.SetProfileScriptOverride(id, "function main(c){return c}"); return err },
		"config file": func() error { _, err := a.WriteProfileConfig(id, "mode: rule"); return err },
		"auto update": func() error { _, err := a.SetProfileAutoUpdate(id, true, 60); return err },
		"preview":     func() error { _, err := a.PreviewProfileScript(id, ""); return err },
		"refresh":     func() error { _, err := a.refreshProfileSubscription(id, false); return err },
	}
	for name, call := range refused {
		if err := call(); err == nil {
			t.Errorf("%s: accepted on a Desync-only profile", name)
		} else if name != "refresh" && !errors.Is(err, errDesyncOnlyProfileField) {
			t.Errorf("%s: err = %v, want errDesyncOnlyProfileField", name, err)
		}
	}
	if _, err := os.Stat(a.GetProfilePaths(id).ConfigPath); err == nil {
		t.Fatal("WriteProfileConfig wrote config.yaml before refusing")
	}

	accepted := map[string]func() error{
		"rename":           func() error { _, err := a.UpdateProfileInfo(id, "Renamed", ""); return err },
		"clear age key":    func() error { _, err := a.SetProfileAgeSecretKey(id, ""); return err },
		"clear extend":     func() error { _, err := a.SetProfileMergeTemplate(id, ""); return err },
		"clear script":     func() error { _, err := a.ClearProfileScriptOverride(id); return err },
		"auto update off":  func() error { _, err := a.SetProfileAutoUpdate(id, false, 60); return err },
		"clear rules tmpl": func() error { _, err := a.SetProfileRulesTemplate(id, "  "); return err },
	}
	for name, call := range accepted {
		if err := call(); err != nil {
			t.Errorf("%s: %v", name, err)
		}
	}
	got := a.profiles[0]
	if got.Name != "Renamed" || got.URL != "" || got.SkipAutoConfig || got.AutoUpdateEnabled || got.AutoUpdateIntervalMinutes != 0 {
		t.Fatalf("profile after the calls = %+v", got)
	}
}

func TestDesyncOnlyProfilePinsRuleMode(t *testing.T) {
	isolateAppDataRoot(t)
	a := &App{profiles: []Profile{
		{ID: "profile-1", Name: "sub", Type: "subscription", URL: "https://example.com/s"},
		{ID: "profile-2", Name: "d", Type: "local", Kind: profileKindDesyncOnly},
	}}
	a.state.Profile.Profiles = a.profiles
	a.state.Profile.ActiveProfileID = "profile-1"
	a.state.Mode.Current = "global"
	a.state.Proxy.ActiveGroup = "GLOBAL"

	// The subscription profile keeps Global.
	if _, err := a.SetMode("direct"); err != nil {
		t.Fatalf("SetMode on a subscription profile: %v", err)
	}
	if _, err := a.SetMode("global"); err != nil {
		t.Fatal(err)
	}
	a.state.Proxy.ActiveGroup = "GLOBAL"

	state, err := a.ActivateProfile("profile-2")
	if err != nil {
		t.Fatal(err)
	}
	if state.Mode.Current != "rule" || state.Proxy.ActiveGroup != "" {
		t.Fatalf("after activating a Desync-only profile: mode=%q activeGroup=%q", state.Mode.Current, state.Proxy.ActiveGroup)
	}
	for _, mode := range []string{"global", "direct"} {
		if _, err := a.SetMode(mode); !errors.Is(err, errDesyncOnlyMode) {
			t.Fatalf("SetMode(%s) err = %v, want errDesyncOnlyMode", mode, err)
		}
	}
	if _, err := a.SetMode("rule"); err != nil {
		t.Fatalf("SetMode(rule): %v", err)
	}

	// Loading a profiles.json whose active Desync-only profile was saved in
	// Global (hand edit, older build) comes up in Rule.
	a.state.Mode.Current = "global"
	if err := a.persistProfilesLocked(); err != nil {
		t.Fatal(err)
	}
	b := &App{}
	b.loadProfilesFromDisk()
	if b.state.Mode.Current != "rule" || b.state.Profile.ActiveProfileID != "profile-2" {
		t.Fatalf("loaded mode=%q active=%q", b.state.Mode.Current, b.state.Profile.ActiveProfileID)
	}
}

// A Global choice survives a visit to a Desync-only profile: the forced
// Rule mode ends when a profile with servers is active again, by activation
// or by deleting the active Desync-only profile, and across a restart.
func TestDesyncOnlyProfileRestoresGlobalOnLeave(t *testing.T) {
	isolateAppDataRoot(t)
	newApp := func() *App {
		a := &App{profiles: []Profile{
			{ID: "profile-1", Name: "sub", Type: "subscription", URL: "https://example.com/s"},
			{ID: "profile-2", Name: "d", Type: "local", Kind: profileKindDesyncOnly},
			{ID: "profile-3", Name: "sub2", Type: "subscription", URL: "https://example.com/t"},
		}}
		a.state.Profile.Profiles = a.profiles
		a.state.Profile.ActiveProfileID = "profile-1"
		a.state.Mode.Current = "rule"
		a.state.Mode.LastNonDirectMode = "rule"
		if _, err := a.SetMode("global"); err != nil {
			t.Fatal(err)
		}
		if _, err := a.ActivateProfile("profile-2"); err != nil {
			t.Fatal(err)
		}
		if a.state.Mode.Current != "rule" || a.state.Mode.LastNonDirectMode != "global" {
			t.Fatalf("on the Desync-only profile: mode=%q last=%q, want rule with global remembered", a.state.Mode.Current, a.state.Mode.LastNonDirectMode)
		}
		return a
	}

	a := newApp()
	if state, err := a.ActivateProfile("profile-3"); err != nil || state.Mode.Current != "global" {
		t.Fatalf("after leaving by activation: mode=%q err=%v, want global", state.Mode.Current, err)
	}

	a = newApp()
	if state, err := a.DeleteProfile("profile-2"); err != nil || state.Mode.Current != "global" {
		t.Fatalf("after deleting the active Desync-only profile: mode=%q err=%v, want global", state.Mode.Current, err)
	}

	// Saved while on the Desync-only profile, then loaded by a new run.
	a = newApp()
	b := &App{}
	b.loadProfilesFromDisk()
	if b.state.Mode.Current != "rule" || b.state.Mode.LastNonDirectMode != "global" {
		t.Fatalf("loaded mode=%q last=%q", b.state.Mode.Current, b.state.Mode.LastNonDirectMode)
	}
	if state, err := b.ActivateProfile("profile-1"); err != nil || state.Mode.Current != "global" {
		t.Fatalf("after a restart: mode=%q err=%v, want global", state.Mode.Current, err)
	}

	// A Rule choice stays Rule, and switching between profiles with servers
	// never changes the mode.
	c := &App{profiles: a.profiles}
	c.state.Profile.Profiles = c.profiles
	c.state.Profile.ActiveProfileID = "profile-2"
	c.state.Mode.Current = "rule"
	c.state.Mode.LastNonDirectMode = "rule"
	if state, _ := c.ActivateProfile("profile-1"); state.Mode.Current != "rule" {
		t.Fatalf("a Rule choice became %q", state.Mode.Current)
	}
	c.state.Mode.LastNonDirectMode = "global"
	if state, _ := c.ActivateProfile("profile-3"); state.Mode.Current != "rule" {
		t.Fatalf("switching between subscription profiles changed the mode to %q", state.Mode.Current)
	}
}

// A hand-edited profiles.json cannot smuggle a subscription into a
// Desync-only profile.
func TestLoadSanitizesDesyncOnlyProfile(t *testing.T) {
	isolateAppDataRoot(t)
	a := &App{profiles: []Profile{
		{
			ID: "profile-1", Name: "d", Type: "subscription", Kind: profileKindDesyncOnly,
			URL: "https://example.com/s", MergeTemplate: "x", RulesTemplate: "y", ProxyTemplate: "z",
			ScriptOverride: "s", SkipAutoConfig: true, AutoUpdateEnabled: true, AutoUpdateIntervalMinutes: 60,
			LastUpdateError: "boom", LastGoodGroup: "PROXY",
		},
		{ID: "profile-2", Name: "sub", Type: "subscription", URL: "https://example.com/t", MergeTemplate: "keep"},
	}}
	a.state.Profile.ActiveProfileID = "profile-2"
	if err := a.persistProfilesLocked(); err != nil {
		t.Fatal(err)
	}
	b := &App{}
	b.loadProfilesFromDisk()
	got := b.profiles[0]
	want := Profile{ID: "profile-1", Name: "d", Type: "local", Kind: profileKindDesyncOnly}
	if got.ID != want.ID || got.Name != want.Name || got.Type != want.Type || got.Kind != want.Kind ||
		got.URL != "" || got.MergeTemplate != "" || got.RulesTemplate != "" || got.ProxyTemplate != "" ||
		got.ScriptOverride != "" || got.SkipAutoConfig || got.AutoUpdateEnabled || got.AutoUpdateIntervalMinutes != 0 ||
		got.LastUpdateError != "" || got.LastGoodGroup != "" {
		t.Fatalf("sanitized = %+v", got)
	}
	if b.profiles[1].MergeTemplate != "keep" || b.profiles[1].URL == "" {
		t.Fatalf("a subscription profile was touched: %+v", b.profiles[1])
	}
}
