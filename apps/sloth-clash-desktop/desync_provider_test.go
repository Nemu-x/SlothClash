package main

import (
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"
)

func TestParseDesyncProviderMode(t *testing.T) {
	header := func(v string) func(string) string {
		return func(k string) string {
			if k == desyncProviderHeader {
				return v
			}
			return ""
		}
	}
	for _, tc := range []struct{ url, value, want string }{
		{"https://example.com/s", "deny", "deny"},
		{"https://example.com/s", " DENY ", "deny"},
		{"https://example.com/s", "Off", "off"},
		{"https://example.com/s", "on", ""}, // a provider cannot enable Desync
		{"https://example.com/s", "maybe", ""},
		{"https://example.com/s", "", ""},
		{"HTTPS://example.com/s", "deny", "deny"},
		{"http://example.com/s", "deny", ""}, // HTTPS only, as branding
		{"::not a url", "deny", ""},
	} {
		if got := parseDesyncProviderMode(tc.url, header(tc.value)); got != tc.want {
			t.Errorf("%s %q: got %q, want %q", tc.url, tc.value, got, tc.want)
		}
	}
}

func TestApplyDesyncProviderMode(t *testing.T) {
	on := desyncPtr(DesyncSettings{Enabled: true, YouTube: true})
	if s, changed := applyDesyncProviderMode(on, "", ""); changed || s != on {
		t.Fatal("no header and none stored changed something")
	}
	if s, changed := applyDesyncProviderMode(on, "off", "off"); changed || !s.Enabled {
		t.Fatal("a repeated off switched Desync off again")
	}
	for _, mode := range []string{"off", "deny"} {
		s, changed := applyDesyncProviderMode(on, "", mode)
		if !changed || s.Enabled || !s.YouTube || !on.Enabled {
			t.Fatalf("%s: %+v changed=%v (input mutated: %v)", mode, s, changed, !on.Enabled)
		}
	}
	if s, changed := applyDesyncProviderMode(nil, "", "deny"); !changed || s != nil {
		t.Fatal("default settings were materialized")
	}
	off := desyncPtr(DesyncSettings{YouTube: true})
	if s, changed := applyDesyncProviderMode(off, "deny", ""); !changed || s != off || s.Enabled {
		t.Fatal("lifting deny must change only the stored mode")
	}
}

// providerApp is desyncProfileApp with the provider recorder installed.
func providerApp(t *testing.T, sub *DesyncSettings) *App {
	t.Helper()
	a := desyncProfileApp(t, sub, desyncPtr(DesyncSettings{Enabled: true, Discord: true}))
	desyncProviderRecorder.Store(a)
	t.Cleanup(func() { desyncProviderRecorder.Store(nil) })
	return a
}

func profileRuntimeDir(t *testing.T, id string) string {
	t.Helper()
	root, err := slothDataRoot()
	if err != nil {
		t.Fatal(err)
	}
	return filepath.Join(root, "runtime", id)
}

func providerHeaders(mode string) http.Header {
	h := http.Header{}
	if mode != "" {
		h.Set(desyncProviderHeader, mode)
	}
	return h
}

func TestDesyncProviderOffAppliesOnce(t *testing.T) {
	withDesyncCoreFeatures(t, "desync", "desync-outbound")
	a := providerApp(t, desyncPtr(DesyncSettings{Enabled: true, YouTube: true}))
	dir := profileRuntimeDir(t, "profile-1")
	captureSubscriptionHeaders(dir, "https://example.com/s", providerHeaders("off"))
	if s := a.profileDesync(t, "profile-1"); s.Enabled || !s.YouTube {
		t.Fatalf("off did not switch Desync off: %+v", s)
	}
	if currentDesyncSettings().Enabled {
		t.Fatal("the live copy still has Desync on")
	}
	if _, err := a.SetDesyncSettings(DesyncSettings{Enabled: true, YouTube: true}); err != nil {
		t.Fatalf("the user cannot switch Desync on after off: %v", err)
	}
	captureSubscriptionHeaders(dir, "https://example.com/s", providerHeaders("off"))
	if !a.profileDesync(t, "profile-1").Enabled {
		t.Fatal("a repeated off overrode the user's choice")
	}
	if got := a.GetDesyncStatus().ProviderMode; got != "off" {
		t.Fatalf("status provider mode %q", got)
	}
}

