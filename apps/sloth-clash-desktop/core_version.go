package main

import (
	"io/fs"
	"regexp"
	"strings"
	"sync"
)

// The mihomo version we ship is pinned in scripts/prebuild.mjs
// (META_VERSION_PINNED). prebuild writes it to build/sidecar/core-version.txt
// next to the core binary, and main.go's `//go:embed all:build/sidecar` carries
// it into the binary, so Go can name the exact core without spawning it.
//
// Consumers today: the subscription User-Agent (subscription_user_agent.go).
// Panels such as mikan gate newer protocol output on the core version named in
// the UA, so an absent or wrong version silently degrades what users receive.
const embeddedCoreVersionFile = "build/sidecar/core-version.txt"

var coreVersionPattern = regexp.MustCompile(`^v?(\d+\.\d+\.\d+)$`)

// coreVersionFromFS reads and validates the pinned core version from a bundle.
// Returns "v<major>.<minor>.<patch>" or "" when the file is missing, empty or
// not a plain semver — never a half-parsed or fabricated version.
func coreVersionFromFS(bundle fs.FS) string {
	if bundle == nil {
		return ""
	}
	b, err := fs.ReadFile(bundle, embeddedCoreVersionFile)
	if err != nil {
		return ""
	}
	raw := strings.TrimSpace(string(b))
	// First line only: tolerate a trailing newline or an editor's extra line.
	if i := strings.IndexAny(raw, "\r\n"); i >= 0 {
		raw = strings.TrimSpace(raw[:i])
	}
	m := coreVersionPattern.FindStringSubmatch(raw)
	if m == nil {
		return ""
	}
	return "v" + m[1]
}

var (
	embeddedCoreVersionOnce  sync.Once
	embeddedCoreVersionValue string
)

// embeddedCoreVersion returns the pinned core version baked into this binary,
// read once. "" when the build was made without the version file.
func embeddedCoreVersion() string {
	embeddedCoreVersionOnce.Do(func() {
		embeddedCoreVersionValue = coreVersionFromFS(bundledResources)
	})
	return embeddedCoreVersionValue
}
