package main

import (
	"encoding/json"
	"fmt"
	"reflect"
	"strings"
	"testing"

	"SlothClashDesktop/internal/desync"
)

func customError(err error) *desyncError {
	custom, _ := err.(*desyncError)
	return custom
}

func TestDesyncServiceListMatchesCatalog(t *testing.T) {
	list := (&App{}).ListDesyncServices()
	if len(list) != len(desync.Services) {
		t.Fatalf("list: %+v", list)
	}
	for i, p := range desync.Services {
		if list[i].ID != p.ID || list[i].Name != p.Name || !reflect.DeepEqual(list[i].Domains, p.Domains) {
			t.Fatalf("service %d: %+v", i, list[i])
		}
	}
	// A copy: the UI cannot edit the catalog through the result.
	list[0].Domains[0] = "changed.example"
	if desync.Services[0].Domains[0] == "changed.example" {
		t.Fatal("ListDesyncServices exposes the catalog slice")
	}
}

func TestDesyncServiceLifecycle(t *testing.T) {
	a := desyncProfileApp(t, nil, nil)
	if _, err := a.AddDesyncService("myspace"); !isDesyncCode(err, "unknown_service") {
		t.Fatalf("unknown service: %v", err)
	}
	status, err := a.AddDesyncService("facebook")
	if err != nil || len(status.Settings.Custom) != 1 {
		t.Fatalf("add: %+v %v", status.Settings.Custom, err)
	}
	entry := status.Settings.Custom[0]
	if entry.Service != "facebook" || entry.Domain != "" || !entry.Enabled || !validDesyncCustomID(entry.ID) || entry.key() != "preset:facebook" {
		t.Fatalf("entry: %+v", entry)
	}
	if _, err := a.AddDesyncService("facebook"); !isDesyncCode(err, "duplicate") {
		t.Fatalf("duplicate service: %v", err)
	}
	// A user domain the service covers is rejected, naming the service, while
	// the service is in the list, also switched off.
	for _, enabled := range []bool{true, false} {
		if _, err := a.SetDesyncCustomDomainEnabled(entry.ID, enabled); err != nil {
			t.Fatal(err)
		}
		for _, domain := range []string{"facebook.com", "https://scontent-ams2-1.xx.fbcdn.net/v/x.jpg", "m.me"} {
			_, err := a.AddDesyncCustomDomain(domain)
			if c := customError(err); c == nil || c.Code != "service_overlap" || c.Detail != "facebook" || err.Error() != "desync: service_overlap: facebook" {
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
		t.Fatalf("remove service: %+v %v", status.Settings.Custom, err)
	}
	// After removing the service its domains are plain-addable again.
	if _, err := a.AddDesyncCustomDomain("facebook.com"); err != nil {
		t.Fatalf("after remove: %v", err)
	}
	// Prefs round-trip; a plain entry has no "service" key.
	raw, _ := json.Marshal(currentDesktopPrefs().Desync)
	if strings.Contains(string(raw), `"service"`) || strings.Contains(string(raw), `"preset"`) {
		t.Fatalf("plain entries serialize a service key: %s", raw)
	}
	status, _ = a.AddDesyncService("x")
	raw, _ = json.Marshal(currentDesktopPrefs().Desync)
	var back DesyncSettings
	if json.Unmarshal(raw, &back) != nil || !reflect.DeepEqual(effectiveDesyncSettings(back).Custom, status.Settings.Custom) || !strings.Contains(string(raw), `"service":"x"`) {
		t.Fatalf("round trip: %s", raw)
	}
}

// Entries stored before the rename carry "preset"; they read as services.
func TestDesyncCustomEntryReadsLegacyPresetKey(t *testing.T) {
	var got []DesyncCustomEntry
	raw := `[{"id":"a1","domain":"","preset":"facebook","enabled":true},{"id":"a2","domain":"example.org","enabled":false},{"id":"a3","domain":"","preset":"x","service":"instagram","enabled":true}]`
	if err := json.Unmarshal([]byte(raw), &got); err != nil {
		t.Fatal(err)
	}
	want := []DesyncCustomEntry{{ID: "a1", Service: "facebook", Enabled: true}, {ID: "a2", Domain: "example.org"}, {ID: "a3", Service: "instagram", Enabled: true}}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("legacy entries: %+v", got)
	}
	out, _ := json.Marshal(got[0])
	if string(out) != `{"id":"a1","domain":"","service":"facebook","enabled":true}` {
		t.Fatalf("written as %s", out)
	}
}

// A plain entry the service covers is replaced by it (the owner's case: a
// "facebook.com" typed before services existed); others stay in order.
func TestDesyncServiceReplacesCoveredPlainEntries(t *testing.T) {
	a := desyncProfileApp(t, desyncPtr(DesyncSettings{Enabled: true, Custom: []DesyncCustomEntry{
		{ID: "0a1b2c3d", Domain: "facebook.com", Enabled: true},
		{ID: "1b2c3d4e", Domain: "example.org", Enabled: true},
		{ID: "2c3d4e5f", Domain: "video.xx.fbcdn.net", Enabled: false},
		{ID: "3d4e5f6a", Domain: "instagram.com", Enabled: true},
	}}), nil)
	status, err := a.AddDesyncService("facebook")
	if err != nil {
		t.Fatal(err)
	}
	got := status.Settings.Custom
	if len(got) != 3 || got[0].ID != "1b2c3d4e" || got[1].ID != "3d4e5f6a" || got[2].Service != "facebook" || !got[2].Enabled {
		t.Fatalf("custom after service: %+v", got)
	}
	// Instagram shares fbcdn.net with Facebook: both services coexist (the
	// overlap is resolved by catalog order at generation time), and the
	// plain instagram.com entry is replaced.
	status, err = a.AddDesyncService("instagram")
	if err != nil || len(status.Settings.Custom) != 3 || status.Settings.Custom[1].Service != "facebook" || status.Settings.Custom[2].Service != "instagram" {
		t.Fatalf("instagram: %+v %v", status.Settings.Custom, err)
	}
}

func TestDesyncServiceCountsAsOneEntryTowardTheCap(t *testing.T) {
	var full []DesyncCustomEntry
	for i := range desyncCustomLimit - 1 {
		full = append(full, DesyncCustomEntry{ID: fmt.Sprintf("e%d", i), Domain: fmt.Sprintf("site%d.example.org", i), Enabled: true})
	}
	a := desyncProfileApp(t, desyncPtr(DesyncSettings{Enabled: true, Custom: full}), nil)
	if _, err := a.AddDesyncService("x"); err != nil {
		t.Fatalf("service as the last entry: %v", err)
	}
	if _, err := a.AddDesyncService("facebook"); !isDesyncCode(err, "limit") {
		t.Fatalf("over the cap: %v", err)
	}
	if _, err := a.AddDesyncCustomDomain("other.example.net"); !isDesyncCode(err, "limit") {
		t.Fatalf("plain over the cap: %v", err)
	}
	// Replacing a covered entry frees its slot.
	a.mu.Lock()
	a.profiles[0].Desync.Custom[0] = DesyncCustomEntry{ID: "e0", Domain: "instagram.com", Enabled: true}
	a.mu.Unlock()
	if _, err := a.AddDesyncService("instagram"); err != nil {
		t.Fatalf("replacing at the cap: %v", err)
	}
}

func TestDesyncServiceSanitize(t *testing.T) {
	in := []DesyncCustomEntry{
		{ID: "a1", Domain: "facebook.com", Enabled: true},       // covered by the service below: dropped
		{ID: "a2", Service: "facebook", Enabled: true},           // kept
		{ID: "a3", Service: "facebook", Enabled: true},           // repeated service
		{ID: "a4", Service: "myspace", Enabled: true},            // unknown service
		{ID: "a5", Service: "x", Domain: "x.com", Enabled: true}, // service with a domain
		{ID: "a2", Service: "instagram", Enabled: true},          // repeated ID
		{ID: "a6", Domain: "example.org", Enabled: true},        // kept
		{ID: "A7", Service: "x", Enabled: true},                  // bad ID
		{ID: "a8", Service: "x", Enabled: false},                 // kept
		{ID: "a9", Domain: "pbs.twimg.com", Enabled: true},      // covered by x: dropped
	}
	got := sanitizeDesyncCustom(in)
	want := []DesyncCustomEntry{in[1], in[6], in[8]}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("sanitize:\n got %+v\nwant %+v", got, want)
	}
}

func TestDesyncSaveSelectionKeepsPresetWinners(t *testing.T) {
	withDesyncPrefs(t, DesyncSettings{Enabled: true, Custom: []DesyncCustomEntry{
		{ID: "0a1b2c3d", Service: "facebook", Enabled: true},
		{ID: "5a6b7c8d", Domain: "example.org", Enabled: true},
	}})
	prefsMu.Lock()
	prefsCurrent.DesyncCache = []DesyncNetworkCache{{Network: "net-a", Custom: map[string]string{"preset:facebook": presetIDOf("oob-1"), "preset:x": "r1", "facebook": "r2"}}}
	prefsMu.Unlock()
	saveDesyncSelection("net-a", desync.CustomService("example.org"), "r4")
	cache := currentDesktopPrefs().DesyncCache[0].Custom
	if want := map[string]string{"preset:facebook": presetIDOf("oob-1"), "example.org": "r4"}; !reflect.DeepEqual(cache, want) {
		t.Fatalf("cache: %+v", cache)
	}
	if got := desyncCachedSelection("net-a", desync.CustomService(DesyncCustomEntry{Service: "facebook"}.key())); got != presetIDOf("oob-1") {
		t.Fatalf("cached service winner: %q", got)
	}
}
