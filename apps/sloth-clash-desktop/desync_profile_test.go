package main

import (
	"encoding/json"
	"os"
	"reflect"
	"strings"
	"testing"
)

// desyncProfileApp returns an App with a subscription profile "sub" (active)
// and a Desync-only profile "only", with the live Desync copy synced. A nil
// setting means the profile never had its settings changed.
func desyncProfileApp(t *testing.T, sub, only *DesyncSettings) *App {
	t.Helper()
	isolateAppDataRoot(t)
	withDesyncPrefs(t, DesyncSettings{})
	a := &App{profiles: []Profile{
		{ID: "profile-1", Name: "sub", Type: "subscription", URL: "https://example.com/s", Desync: sub},
		{ID: "profile-2", Name: "only", Type: "local", Kind: profileKindDesyncOnly, Desync: only},
	}}
	a.state.Profile.Profiles = a.profiles
	a.state.Profile.ActiveProfileID = "profile-1"
	a.state.Mode.Current = "rule"
	a.mu.Lock()
	a.syncActiveDesyncLocked()
	a.mu.Unlock()
	return a
}

func desyncPtr(s DesyncSettings) *DesyncSettings { return &s }

func (a *App) profileDesync(t *testing.T, id string) DesyncSettings {
	t.Helper()
	a.mu.RLock()
	defer a.mu.RUnlock()
	for _, p := range a.profiles {
		if p.ID == id {
			return profileDesyncSettings(p)
		}
	}
	t.Fatalf("no profile %s", id)
	return DesyncSettings{}
}

// Turning Desync off, or changing what it covers, on one profile leaves the
// other alone; activating a profile makes its settings the live ones.
func TestDesyncSettingsArePerProfile(t *testing.T) {
	withDesyncCoreFeatures(t, "desync")
	a := desyncProfileApp(t, desyncPtr(DesyncSettings{Enabled: true, YouTube: true}), desyncPtr(DesyncSettings{Enabled: true, YouTube: true}))
	if !currentDesyncSettings().Enabled {
		t.Fatal("the active profile's settings are not live")
	}
	if _, err := a.SetDesyncSettings(DesyncSettings{Enabled: false, YouTube: true}); err != nil {
		t.Fatal(err)
	}
	if _, err := a.AddDesyncCustomDomain("example.org"); err != nil {
		t.Fatal(err)
	}
	if s := a.profileDesync(t, "profile-1"); s.Enabled || len(s.Custom) != 1 {
		t.Fatalf("sub after the change: %+v", s)
	}
	if s := a.profileDesync(t, "profile-2"); !s.Enabled || len(s.Custom) != 0 || !s.YouTube {
		t.Fatalf("the other profile changed: %+v", s)
	}
	if live := currentDesyncSettings(); live.Enabled || len(live.Custom) != 1 {
		t.Fatalf("live copy: %+v", live)
	}

	if _, err := a.ActivateProfile("profile-2"); err != nil {
		t.Fatal(err)
	}
	if live := currentDesyncSettings(); !live.Enabled || len(live.Custom) != 0 {
		t.Fatalf("live copy after activating the other profile: %+v", live)
	}
	if status := a.GetDesyncStatus(); !status.DesyncOnly || !status.Settings.Enabled {
		t.Fatalf("status: %+v", status)
	}
	// The cache keeps the inactive profile's winners.
	if !desyncOtherProfileKeys["example.org"] {
		t.Fatalf("other profiles' cache keys: %v", desyncOtherProfileKeys)
	}

	// Saved per profile in profiles.json; a new run reads them back.
	b := &App{}
	b.loadProfilesFromDisk()
	if s := b.profileDesync(t, "profile-1"); s.Enabled || len(s.Custom) != 1 || s.Custom[0].Domain != "example.org" {
		t.Fatalf("sub after a reload: %+v", s)
	}
	if !currentDesyncSettings().Enabled {
		t.Fatal("the reloaded active profile's settings are not live")
	}
}

