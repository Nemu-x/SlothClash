package main

import (
	"os"
	"path/filepath"
	"testing"
)

// Deleted profiles leave `runtime/<id>/` behind; startup must reclaim them while
// never touching live profiles or non-profile dirs (audit finding R3).
func TestPruneOrphanRuntimeDirsIn(t *testing.T) {
	runtimeDir := t.TempDir()
	for _, d := range []string{"profile-live", "profile-orphan-1", "profile-orphan-2", "logs"} {
		if err := os.MkdirAll(filepath.Join(runtimeDir, d, "providers"), 0o755); err != nil {
			t.Fatal(err)
		}
	}

	live := map[string]struct{}{"profile-live": {}}
	if got := pruneOrphanRuntimeDirsIn(runtimeDir, live); got != 2 {
		t.Errorf("removed = %d, want 2", got)
	}

	for _, keep := range []string{"profile-live", "logs"} {
		if _, err := os.Stat(filepath.Join(runtimeDir, keep)); err != nil {
			t.Errorf("%s must be kept: %v", keep, err)
		}
	}
	for _, gone := range []string{"profile-orphan-1", "profile-orphan-2"} {
		if _, err := os.Stat(filepath.Join(runtimeDir, gone)); !os.IsNotExist(err) {
			t.Errorf("%s must be pruned, still present (%v)", gone, err)
		}
	}
}

func TestPruneOrphanRuntimeDirsIn_MissingDirIsNoop(t *testing.T) {
	if got := pruneOrphanRuntimeDirsIn(filepath.Join(t.TempDir(), "nope"), nil); got != 0 {
		t.Errorf("removed = %d, want 0", got)
	}
}
