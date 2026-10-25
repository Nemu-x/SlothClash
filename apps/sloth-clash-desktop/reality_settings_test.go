package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"SlothClashDesktop/internal/runtimecfg"
	"SlothClashDesktop/internal/runtimecfg/runtimecfgtest"
)

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

	dir := t.TempDir()
	if err := runtimecfg.Write(runtimecfg.Input{
		DataDir:                dir,
		SubURL:                 "https://example.com/sub",
		CtrlPort:               9090,
		MixedPort:              7890,
		Secret:                 "secret",
		WithExternalController: false,
		Traffic:                "rule",
		EnableTun:              false,
		Env:                    currentRuntimeConfigEnv(),
	}, nil); err != nil {
		t.Fatalf("runtimecfg.Write: %v", err)
	}
	cfg := runtimecfgtest.ReadYAMLMap(t, filepath.Join(dir, "config.yaml"))
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

	bin := runtimecfgtest.CoreBinary()
	if bin == "" {
		t.Skip("no core binary available (set SLOTH_MIHOMO_BIN or run `pnpm run prebuild`)")
	}
	if err := runtimecfg.Preflight(bin, dir, currentRuntimeConfigPrefs()); err != nil {
		t.Fatalf("core rejected a config carrying the REALITY keys: %v", err)
	}
}