// A Desync-only profile has nothing but Desync routes: it is always on,
// whatever was stored, and the off switch cannot turn it off.
func TestDesyncOnlyProfileIsAlwaysEnabled(t *testing.T) {
	withDesyncCoreFeatures(t, "desync")
	for _, stored := range []*DesyncSettings{nil, desyncPtr(DesyncSettings{YouTube: true})} {
		p := Profile{ID: "profile-2", Kind: profileKindDesyncOnly, Desync: stored}
		if !profileDesyncSettings(p).Enabled {
			t.Fatalf("stored %+v: not enabled", stored)
		}
	}
	if profileDesyncSettings(Profile{ID: "profile-1"}).Enabled {
		t.Fatal("a subscription profile is on by default")
	}

	a := desyncProfileApp(t, nil, desyncPtr(DesyncSettings{YouTube: true}))
	if _, err := a.ActivateProfile("profile-2"); err != nil {
		t.Fatal(err)
	}
	status, err := a.SetDesyncSettings(DesyncSettings{Enabled: false, YouTube: true})
	if err != nil || !status.Settings.Enabled || !currentDesyncSettings().Enabled {
		t.Fatalf("switched a Desync-only profile off: %+v %v", status.Settings, err)
	}
}

// The legacy global settings are copied into every profile without settings
// of its own, once; the copy is saved and prefs.json loses the object.
func TestDesyncLegacyPrefsMigrateIntoProfiles(t *testing.T) {
	isolateAppDataRoot(t)
	withDesyncPrefs(t, DesyncSettings{})
	key := storedDesyncReportKey()
	t.Cleanup(func() { prefsDesyncReportKey.Store(key) })
	own := DesyncSettings{Enabled: true, Discord: true}
	disk := profilesPersisted{
		ActiveProfileID: "profile-1",
		Profiles: []Profile{
			{ID: "profile-1", Name: "sub", Type: "subscription", URL: "https://example.com/s"},
			{ID: "profile-2", Name: "only", Type: "local", Kind: profileKindDesyncOnly},
			{ID: "profile-3", Name: "own", Type: "subscription", URL: "https://example.com/t", Desync: &own},
		},
	}
	writeProfilesFile(t, disk)
	// As the last build before per-profile settings saved it: TTL fields
	// and a custom list with an entry under the old "preset" key.
	legacy := `{"enabled":true,"youtube":false,"discord":true,"discordTTL":4,"discordUDP":true,"discordUDPTTL":4,"discordUDPRepeats":1,"custom":[{"id":"a1","domain":"example.org","enabled":true},{"id":"b2","domain":"","preset":"facebook","enabled":false}]}`
	writePrefsFile(t, `{"tun":{},"desync":`+legacy+`,"desyncReportKey":"`+strings.Repeat("ab", 32)+`"}`)
	loadDesktopPrefs()

	a := &App{}
	a.loadProfilesFromDisk()
	want := DesyncSettings{Enabled: true, Discord: true, DiscordTTL: 4, DiscordUDP: true, DiscordUDPTTL: 4, DiscordUDPRepeats: 1, Custom: []DesyncCustomEntry{
		{ID: "a1", Domain: "example.org", Enabled: true},
		{ID: "b2", Service: "facebook"},
	}}
	if got := a.profileDesync(t, "profile-1"); !reflect.DeepEqual(got, want) {
		t.Fatalf("sub: %+v", got)
	}
	only := want
	if got := a.profileDesync(t, "profile-2"); !reflect.DeepEqual(got, only) {
		t.Fatalf("Desync-only: %+v", got)
	}
	if got := a.profileDesync(t, "profile-3"); got.YouTube || !got.Discord || len(got.Custom) != 0 {
		t.Fatalf("a profile with its own settings was overwritten: %+v", got)
	}
	if !reflect.DeepEqual(currentDesyncSettings(), want) {
		t.Fatalf("live copy: %+v", currentDesyncSettings())
	}

	// On disk: the profiles carry the copy and the marker; prefs.json has
	// no "desync" any more, the rest of it is kept.
	saved := readProfilesFile(t)
	if !saved.DesyncPerProfile || saved.Profiles[0].Desync == nil || saved.Profiles[1].Desync == nil {
		t.Fatalf("profiles.json after the move: %+v", saved)
	}
	prefs := readPrefsFile(t)
	if strings.Contains(prefs, `"desync"`) || !strings.Contains(prefs, `"desyncReportKey"`) {
		t.Fatalf("prefs.json after the move:\n%s", prefs)
	}

	// Idempotent: a second start changes nothing, even if an old prefs.json
	// came back (restored backup, older build).
	a.mu.Lock()
	a.profiles[0].Desync = &DesyncSettings{YouTube: true}
	if err := a.persistProfilesLocked(); err != nil {
		t.Fatal(err)
	}
	a.mu.Unlock()
	writePrefsFile(t, `{"desync":`+legacy+`}`)
	loadDesktopPrefs()
	b := &App{}
	b.loadProfilesFromDisk()
	if got := b.profileDesync(t, "profile-1"); got.Enabled || !got.YouTube || got.Discord {
		t.Fatalf("migrated twice: %+v", got)
	}
}

