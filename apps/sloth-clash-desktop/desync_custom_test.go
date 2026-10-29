package main

import (
	"encoding/json"
	"errors"
	"fmt"
	"reflect"
	"strings"
	"testing"

	"SlothClashDesktop/internal/desync"
)

func TestDesyncSaveSelectionPrunesRemovedCustomWinners(t *testing.T) {
	withDesyncPrefs(t, DesyncSettings{Enabled: true, Custom: []DesyncCustomEntry{
		{ID: "0a1b2c3d", Domain: "example.org", Enabled: true},
		{ID: "5a6b7c8d", Domain: "off.example.net", Enabled: false},
	}})
	stale := map[string]string{"example.org": "r3", "off.example.net": "split"}
	for i := range 40 {
		stale[fmt.Sprintf("gone%d.example.com", i)] = "r1"
	}
	prefsMu.Lock()
	prefsCurrent.DesyncCache = []DesyncNetworkCache{
		{Network: "net-a", YouTube: "r2", Custom: stale},
		{Network: "net-b", Custom: map[string]string{"gone0.example.com": "r1"}},
	}
	prefsMu.Unlock()

	saveDesyncSelection("net-a", desync.CustomService("example.org"), "r4")
	var a, b DesyncNetworkCache
	for _, c := range currentDesktopPrefs().DesyncCache {
		switch c.Network {
		case "net-a":
			a = c
		case "net-b":
			b = c
		}
	}
	// Removed entries' winners are dropped; a switched-off entry keeps its
	// winner (it is still in the list), and the built-in winner stays.
	want := map[string]string{"example.org": "r4", "off.example.net": "split"}
	if !reflect.DeepEqual(a.Custom, want) || a.YouTube != "r2" {
		t.Fatalf("net-a cache: %+v", a)
	}
	// Only the saved network is rewritten.
	if len(b.Custom) != 1 {
		t.Fatalf("net-b cache changed: %+v", b)
	}
}

func TestDesyncCustomDomainNormalization(t *testing.T) {
	cases := []struct {
		in, want, code string
	}{
		{in: "example.org", want: "example.org"},
		{in: "  Example.ORG  ", want: "example.org"},
		{in: "example.org.", want: "example.org"},
		{in: ".example.org", want: "example.org"},
		{in: "https://Example.org/path?q=1#x", want: "example.org"},
		{in: "http://user:pw@example.org:8443/", want: "example.org"},
		{in: "example.org:443", want: "example.org"},
		{in: "example.org/watch?v=1", want: "example.org"},
		{in: "sub.cdn.example.net", want: "sub.cdn.example.net"},
		{in: "пример.рф", want: "xn--e1afmkfd.xn--p1ai"},
		{in: "ПРИМЕР.РФ", want: "xn--e1afmkfd.xn--p1ai"},
		{in: "bücher.example", want: "xn--bcher-kva.example"},
		{in: "xn--e1afmkfd.xn--p1ai", want: "xn--e1afmkfd.xn--p1ai"},
		{in: "user.github.io", want: "user.github.io"},
		{in: "", code: "empty"},
		{in: "   ", code: "empty"},
		{in: "1.2.3.4", code: "ip"},
		{in: "https://1.2.3.4/", code: "ip"},
		{in: "1.2.3.4:443", code: "ip"},
		{in: "[2001:db8::1]:443", code: "ip"},
		{in: "2001:db8::1", code: "ip"},
		{in: "10.0.0", code: "ip"},
		{in: "*.example.org", code: "wildcard"},
		{in: "+.example.org", code: "wildcard"},
		{in: "ex*mple.org", code: "wildcard"},
		{in: "com", code: "public_suffix"},
		{in: "co.uk", code: "public_suffix"},
		{in: "github.io", code: "public_suffix"},
		{in: "localhost", code: "public_suffix"},
		{in: "рф", code: "public_suffix"},
		{in: "youtube.com", code: "builtin"},
		{in: "www.youtube.com", code: "builtin"},
		{in: "rr3.googlevideo.com", code: "builtin"},
		{in: "cdn.discordapp.com", code: "builtin"},
		{in: "exa_mple.org", code: "invalid"},
		{in: "-example.org", code: "invalid"},
		{in: "example-.org", code: "invalid"},
		{in: "exa mple.org", code: "invalid"},
		{in: "example..org", code: "invalid"},
		{in: "example.org,DIRECT", code: "invalid"},
		{in: "example.org),(NETWORK,udp", code: "invalid"},
		{in: "example.org:port", code: "invalid"},
		{in: strings.Repeat("a", 64) + ".org", code: "invalid"},
		{in: "https:///path", code: "invalid"},
	}
	for _, c := range cases {
		got, err := normalizeDesyncCustomDomain(c.in)
		if c.code == "" {
			if err != nil || got != c.want {
				t.Errorf("%q: got %q, %v; want %q", c.in, got, err, c.want)
			}
			continue
		}
		var custom *desyncError
		if !errors.As(err, &custom) || custom.Code != c.code {
			t.Errorf("%q: got %q, %v; want error %s", c.in, got, err, c.code)
		}
	}
}

