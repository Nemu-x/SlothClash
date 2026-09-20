package main

import (
	"embed"
	"encoding/json"
	"os"
	"testing"
)

func TestUISettingsDefaults(t *testing.T) {
	var u UISettings
	if u.IsSet() {
		t.Fatal("zero value must read as not-yet-migrated")
	}
	if u.IsStartMinimized() || u.IsAutoConnectOnStartup() {
		t.Fatal("start-minimized and auto-connect default to off")
	}
	if !u.IsCloseToTray() {
		t.Fatal("close-to-tray defaults to on")
	}
	// Explicit false must win over the close-to-tray default.
	u.CloseToTray = boolPtr(false)
	if u.IsCloseToTray() {
		t.Fatal("explicit closeToTray=false ignored")
	}
	if !u.IsSet() {
		t.Fatal("any explicit field marks the section as set")
	}
}

func TestUISettingsNormalizedFillsEveryField(t *testing.T) {
	n := UISettings{StartMinimized: boolPtr(true)}.normalized()
	if n.StartMinimized == nil || n.AutoConnectOnStartup == nil || n.CloseToTray == nil {
		t.Fatalf("normalized must set all fields: %+v", n)
	}
	if !*n.StartMinimized || *n.AutoConnectOnStartup || !*n.CloseToTray {
		t.Fatalf("normalized changed values: %+v", n)
	}
}

// SetUISettings must (1) persist a fully-set section atomically so a later
// launch reads exactly what was toggled, and (2) mirror close-to-tray into the
// live App flag that beforeClose consults.
func TestSetUISettingsPersistsAndMirrorsCloseToTray(t *testing.T) {
	prefsMu.Lock()
	prev := prefsCurrent
	prefsCurrent = DesktopPrefs{}
	prefsMu.Unlock()
	t.Cleanup(func() {
		prefsMu.Lock()
		prefsCurrent = prev
		prefsMu.Unlock()
		_ = saveDesktopPrefsLocked(prev)
	})

	var b embed.FS
	a := NewApp(b)
	if !a.closeToTray {
		t.Fatal("fresh App must default to close-to-tray on")
	}

	out := a.SetUISettings(UISettings{
		StartMinimized:       boolPtr(true),
		AutoConnectOnStartup: boolPtr(true),
		CloseToTray:          boolPtr(false),
	})
	if !out.UI.IsSet() || !out.UI.IsStartMinimized() || !out.UI.IsAutoConnectOnStartup() || out.UI.IsCloseToTray() {
		t.Fatalf("returned snapshot does not reflect the write: %+v", out.UI)
	}
	a.mu.RLock()
	ct := a.closeToTray
	a.mu.RUnlock()
	if ct {
		t.Fatal("closeToTray=false was not mirrored into the live App flag")
	}

	// Re-read from disk: this is what the next launch (main.go StartHidden,
	// startup closeToTray init) will see.
	p, err := prefsStorePath()
	if err != nil {
		t.Fatal(err)
	}
	raw, err := os.ReadFile(p)
	if err != nil {
		t.Fatalf("prefs.json not written: %v", err)
	}
	var disk DesktopPrefs
	if err := json.Unmarshal(raw, &disk); err != nil {
		t.Fatal(err)
	}
	if !disk.UI.IsStartMinimized() || !disk.UI.IsAutoConnectOnStartup() || disk.UI.IsCloseToTray() {
		t.Fatalf("on-disk UI section wrong: %s", string(raw))
	}

	// A partial write (only one field) still lands a fully-set section.
	out = a.SetUISettings(UISettings{CloseToTray: boolPtr(true)})
	if out.UI.StartMinimized == nil || *out.UI.StartMinimized {
		t.Fatalf("partial write must reset unspecified fields to defaults, got %+v", out.UI)
	}
	a.mu.RLock()
	ct = a.closeToTray
	a.mu.RUnlock()
	if !ct {
		t.Fatal("closeToTray=true was not mirrored back")
	}
}