// Untouched legacy settings (the defaults) leave the profiles on the
// defaults, without storing a copy.
func TestDesyncLegacyDefaultsMigrateToNothing(t *testing.T) {
	for _, raw := range []string{``, `{}`, `{"enabled":false,"youtube":false,"discord":false,"discordTTL":0,"discordUDP":false,"discordUDPTTL":0,"discordUDPRepeats":0}`} {
		if _, ok := legacyDesyncSelection(json.RawMessage(raw)); ok {
			t.Fatalf("%q migrated", raw)
		}
	}
	// Everything switched off by hand is a choice, not the defaults.
	s, ok := legacyDesyncSelection(json.RawMessage(`{"enabled":false,"youtube":false,"discord":false,"discordTTL":4,"discordUDPTTL":4,"discordUDPRepeats":1}`))
	if !ok || s.YouTube || s.Discord {
		t.Fatalf("all off: %+v %t", s, ok)
	}
	// Untouched switches with custom entries: the defaults plus the list.
	s, ok = legacyDesyncSelection(json.RawMessage(`{"custom":[{"id":"a1","domain":"example.org","enabled":true}]}`))
	if !ok || !s.YouTube || len(s.Custom) != 1 {
		t.Fatalf("custom only: %+v %t", s, ok)
	}
}

// The onboarding's selection lands in the new profile in one call; nothing
// else changes, and a selection Desync cannot run is refused.
func TestCreateDesyncOnlyProfileStoresSelection(t *testing.T) {
	withDesyncCoreFeatures(t, "desync")
	sub := DesyncSettings{Enabled: false, YouTube: true}
	a := desyncProfileApp(t, &sub, nil)
	selection := DesyncSettings{Custom: []DesyncCustomEntry{{ID: "a1", Domain: "example.org", Enabled: true}, {ID: "b2", Service: "facebook", Enabled: true}}}
	state, err := a.CreateDesyncOnlyProfile("Mine", selection)
	if err != nil {
		t.Fatal(err)
	}
	created := state.Profile.Profiles[len(state.Profile.Profiles)-1]
	if created.Name != "Mine" || created.Desync == nil || !created.Desync.Enabled || len(created.Desync.Custom) != 2 {
		t.Fatalf("created: %+v %+v", created, created.Desync)
	}
	if got := a.profileDesync(t, "profile-1"); !reflect.DeepEqual(got, profileDesyncSettings(Profile{Desync: &sub})) {
		t.Fatalf("the active profile changed: %+v", got)
	}
	before := len(a.profiles)
	if _, err := a.CreateDesyncOnlyProfile("Empty", DesyncSettings{}); err == nil || len(a.profiles) != before {
		t.Fatalf("created a profile with nothing selected: %v", err)
	}
}

func writeProfilesFile(t *testing.T, disk profilesPersisted) {
	t.Helper()
	path, err := profilesStorePath()
	if err != nil {
		t.Fatal(err)
	}
	b, _ := json.Marshal(disk)
	if err := os.WriteFile(path, b, 0o644); err != nil {
		t.Fatal(err)
	}
}

func readProfilesFile(t *testing.T) profilesPersisted {
	t.Helper()
	path, _ := profilesStorePath()
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var disk profilesPersisted
	if err := json.Unmarshal(b, &disk); err != nil {
		t.Fatal(err)
	}
	return disk
}

func writePrefsFile(t *testing.T, body string) {
	t.Helper()
	path, err := prefsStorePath()
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
}

func readPrefsFile(t *testing.T) string {
	t.Helper()
	path, _ := prefsStorePath()
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}