func TestDesyncProviderDenyRefusesEnablingUntilLifted(t *testing.T) {
	withDesyncCoreFeatures(t, "desync", "desync-outbound")
	a := providerApp(t, desyncPtr(DesyncSettings{Enabled: true, YouTube: true}))
	dir := profileRuntimeDir(t, "profile-1")
	captureSubscriptionHeaders(dir, "https://example.com/s", providerHeaders("deny"))
	if a.profileDesync(t, "profile-1").Enabled {
		t.Fatal("deny left Desync on")
	}
	if _, err := a.SetDesyncSettings(DesyncSettings{Enabled: true, YouTube: true}); customError(err) == nil || customError(err).Code != "provider_denied" {
		t.Fatalf("enabling under deny: %v", err)
	}
	// Editing the selection stays allowed.
	if _, err := a.AddDesyncCustomDomain("example.org"); err != nil {
		t.Fatalf("editing under deny: %v", err)
	}
	if a.profileDesync(t, "profile-1").Enabled {
		t.Fatal("an edit under deny switched Desync on")
	}
	if got := a.GetDesyncStatus().ProviderMode; got != "deny" {
		t.Fatalf("status provider mode %q", got)
	}
	// Another profile's response does not touch this one.
	captureSubscriptionHeaders(profileRuntimeDir(t, "profile-2"), "https://example.com/s", providerHeaders(""))
	if got := a.GetDesyncStatus().ProviderMode; got != "deny" {
		t.Fatalf("another profile's response lifted the deny: %q", got)
	}
	captureSubscriptionHeaders(dir, "https://example.com/s", providerHeaders(""))
	if got := a.GetDesyncStatus().ProviderMode; got != "" {
		t.Fatalf("deny not lifted: %q", got)
	}
	if a.profileDesync(t, "profile-1").Enabled {
		t.Fatal("lifting deny switched Desync on")
	}
	if _, err := a.SetDesyncSettings(DesyncSettings{Enabled: true, YouTube: true}); err != nil {
		t.Fatalf("enabling after the deny was lifted: %v", err)
	}
}

func TestDesyncProviderModeIsSavedWithTheProfile(t *testing.T) {
	withDesyncCoreFeatures(t, "desync", "desync-outbound")
	a := providerApp(t, desyncPtr(DesyncSettings{Enabled: true, YouTube: true}))
	captureSubscriptionHeaders(profileRuntimeDir(t, "profile-1"), "https://example.com/s", providerHeaders("deny"))
	b := &App{}
	b.loadProfilesFromDisk()
	b.mu.RLock()
	mode := b.profiles[0].DesyncProviderMode
	b.mu.RUnlock()
	if mode != "deny" || b.profileDesync(t, "profile-1").Enabled {
		t.Fatalf("after a reload: mode %q, desync %+v", mode, b.profileDesync(t, "profile-1"))
	}
	_ = a
}

func TestDesyncProviderSkipsProfilesWithoutSubscription(t *testing.T) {
	withDesyncCoreFeatures(t, "desync", "desync-outbound")
	a := providerApp(t, nil)
	captureSubscriptionHeaders(profileRuntimeDir(t, "profile-2"), "https://example.com/s", providerHeaders("deny"))
	a.mu.RLock()
	mode := a.profiles[1].DesyncProviderMode
	a.mu.RUnlock()
	if mode != "" || !a.profileDesync(t, "profile-2").Enabled {
		t.Fatal("a Desync-only profile took a provider mode")
	}
}

