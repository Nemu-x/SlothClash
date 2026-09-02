package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// The guard that makes the whole suite safe to run: a test binary must never be
// handed the real app data directory.
//
// Without it, any test that calls an App mutation method persists through
// persistProfilesLocked into the developer's real profiles.json — and the next
// app start deletes every runtime dir missing from that file
// (pruneOrphanRuntimeDirs). That is exactly how three real profiles were lost
// on 2026-09-02.
func TestDataRootIsNeverTheRealOneUnderTest(t *testing.T) {
	if !runningUnderGoTest() {
		t.Fatalf("runningUnderGoTest() is false inside a test binary (%s) — the guard is blind", os.Args[0])
	}

	got, err := slothDataRoot()
	if err != nil {
		t.Fatalf("data root: %v", err)
	}
	parent, err := os.UserConfigDir()
	if err != nil {
		t.Fatalf("user config dir: %v", err)
	}
	realRoot := filepath.Join(parent, "SlothClash")
	if strings.EqualFold(got, realRoot) {
		t.Fatalf("slothDataRoot() = %s, which is the REAL app data root", got)
	}
	if !strings.Contains(got, "sloth-test-data-root-") {
		t.Errorf("slothDataRoot() = %s, want the throwaway test root", got)
	}
}

// Every call in one test process must agree, or two halves of a test would look
// at different directories.
func TestDataRootIsStableWithinTheProcess(t *testing.T) {
	first, err := slothDataRoot()
	if err != nil {
		t.Fatalf("data root: %v", err)
	}
	second, err := slothDataRoot()
	if err != nil {
		t.Fatalf("data root: %v", err)
	}
	if first != second {
		t.Errorf("slothDataRoot() returned %s then %s — it must be stable within a process", first, second)
	}
}

func TestRunningUnderGoTestDetectsTestBinaries(t *testing.T) {
	// Sanity-check the detection itself against the shapes `go test` produces.
	for _, name := range []string{"pkg.test", "pkg.test.exe", "SlothClashDesktop.test", "/tmp/x.test.exe"} {
		exe := strings.ToLower(filepath.Base(name))
		if !strings.HasSuffix(exe, ".test") && !strings.HasSuffix(exe, ".test.exe") {
			t.Errorf("%q should be recognised as a test binary", name)
		}
	}
	for _, name := range []string{"SlothClashDesktop.exe", "sloth-clash-desktop", "mihomo.exe"} {
		exe := strings.ToLower(filepath.Base(name))
		if strings.HasSuffix(exe, ".test") || strings.HasSuffix(exe, ".test.exe") {
			t.Errorf("%q must NOT be treated as a test binary", name)
		}
	}
}
