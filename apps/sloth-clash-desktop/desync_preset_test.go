package main

import (
	"encoding/json"
	"fmt"
	"reflect"
	"strings"
	"testing"

	"SlothClashDesktop/internal/desync"
)

func customError(err error) *desyncCustomError {
	custom, _ := err.(*desyncCustomError)
	return custom
}

func TestDesyncPresetListMatchesCatalog(t *testing.T) {
	list := (&App{}).ListDesyncPresets()
	if len(list) != len(desync.Presets) {
		t.Fatalf("list: %+v", list)
	}
	for i, p := range desync.Presets {
		if list[i].ID != p.ID || list[i].Name != p.Name || !reflect.DeepEqual(list[i].Domains, p.Domains) {
			t.Fatalf("preset %d: %+v", i, list[i])
		}
	}
	// A copy: the UI cannot edit the catalog through the result.
	list[0].Domains[0] = "changed.example"
	if desync.Presets[0].Domains[0] == "changed.example" {
		t.Fatal("ListDesyncPresets exposes the catalog slice")
	}
}

func TestDesyncPresetLifecycle(t *testing.T) {
	withDesyncPrefs(t, DesyncSettings{})
	a := &App{}
	if _, err := a.AddDesyncPreset("myspace"); !isCustomCode(err, "unknown_preset") {
		t.Fatalf("unknown preset: %v", err)
	}
	status, err := a.AddDesyncPreset("facebook")
	if err != nil || len(status.Settings.Custom) != 1 {
		t.Fatalf("add: %+v %v", status.Settings.Custom, err)
	}
	entry := status.Settings.Custom[0]
	if entry.Preset != "facebook" || entry.Domain != "" || !entry.Enabled || !validDesyncCustomID(entry.ID) || entry.key() != "preset:facebook" {
		t.Fatalf("entry: %+v", entry)
	}
	if _, err := a.AddDesyncPreset("facebook"); !isCustomCode(err, "duplicate") {
		t.Fatalf("duplicate preset: %v", err)
	}
	// A user domain the preset covers is rejected, naming the preset, while
	// the preset is in the list, also switched off.
	for _, enabled := range []bool{true, false} {
		if _, err := a.SetDesyncCustomDomainEnabled(entry.ID, enabled); err != nil {
			t.Fatal(err)
		}
		for _, domain := range []string{"facebook.com", "https://scontent-ams2-1.xx.fbcdn.net/v/x.jpg", "m.me"} {
			_, err := a.AddDesyncCustomDomain(domain)
			if c := customError(err); c == nil || c.Code != "preset_overlap" || c.Detail != "facebook" || err.Error() != "custom domain: preset_overlap: facebook" {
				t.Fatalf("enabled=%t %s: %v", enabled, domain, err)
			}
		}
	}
	// Unrelated domains, and look-alikes, still work.
	for _, domain := range []string{"notfacebook.com", "example.org"} {
		if _, err := a.AddDesyncCustomDomain(domain); err != nil {
			t.Fatalf("%s: %v", domain, err)
		}
	}
	status, err = a.RemoveDesyncCustomDomain(entry.ID)
	if err != nil || len(status.Settings.Custom) != 2 {
		t.Fatalf("remove preset: %+v %v", status.Settings.Custom, err)
	}
	// After removing the preset its domains are plain-addable again.
	if _, err := a.AddDesyncCustomDomain("facebook.com"); err != nil {
		t.Fatalf("after remove: %v", err)
	}
	// Prefs round-trip; a plain entry has no "preset" key.
	raw, _ := json.Marshal(currentDesktopPrefs().Desync)
	if strings.Contains(string(raw), `"preset"`) {
		t.Fatalf("plain entries serialize a preset key: %s", raw)
	}
	status, _ = a.AddDesyncPreset("x")
	raw, _ = json.Marshal(currentDesktopPrefs().Desync)
	var back DesyncSettings
	if json.Unmarshal(raw, &back) != nil || !reflect.DeepEqual(effectiveDesyncSettings(back).Custom, status.Settings.Custom) || !strings.Contains(string(raw), `"preset":"x"`) {
		t.Fatalf("round trip: %s", raw)
	}
}

