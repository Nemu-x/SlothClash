package main

import (
	"encoding/hex"
	"encoding/json"
	"os"
	"strings"
	"testing"
)

// withDesyncReportKey sets the install's report secret for the test.
func withDesyncReportKey(t *testing.T, secret string) {
	t.Helper()
	previous := storedDesyncReportKey()
	prefsDesyncReportKey.Store(secret)
	t.Cleanup(func() { prefsDesyncReportKey.Store(previous) })
}

func TestDesyncReportKeyIsGeneratedOncePersistedAndNotExposed(t *testing.T) {
	withDesyncReportKey(t, "")
	prefsMu.Lock()
	previous := prefsCurrent
	prefsMu.Unlock()
	t.Cleanup(func() {
		prefsMu.Lock()
		prefsCurrent = previous
		savePrefsBestEffort(prefsCurrent)
		prefsMu.Unlock()
	})

	key := desyncReportKey()
	if len(key) != 32 {
		t.Fatalf("key length %d", len(key))
	}
	if again := desyncReportKey(); hex.EncodeToString(again) != hex.EncodeToString(key) {
		t.Fatal("the key changed between uses")
	}
	secret := hex.EncodeToString(key)
	path, err := prefsStorePath()
	if err != nil {
		t.Fatal(err)
	}
	disk, err := os.ReadFile(path)
	if err != nil || !strings.Contains(string(disk), secret) {
		t.Fatalf("key not saved to prefs.json (err=%v)", err)
	}
	// A restart reads the same key back.
	prefsDesyncReportKey.Store("")
	loadDesktopPrefs()
	if storedDesyncReportKey() != secret {
		t.Fatal("key not restored from prefs.json")
	}
	// The UI-facing prefs never carry it.
	ui, _ := json.Marshal((&App{}).GetDesktopPrefs())
	if strings.Contains(string(ui), secret) || strings.Contains(string(ui), "desyncReportKey") {
		t.Fatalf("key exposed through GetDesktopPrefs: %s", ui)
	}
}

func TestDesyncReportDigestFormat(t *testing.T) {
	a, b := make([]byte, 32), make([]byte, 32)
	b[0] = 1
	d := desyncReportDigest(a, "example.org")
	if len(d) != 12 {
		t.Fatalf("digest %q is not 12 hex characters", d)
	}
	if _, err := hex.DecodeString(d); err != nil {
		t.Fatal(err)
	}
	if d != desyncReportDigest(a, "example.org") || d == desyncReportDigest(b, "example.org") || d == desyncReportDigest(a, "example.net") {
		t.Fatal("digest is not a keyed function of the value")
	}
}
