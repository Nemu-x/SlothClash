package main

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// On-demand optional component delivery (corp-vpn-coexistence, task 1.2).
//
// The corporate-VPN feature needs OpenConnect, which must NOT ship in the base
// installer — regular users should carry no cost. This module downloads a
// pinned, signed component bundle only when the user opts in, verifies its
// SHA-256 (fail-closed), and unpacks it into the app data dir. Nothing here runs
// unless the feature is enabled. See openspec/changes/corp-vpn-coexistence.
//
// The bundle is a .tar.gz produced by our CI (relocatable: the executable +
// its dylibs + the vpnc-script), hosted as a release asset.

// componentSpec describes what to fetch, how to verify it, and where the
// executable lives inside the unpacked archive.
type componentSpec struct {
	Name    string // stable id, e.g. "openconnect"
	Version string // pinned version tag, e.g. "9.21-macos-arm64"
	URL     string // download URL of the .tar.gz bundle
	SHA256  string // hex sha256 of the archive (fail-closed verification)
	BinRel  string // path to the executable inside the archive, e.g. "openconnect"
}

// componentMaxArchiveBytes caps a component download so a hostile/corrupt asset
// can't exhaust memory or disk. The OpenConnect bundle is a few MB.
const componentMaxArchiveBytes = 64 << 20 // 64 MiB

// componentDirAt returns a component's install directory under an explicit
// components root (injectable for tests).
func componentDirAt(root, name string) string {
	return filepath.Join(root, name)
}

// componentInstalledAt reports whether the exact pinned version is already
// unpacked (its `.version` marker matches and the executable exists).
func componentInstalledAt(root string, spec componentSpec) bool {
	dir := componentDirAt(root, spec.Name)
	marker, err := os.ReadFile(filepath.Join(dir, ".version"))
	if err != nil || strings.TrimSpace(string(marker)) != spec.Version {
		return false
	}
	_, err = os.Stat(filepath.Join(dir, spec.BinRel))
	return err == nil
}

// ensureComponentAt installs the component (if the pinned version is not already
// present) under an explicit components root and returns the path to its
// executable. Fail-closed: a hash mismatch or missing binary aborts without
// leaving a half-installed component.
func ensureComponentAt(ctx context.Context, root string, spec componentSpec) (string, error) {
	dir := componentDirAt(root, spec.Name)
	binPath := filepath.Join(dir, spec.BinRel)
	if componentInstalledAt(root, spec) {
		return binPath, nil
	}
	if strings.TrimSpace(spec.URL) == "" || strings.TrimSpace(spec.SHA256) == "" {
		return "", errors.New("component spec missing url or sha256")
	}

	archive, err := downloadComponentArchive(ctx, spec.URL)
	if err != nil {
		return "", err
	}
	// Verify BEFORE touching disk.
	sum := sha256.Sum256(archive)
	if got := hex.EncodeToString(sum[:]); !strings.EqualFold(got, strings.TrimSpace(spec.SHA256)) {
		return "", fmt.Errorf("component %s hash mismatch: got %s, want %s", spec.Name, got, spec.SHA256)
	}

	// Unpack into a temp dir, then atomically swap it into place so a partial
	// extract never leaves a broken install.
	tmp := dir + ".tmp"
	_ = os.RemoveAll(tmp)
	if err := os.MkdirAll(tmp, 0o755); err != nil {
		return "", err
	}
	if err := untarGz(archive, tmp); err != nil {
		_ = os.RemoveAll(tmp)
		return "", err
	}
	if _, err := os.Stat(filepath.Join(tmp, spec.BinRel)); err != nil {
		_ = os.RemoveAll(tmp)
		return "", fmt.Errorf("component %s: %q missing from archive", spec.Name, spec.BinRel)
	}
	_ = os.Chmod(filepath.Join(tmp, spec.BinRel), 0o755)
	if err := os.WriteFile(filepath.Join(tmp, ".version"), []byte(spec.Version), 0o644); err != nil {
		_ = os.RemoveAll(tmp)
		return "", err
	}

	_ = os.RemoveAll(dir)
	if err := os.MkdirAll(filepath.Dir(dir), 0o755); err != nil {
		_ = os.RemoveAll(tmp)
		return "", err
	}
	if err := os.Rename(tmp, dir); err != nil {
		_ = os.RemoveAll(tmp)
		return "", err
	}
	return binPath, nil
}

// componentsRoot resolves the real components directory (<data>/components).
func componentsRoot() (string, error) {
	root, err := slothDataRoot()
	if err != nil {
		return "", err
	}
	return filepath.Join(root, "components"), nil
}

// ensureComponent is the production entry point: installs the component under
// the real app data dir and returns the executable path.
func ensureComponent(ctx context.Context, spec componentSpec) (string, error) {
	root, err := componentsRoot()
	if err != nil {
		return "", err
	}
	return ensureComponentAt(ctx, root, spec)
}

func downloadComponentArchive(ctx context.Context, url string) ([]byte, error) {
	ctx, cancel := context.WithTimeout(ctx, 5*time.Minute)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, err
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, fmt.Errorf("download %s: HTTP %d", url, resp.StatusCode)
	}
	// Read one byte past the cap so we can detect an over-size asset.
	data, err := io.ReadAll(io.LimitReader(resp.Body, componentMaxArchiveBytes+1))
	if err != nil {
		return nil, err
	}
	if len(data) > componentMaxArchiveBytes {
		return nil, fmt.Errorf("component archive exceeds %d bytes", componentMaxArchiveBytes)
	}
	return data, nil
}

// untarGz extracts a gzip-compressed tar into dest, guarding against path
// traversal (zip-slip): every entry must resolve inside dest, and symlinks are
// rejected (our bundles are plain files + dylibs, no links).
func untarGz(archive []byte, dest string) error {
	gz, err := gzip.NewReader(bytes.NewReader(archive))
	if err != nil {
		return err
	}
	defer gz.Close()
	tr := tar.NewReader(gz)
	cleanDest := filepath.Clean(dest)
	for {
		hdr, err := tr.Next()
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			return err
		}
		target := filepath.Join(cleanDest, hdr.Name)
		if target != cleanDest && !strings.HasPrefix(target, cleanDest+string(os.PathSeparator)) {
			return fmt.Errorf("unsafe path in archive: %q", hdr.Name)
		}
		switch hdr.Typeflag {
		case tar.TypeDir:
			if err := os.MkdirAll(target, 0o755); err != nil {
				return err
			}
		case tar.TypeReg:
			if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
				return err
			}
			f, err := os.OpenFile(target, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, os.FileMode(hdr.Mode)&0o777)
			if err != nil {
				return err
			}
			if _, err := io.Copy(f, io.LimitReader(tr, componentMaxArchiveBytes)); err != nil {
				_ = f.Close()
				return err
			}
			if err := f.Close(); err != nil {
				return err
			}
		case tar.TypeSymlink, tar.TypeLink:
			return fmt.Errorf("links in component archive not allowed: %q", hdr.Name)
		default:
			// Skip fifos/devices/pax headers.
		}
	}
	return nil
}
