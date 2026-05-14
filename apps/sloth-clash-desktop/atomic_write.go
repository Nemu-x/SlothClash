package main

import (
	"os"
	"path/filepath"
)

// atomicWriteFile writes `data` to `path` in a crash-safe way: a temp file in
// the same directory is created and renamed over the target. The rename is
// atomic on every filesystem we ship to (NTFS, ext4, APFS, btrfs), so a
// process kill between `truncate` and `write` can never leave the target
// half-written — which `os.WriteFile` allows.
//
// Why this matters: config.yaml, subscription.cache.yaml, profiles.json and
// prefs.json are all read on next startup. A corrupted YAML/JSON breaks
// Mihomo boot or wipes user state with no obvious cause. Verge's
// `help::save_yaml` uses the same temp+rename pattern.
//
// Caveats:
//   - The target's parent directory must exist (we don't auto-mkdir).
//   - On Windows, file permissions (`perm`) are largely advisory but we still
//     call Chmod for symmetry with Unix semantics.
//   - If `Rename` fails, the temp file is cleaned up so we don't leave
//     `.swap-*` litter in the runtime directory.
func atomicWriteFile(path string, data []byte, perm os.FileMode) error {
	dir := filepath.Dir(path)
	tmp, err := os.CreateTemp(dir, ".swap-*")
	if err != nil {
		return err
	}
	tmpPath := tmp.Name()
	// Cleanup guard — if any of the steps below errors, the partial temp file
	// should not survive. Remove returns a benign "no such file" once we
	// successfully renamed it, which we ignore.
	defer func() {
		_ = os.Remove(tmpPath)
	}()
	if _, err := tmp.Write(data); err != nil {
		_ = tmp.Close()
		return err
	}
	// Best-effort chmod; on Windows this only flips the read-only bit.
	if err := tmp.Chmod(perm); err != nil {
		_ = tmp.Close()
		return err
	}
	// Sync the data to disk before the rename. Without this, an OS crash
	// (not just a process kill) can leave the rename committed but the bytes
	// not flushed — same end-state as a torn write.
	if err := tmp.Sync(); err != nil {
		_ = tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	return os.Rename(tmpPath, path)
}