func TestDesyncCustomSanitizeDropsUnsafeStoredEntries(t *testing.T) {
	var many []DesyncCustomEntry
	for i := 0; i < desyncCustomLimit+5; i++ {
		many = append(many, DesyncCustomEntry{ID: fmt.Sprintf("e%d", i), Domain: fmt.Sprintf("site%d.example.org", i), Enabled: true})
	}
	if got := sanitizeDesyncCustom(many); len(got) != desyncCustomLimit || got[0].ID != "e0" {
		t.Fatalf("cap: %d", len(got))
	}
	got := sanitizeDesyncCustom([]DesyncCustomEntry{
		{ID: "a1", Domain: "example.org", Enabled: true},
		{ID: "a1", Domain: "other.org"},                        // duplicate ID
		{ID: "b2", Domain: "example.org"},                      // duplicate domain
		{ID: "c3", Domain: "Example.NET"},                      // not normalized
		{ID: "d4", Domain: "x.org),(NETWORK,udp)),DIRECT"},     // rule injection
		{ID: "E5", Domain: "upper-id.org"},                     // ID outside the alphabet
		{ID: "f6,DIRECT", Domain: "bad-id.org"},                // ID would end up in a group name
		{ID: "g7", Domain: "youtube.com"},                      // covered by a built-in
		{ID: "h8", Domain: "kept.example.net", Enabled: false}, // valid, disabled
	})
	if len(got) != 2 || got[0].Domain != "example.org" || got[1].ID != "h8" {
		t.Fatalf("sanitized: %+v", got)
	}
}

// withDesyncPrefs sets the live Desync settings (the active profile's copy)
// for one test; the prefs and the other profiles' cache keys are restored
// afterwards.
func withDesyncPrefs(t *testing.T, s DesyncSettings) {
	t.Helper()
	prefsMu.Lock()
	previous, others, legacy := prefsCurrent, desyncOtherProfileKeys, prefsLegacyDesync
	prefsCurrent.Desync = s
	prefsMu.Unlock()
	t.Cleanup(func() {
		prefsMu.Lock()
		prefsCurrent, desyncOtherProfileKeys, prefsLegacyDesync = previous, others, legacy
		prefsMu.Unlock()
	})
}

func TestDesyncCustomEntryLifecycle(t *testing.T) {
	a := desyncProfileApp(t, nil, nil)
	status, err := a.AddDesyncCustomDomain("https://Example.org/page")
	if err != nil || len(status.Settings.Custom) != 1 {
		t.Fatalf("add: %+v %v", status.Settings.Custom, err)
	}
	entry := status.Settings.Custom[0]
	if entry.Domain != "example.org" || !entry.Enabled || !validDesyncCustomID(entry.ID) {
		t.Fatalf("entry: %+v", entry)
	}
	// Defaults of a never-touched config are kept when the list is created.
	if !status.Settings.YouTube || status.Settings.Enabled {
		t.Fatalf("defaults lost: %+v", status.Settings)
	}
	if _, err := a.AddDesyncCustomDomain("EXAMPLE.org"); !isDesyncCode(err, "duplicate") {
		t.Fatalf("duplicate: %v", err)
	}
	status, _ = a.AddDesyncCustomDomain("other.example.net")
	if len(status.Settings.Custom) != 2 || status.Settings.Custom[1].ID == entry.ID {
		t.Fatalf("second: %+v", status.Settings.Custom)
	}
	status, err = a.SetDesyncCustomDomainEnabled(entry.ID, false)
	if err != nil || status.Settings.Custom[0].Enabled || !status.Settings.Custom[1].Enabled {
		t.Fatalf("toggle: %+v %v", status.Settings.Custom, err)
	}
	// A stale full settings object from the UI never rolls the list back.
	stale := status.Settings
	stale.Custom = nil
	stale.Enabled = false
	if status, err = a.SetDesyncSettings(stale); err != nil || len(status.Settings.Custom) != 2 {
		t.Fatalf("SetDesyncSettings dropped custom entries: %+v %v", status.Settings.Custom, err)
	}
	status, err = a.RemoveDesyncCustomDomain(entry.ID)
	if err != nil || len(status.Settings.Custom) != 1 || status.Settings.Custom[0].Domain != "other.example.net" {
		t.Fatalf("remove: %+v %v", status.Settings.Custom, err)
	}
	if _, err := a.RemoveDesyncCustomDomain(entry.ID); !isDesyncCode(err, "not_found") {
		t.Fatalf("remove twice: %v", err)
	}
	// Stored as JSON the prefs file round-trips; an old file without the key
	// is an empty list.
	raw, _ := json.Marshal(currentDesktopPrefs().Desync)
	var back DesyncSettings
	if json.Unmarshal(raw, &back) != nil || len(back.Custom) != 1 {
		t.Fatalf("round trip: %s", raw)
	}
	var legacy DesyncSettings
	if json.Unmarshal([]byte(`{"enabled":true,"youtube":true}`), &legacy) != nil || legacy.Custom != nil || len(effectiveDesyncSettings(legacy).Custom) != 0 {
		t.Fatal("legacy prefs gained entries")
	}
}