// A plain entry the preset covers is replaced by it (the owner's case: a
// "facebook.com" typed before presets existed); others stay in order.
func TestDesyncPresetReplacesCoveredPlainEntries(t *testing.T) {
	withDesyncPrefs(t, DesyncSettings{Enabled: true, DiscordTTL: 4, Custom: []DesyncCustomEntry{
		{ID: "0a1b2c3d", Domain: "facebook.com", Enabled: true},
		{ID: "1b2c3d4e", Domain: "example.org", Enabled: true},
		{ID: "2c3d4e5f", Domain: "video.xx.fbcdn.net", Enabled: false},
		{ID: "3d4e5f6a", Domain: "instagram.com", Enabled: true},
	}})
	status, err := (&App{}).AddDesyncPreset("facebook")
	if err != nil {
		t.Fatal(err)
	}
	got := status.Settings.Custom
	if len(got) != 3 || got[0].ID != "1b2c3d4e" || got[1].ID != "3d4e5f6a" || got[2].Preset != "facebook" || !got[2].Enabled {
		t.Fatalf("custom after preset: %+v", got)
	}
	// Instagram shares fbcdn.net with Facebook: both presets coexist (the
	// overlap is resolved by catalog order at generation time), and the
	// plain instagram.com entry is replaced.
	status, err = (&App{}).AddDesyncPreset("instagram")
	if err != nil || len(status.Settings.Custom) != 3 || status.Settings.Custom[1].Preset != "facebook" || status.Settings.Custom[2].Preset != "instagram" {
		t.Fatalf("instagram: %+v %v", status.Settings.Custom, err)
	}
}

func TestDesyncPresetCountsAsOneEntryTowardTheCap(t *testing.T) {
	var full []DesyncCustomEntry
	for i := range desyncCustomLimit - 1 {
		full = append(full, DesyncCustomEntry{ID: fmt.Sprintf("e%d", i), Domain: fmt.Sprintf("site%d.example.org", i), Enabled: true})
	}
	withDesyncPrefs(t, DesyncSettings{Enabled: true, DiscordTTL: 4, Custom: full})
	a := &App{}
	if _, err := a.AddDesyncPreset("x"); err != nil {
		t.Fatalf("preset as the last entry: %v", err)
	}
	if _, err := a.AddDesyncPreset("facebook"); !isCustomCode(err, "limit") {
		t.Fatalf("over the cap: %v", err)
	}
	if _, err := a.AddDesyncCustomDomain("other.example.net"); !isCustomCode(err, "limit") {
		t.Fatalf("plain over the cap: %v", err)
	}
	// Replacing a covered entry frees its slot.
	prefsMu.Lock()
	prefsCurrent.Desync.Custom[0] = DesyncCustomEntry{ID: "e0", Domain: "instagram.com", Enabled: true}
	prefsMu.Unlock()
	if _, err := a.AddDesyncPreset("instagram"); err != nil {
		t.Fatalf("replacing at the cap: %v", err)
	}
}

func TestDesyncPresetSanitize(t *testing.T) {
	in := []DesyncCustomEntry{
		{ID: "a1", Domain: "facebook.com", Enabled: true},       // covered by the preset below: dropped
		{ID: "a2", Preset: "facebook", Enabled: true},           // kept
		{ID: "a3", Preset: "facebook", Enabled: true},           // repeated preset
		{ID: "a4", Preset: "myspace", Enabled: true},            // unknown preset
		{ID: "a5", Preset: "x", Domain: "x.com", Enabled: true}, // preset with a domain
		{ID: "a2", Preset: "instagram", Enabled: true},          // repeated ID
		{ID: "a6", Domain: "example.org", Enabled: true},        // kept
		{ID: "A7", Preset: "x", Enabled: true},                  // bad ID
		{ID: "a8", Preset: "x", Enabled: false},                 // kept
		{ID: "a9", Domain: "pbs.twimg.com", Enabled: true},      // covered by x: dropped
	}
	got := sanitizeDesyncCustom(in)
	want := []DesyncCustomEntry{in[1], in[6], in[8]}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("sanitize:\n got %+v\nwant %+v", got, want)
	}
}

func TestDesyncSaveSelectionKeepsPresetWinners(t *testing.T) {
	withDesyncPrefs(t, DesyncSettings{Enabled: true, DiscordTTL: 4, Custom: []DesyncCustomEntry{
		{ID: "0a1b2c3d", Preset: "facebook", Enabled: true},
		{ID: "5a6b7c8d", Domain: "example.org", Enabled: true},
	}})
	prefsMu.Lock()
	prefsCurrent.DesyncCache = []DesyncNetworkCache{{Network: "net-a", Custom: map[string]string{"preset:facebook": "r3", "preset:x": "r1", "facebook": "r2"}}}
	prefsMu.Unlock()
	saveDesyncSelection("net-a", desync.CustomService("example.org"), "r4")
	cache := currentDesktopPrefs().DesyncCache[0].Custom
	if want := map[string]string{"preset:facebook": "r3", "example.org": "r4"}; !reflect.DeepEqual(cache, want) {
		t.Fatalf("cache: %+v", cache)
	}
	if got := desyncCachedSelection("net-a", desync.CustomService(DesyncCustomEntry{Preset: "facebook"}.key())); got != "r3" {
		t.Fatalf("cached preset winner: %q", got)
	}
}
