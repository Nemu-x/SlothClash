package runtimecfgtest

import (
	"os"
	"path/filepath"
	"strings"
)

// CoreBinary resolves a core binary to validate against:
// the explicit SLOTH_MIHOMO_BIN override first, otherwise the locally
// provisioned sidecar. Returns "" when neither is present.
func CoreBinary() string {
	if bin := strings.TrimSpace(os.Getenv("SLOTH_MIHOMO_BIN")); bin != "" {
		return bin
	}
	matches, _ := filepath.Glob(filepath.Join(appDir(), "build", "sidecar", "sloth-mihomo-*"))
	for _, m := range matches {
		// The alpha sidecar is a separate, unpinned binary — never validate against it.
		if strings.Contains(filepath.Base(m), "-alpha-") {
			continue
		}
		if abs, err := filepath.Abs(m); err == nil {
			return abs
		}
		return m
	}
	return ""
}

// appDir is the desktop app's module root (the directory holding wails.json),
// found by walking up from the working directory, so the fixtures resolve the
// same from package main's tests and from internal packages' tests. "." when
// it cannot be found.
func appDir() string {
	dir, err := os.Getwd()
	if err != nil {
		return "."
	}
	for {
		if _, err := os.Stat(filepath.Join(dir, "wails.json")); err == nil {
			return dir
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			return "."
		}
		dir = parent
	}
}