func TestDesyncCustomOnlySelectionAndLastEntryDisables(t *testing.T) {
	a := desyncProfileApp(t, desyncPtr(DesyncSettings{Enabled: false, YouTube: false, Discord: false}), nil)
	if _, err := validateDesyncSettings(DesyncSettings{Enabled: true}, "linux", true); err == nil {
		t.Fatal("empty selection accepted")
	}
	only := DesyncSettings{Enabled: true, Custom: []DesyncCustomEntry{{ID: "a1", Domain: "example.org", Enabled: true}}}
	if _, err := validateDesyncSettings(only, "linux", true); err != nil {
		t.Fatalf("custom-only selection rejected: %v", err)
	}
	only.Custom[0].Enabled = false
	if _, err := validateDesyncSettings(only, "linux", true); err == nil {
		t.Fatal("only a disabled entry counted as a selection")
	}
	status, _ := a.AddDesyncCustomDomain("example.org")
	id := status.Settings.Custom[0].ID
	settings := status.Settings
	settings.Enabled = true
	if status, err := a.SetDesyncSettings(settings); err != nil || !status.Settings.Enabled {
		if desyncCoreSupported() {
			t.Fatalf("enable with a custom entry: %v", err)
		}
		// Without the embedded core features enabling is refused anyway.
		a.mu.Lock()
		a.profiles[0].Desync.Enabled = true
		a.mu.Unlock()
	}
	if status, _ = a.SetDesyncCustomDomainEnabled(id, false); status.Settings.Enabled {
		t.Fatal("Desync stayed on with nothing selected")
	}
}

// withDesyncCoreFeatures pretends the embedded core reports features.
func withDesyncCoreFeatures(t *testing.T, features ...string) {
	t.Helper()
	embeddedCoreFeatures() // settle the once before overriding its value
	previous := embeddedCoreFeaturesValue
	embeddedCoreFeaturesValue = features
	t.Cleanup(func() { embeddedCoreFeaturesValue = previous })
}

func TestDesyncSettingsEnableKeepsStoredCustomOnlySelection(t *testing.T) {
	withDesyncCoreFeatures(t, "desync")
	stored := []DesyncCustomEntry{{ID: "0a1b2c3d", Domain: "example.org", Enabled: true}}
	a := desyncProfileApp(t, desyncPtr(DesyncSettings{Custom: stored}), nil)
	// The UI sends the toggles only: no YouTube/Discord and no Custom field.
	status, err := a.SetDesyncSettings(DesyncSettings{Enabled: true})
	if err != nil || !status.Settings.Enabled {
		t.Fatalf("enabling with only stored custom domains failed: %+v %v", status.Settings, err)
	}
	if !reflect.DeepEqual(status.Settings.Custom, stored) || !reflect.DeepEqual(currentDesyncSettings().Custom, stored) {
		t.Fatalf("custom list not kept: status %+v, prefs %+v", status.Settings.Custom, currentDesyncSettings().Custom)
	}
}

func isDesyncCode(err error, code string) bool {
	var custom *desyncError
	return errors.As(err, &custom) && custom.Code == code
}
