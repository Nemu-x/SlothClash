package main

import (
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func brandHeadersOf(kv map[string]string) http.Header {
	h := http.Header{}
	for k, v := range kv {
		h.Set(k, v)
	}
	return h
}

func TestParseBrandManifestGate(t *testing.T) {
	cases := []struct {
		name    string
		headers map[string]string
		wantNil bool
	}{
		{"gate absent", map[string]string{brandHdrAccentColor: "#3d7eff"}, true},
		{"gate false", map[string]string{brandHdrEnabled: "false", brandHdrName: "Op"}, true},
		{"gate garbage", map[string]string{brandHdrEnabled: "enabled!", brandHdrName: "Op"}, true},
		{"gate true", map[string]string{brandHdrEnabled: "true"}, false},
		{"gate 1", map[string]string{brandHdrEnabled: "1"}, false},
		{"gate quoted", map[string]string{brandHdrEnabled: `"true"`}, false},
		// Android namespace must never activate the desktop client.
		{"android namespace only", map[string]string{
			"X-Branding-Enabled": "true",
			"X-Brand-Name":       "Foo",
		}, true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			h := brandHeadersOf(tc.headers)
			got := parseBrandManifest(h.Get)
			if (got == nil) != tc.wantNil {
				t.Fatalf("parseBrandManifest = %v, wantNil=%v", got, tc.wantNil)
			}
		})
	}
}

func TestParseBrandManifestFieldValidation(t *testing.T) {
	h := brandHeadersOf(map[string]string{
		brandHdrEnabled:        "true",
		brandHdrName:           "  BeletVPN  ",
		brandHdrAccentColor:    "reddish", // invalid → dropped, rest survives
		brandHdrSupportURL:     "https://support.example.com/help",
		brandHdrWebsiteURL:     "javascript:alert(1)",         // scheme → dropped
		brandHdrRenewURL:       "http://renew.example.com",    // plain http → dropped
		brandHdrTelegramURL:    "tg://resolve?domain=example", // tg allowed
		brandHdrGreeting:       strings.Repeat("я", 300),      // over cap → dropped
		brandHdrHideGlobalMode: "true",
	})
	m := parseBrandManifest(h.Get)
	if m == nil {
		t.Fatal("expected manifest, got nil")
	}
	if m.Name != "BeletVPN" {
		t.Fatalf("Name = %q", m.Name)
	}
	if m.AccentColor != "" {
		t.Fatalf("invalid accent survived: %q", m.AccentColor)
	}
	if m.SupportURL != "https://support.example.com/help" {
		t.Fatalf("SupportURL = %q", m.SupportURL)
	}
	if m.WebsiteURL != "" {
		t.Fatalf("javascript: URL survived: %q", m.WebsiteURL)
	}
	if m.RenewURL != "" {
		t.Fatalf("http:// URL survived: %q", m.RenewURL)
	}
	if m.TelegramURL == "" {
		t.Fatal("tg:// URL dropped")
	}
	if m.Greeting != "" {
		t.Fatalf("over-cap greeting survived: %q", m.Greeting)
	}
	if !m.HideGlobalMode || m.HideAdvanced {
		t.Fatalf("hide flags wrong: %+v", m)
	}
}

func TestParseBrandManifestAccentAndCyrillic(t *testing.T) {
	h := brandHeadersOf(map[string]string{
		brandHdrEnabled:         "true",
		brandHdrAccentColor:     "#3D7EFF",
		brandHdrUserDisplayName: "Пользователь ⚡",
	})
	m := parseBrandManifest(h.Get)
	if m == nil {
		t.Fatal("expected manifest")
	}
	if m.AccentColor != "#3d7eff" {
		t.Fatalf("AccentColor = %q, want normalized lowercase", m.AccentColor)
	}
	if m.UserDisplayName != "Пользователь ⚡" {
		t.Fatalf("UserDisplayName mangled: %q", m.UserDisplayName)
	}
	// #rgb shorthand expands like the frontend normalizeHex.
	h2 := brandHeadersOf(map[string]string{brandHdrEnabled: "true", brandHdrAccentColor: "#F0a"})
	if m2 := parseBrandManifest(h2.Get); m2 == nil || m2.AccentColor != "#ff00aa" {
		t.Fatalf("shorthand accent = %+v", m2)
	}
}

func TestParseBrandManifestDeviceCounts(t *testing.T) {
	h := brandHeadersOf(map[string]string{
		brandHdrEnabled:      "true",
		brandHdrDevicesUsed:  "3",
		brandHdrDevicesLimit: "5",
	})
	m := parseBrandManifest(h.Get)
	if m == nil || m.DevicesUsed != 3 || m.DevicesLimit != 5 {
		t.Fatalf("device counts = %+v", m)
	}

	// Garbage / negative / absurd values must read as "not reported" (0) rather
	// than rendering nonsense next to the user's real quota.
	for _, bad := range []string{"", "many", "-1", "99999999", "3.5"} {
		h := brandHeadersOf(map[string]string{
			brandHdrEnabled:     "true",
			brandHdrDevicesUsed: bad,
		})
		if m := parseBrandManifest(h.Get); m == nil || m.DevicesUsed != 0 {
			t.Fatalf("DevicesUsed for %q = %+v, want 0", bad, m)
		}
	}
}

func TestPersistBrandManifestLifecycle(t *testing.T) {
	dir := t.TempDir()
	branded := brandHeadersOf(map[string]string{
		brandHdrEnabled: "true",
		brandHdrName:    "Op",
	})

	// HTTP subscription URL → headers untrusted, nothing written.
	persistBrandManifestFromHeaders(dir, "http://panel.example.com/sub", branded)
	if _, err := os.Stat(brandManifestPath(dir)); !os.IsNotExist(err) {
		t.Fatal("manifest written for plain-http subscription")
	}

	// HTTPS + gate → persisted and readable.
	persistBrandManifestFromHeaders(dir, "https://panel.example.com/sub", branded)
	m := readBrandManifest(dir)
	if m == nil || m.Name != "Op" {
		t.Fatalf("readBrandManifest = %+v", m)
	}

	// Gate disappears on a later refresh → cached manifest cleared.
	persistBrandManifestFromHeaders(dir, "https://panel.example.com/sub", brandHeadersOf(map[string]string{}))
	if readBrandManifest(dir) != nil {
		t.Fatal("manifest survived gate removal")
	}

	// Corrupt cache file reads as nil, not an error.
	if err := os.WriteFile(filepath.Join(dir, "brand.manifest.json"), []byte("{nope"), 0o600); err != nil {
		t.Fatal(err)
	}
	if readBrandManifest(dir) != nil {
		t.Fatal("corrupt manifest should read as nil")
	}
}
