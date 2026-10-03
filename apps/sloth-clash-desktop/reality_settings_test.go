package main

import (
	"embed"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestParseRealityClientVersion(t *testing.T) {
	for in, want := range map[string]string{
		"": "", "  ": "", "26.9.9": "26.9.9", "v1.9.0": "1.9.0", "025.01.1": "25.1.1", "255.255.255": "255.255.255",
	} {
		got, err := parseRealityClientVersion(in)
		if err != nil || got != want {
			t.Errorf("parseRealityClientVersion(%q) = %q, %v; want %q", in, got, err, want)
		}
	}
	for _, in := range []string{"1.9", "1.9.0.0", "256.0.0", "-1.0.0", "1.x.0", "1.9.0-rc1", "1..0"} {
		if _, err := parseRealityClientVersion(in); err == nil {
			t.Errorf("parseRealityClientVersion(%q) accepted an invalid version", in)
		}
	}
}

func TestRealityMLKEMMapping(t *testing.T) {
	for in, want := range map[string]string{
		"": "auto", "auto": "auto", "Always": "on", "on": "on", "never": "off", "OFF": "off", "bogus": "auto",
	} {
		if got := coreRealityMLKEM(in); got != want {
			t.Errorf("coreRealityMLKEM(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestApplyRealityOverlay(t *testing.T) {
	cases := []struct {
		name    string
		sub     map[string]any
		prefs   RealitySettings
		mlkem   string
		version string // "" = key absent
	}{
		{"defaults", map[string]any{}, RealitySettings{}, "auto", ""},
		{"user wins", map[string]any{"reality-mlkem": "off", "reality-client-version": "1.8.2"},
			RealitySettings{MLKEM: "always", ClientVersion: "26.9.9"}, "on", "26.9.9"},
		{"subscription version inherited", map[string]any{"reality-client-version": "1.9.0"}, RealitySettings{}, "auto", "1.9.0"},
		{"bad subscription values dropped", map[string]any{"reality-mlkem": "always", "reality-client-version": "1.300.0"},
			RealitySettings{}, "auto", ""},
	}
	for _, c := range cases {
		applyRealityOverlay(c.sub, c.prefs)
		if got := c.sub["reality-mlkem"]; got != c.mlkem {
			t.Errorf("%s: reality-mlkem = %v, want %s", c.name, got, c.mlkem)
		}
		got, has := c.sub["reality-client-version"]
		if c.version == "" && has {
			t.Errorf("%s: reality-client-version = %v, want absent", c.name, got)
		}
		if c.version != "" && got != c.version {
			t.Errorf("%s: reality-client-version = %v, want %s", c.name, got, c.version)
		}
	}
}

func TestSetRealitySettingsRejectsBadVersion(t *testing.T) {
	prev := currentDesktopPrefs()
	t.Cleanup(func() {
		prefsMu.Lock()
		prefsCurrent = prev
		prefsMu.Unlock()
	})
	a := &App{}
	if _, err := a.SetRealitySettings(RealitySettings{MLKEM: "always", ClientVersion: "1.2"}); err == nil {
		t.Fatal("SetRealitySettings accepted an invalid version")
	}
	if got := currentDesktopPrefs().Reality; got != prev.Reality {
		t.Fatalf("rejected input changed prefs: %+v", got)
	}
}

// TestRealityKeysReachTheCore runs the full config pipeline with a non-default
// policy and, when a core is available (SLOTH_MIHOMO_BIN or the provisioned
// sidecar), has it validate the result: the patched core must accept the keys
// and an unpatched one must ignore them.
func TestRealityKeysReachTheCore(t *testing.T) {
	prev := currentDesktopPrefs()
	t.Cleanup(func() {
		prefsMu.Lock()
		prefsCurrent = prev
		prefsMu.Unlock()
	})
	prefsMu.Lock()
	prefsCurrent.Reality = RealitySettings{MLKEM: "always", ClientVersion: "1.9.0"}
	prefsMu.Unlock()

	var b embed.FS
	a := NewApp(b)
	dir := t.TempDir()
	if err := a.writeRuntimeConfig(dir, "https://example.com/sub", "", "", "", "", 9090, 7890, "secret", "rule", false, false); err != nil {
		t.Fatalf("writeRuntimeConfig: %v", err)
	}
	cfg := readYAMLMapForTest(t, filepath.Join(dir, "config.yaml"))
	if cfg["reality-mlkem"] != "on" || cfg["reality-client-version"] != "1.9.0" {
		t.Fatalf("REALITY keys not written: mlkem=%v version=%v", cfg["reality-mlkem"], cfg["reality-client-version"])
	}
	raw, err := os.ReadFile(filepath.Join(dir, "config.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	text := string(raw)
	if i, j := strings.Index(text, "\nreality-mlkem:"), strings.Index(text, "\nproxies:"); i < 0 || (j >= 0 && i > j) {
		t.Errorf("reality-mlkem is not placed before proxies (canonical order)")
	}

	bin := testCoreBinaryForPassthrough()
	if bin == "" {
		t.Skip("no core binary available (set SLOTH_MIHOMO_BIN or run `pnpm run prebuild`)")
	}
	if err := runConfigPreflight(bin, dir); err != nil {
		t.Fatalf("core rejected a config carrying the REALITY keys: %v", err)
	}
}