// subscriptionServer serves a minimal subscription over HTTPS with the given
// provider mode, and makes the default transport trust it for the test.
func subscriptionServer(t *testing.T, mode string) string {
	t.Helper()
	srv := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		if mode != "" {
			w.Header().Set(desyncProviderHeader, mode)
		}
		_, _ = w.Write([]byte("proxies: []\n"))
	}))
	t.Cleanup(srv.Close)
	prev := http.DefaultTransport
	http.DefaultTransport = srv.Client().Transport
	t.Cleanup(func() { http.DefaultTransport = prev })
	return srv.URL + "/sub"
}

func importedProfile(t *testing.T, a *App) Profile {
	t.Helper()
	a.mu.RLock()
	defer a.mu.RUnlock()
	return a.profiles[len(a.profiles)-1]
}

func TestImportConnectDesyncIsTheUsersChoice(t *testing.T) {
	withDesyncCoreFeatures(t, "desync", "desync-outbound")
	// YouTube runs on every OS (Discord through Desync is Windows-only).
	only := desyncPtr(DesyncSettings{Enabled: true, YouTube: true, Custom: []DesyncCustomEntry{{ID: "0a1b2c3d", Domain: "example.org", Route: desyncRouteDirect, Enabled: true}}})
	a := desyncProfileApp(t, nil, only)
	url := subscriptionServer(t, "")

	if _, err := a.ImportProfileFromURL("plain", url); err != nil {
		t.Fatal(err)
	}
	if p := importedProfile(t, a); p.Desync != nil || profileDesyncSettings(p).Enabled {
		t.Fatalf("an import without the choice has Desync: %+v", p.Desync)
	}

	if _, err := a.ImportProfileFromURLWithDesync("with desync", url, true); err != nil {
		t.Fatal(err)
	}
	s := profileDesyncSettings(importedProfile(t, a))
	if !s.Enabled || !s.YouTube || s.Discord || len(s.Custom) != 1 || s.Custom[0].Domain != "example.org" {
		t.Fatalf("the Desync-only selection was not carried: %+v", s)
	}
}

func TestImportConnectDesyncDefaultsWithoutDesyncOnlyProfile(t *testing.T) {
	withDesyncCoreFeatures(t, "desync", "desync-outbound")
	a := desyncProfileApp(t, nil, nil)
	a.profiles = a.profiles[:1] // no Desync-only profile
	url := subscriptionServer(t, "")
	if _, err := a.ImportProfileFromURLWithDesync("with desync", url, true); err != nil {
		t.Fatal(err)
	}
	want := defaultDesyncSettings()
	s := profileDesyncSettings(importedProfile(t, a))
	if !s.Enabled || s.YouTube != want.YouTube || s.Discord != want.Discord {
		t.Fatalf("defaults not applied: %+v", s)
	}
}

func TestImportDenyWinsOverConnectDesync(t *testing.T) {
	withDesyncCoreFeatures(t, "desync", "desync-outbound")
	a := desyncProfileApp(t, nil, desyncPtr(DesyncSettings{Enabled: true, YouTube: true}))
	url := subscriptionServer(t, "deny")
	if _, err := a.ImportProfileFromURLWithDesync("denied", url, true); err != nil {
		t.Fatal(err)
	}
	p := importedProfile(t, a)
	if p.DesyncProviderMode != "deny" || profileDesyncSettings(p).Enabled {
		t.Fatalf("deny at import: mode %q, desync %+v", p.DesyncProviderMode, p.Desync)
	}
}

func TestImportConnectDesyncNeedsTheCore(t *testing.T) {
	withDesyncCoreFeatures(t) // a core without the desync outbound
	a := desyncProfileApp(t, nil, desyncPtr(DesyncSettings{Enabled: true, YouTube: true}))
	url := subscriptionServer(t, "")
	if _, err := a.ImportProfileFromURLWithDesync("no core", url, true); err != nil {
		t.Fatal(err)
	}
	if profileDesyncSettings(importedProfile(t, a)).Enabled {
		t.Fatal("Desync switched on without a core that runs it")
	}
}
