package main

import (
	"encoding/json"
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

// CoreBuildInfo says what the shipped core is made of: the upstream mihomo tag
// plus the SlothClash patch series (core/patches/mihomo). prebuild writes it to
// build/sidecar/core-build.json next to the binary; Settings → Info shows it so
// users can see the core is upstream + N patches and follow the link to them.
type CoreBuildInfo struct {
	Version    string   `json:"version"`
	Source     string   `json:"source"` // patched | stock | local; "" = unknown build
	Ref        string   `json:"ref,omitempty"` // commit the build came from; "" = local build
	Patches    []string `json:"patches"`
	PatchesURL string   `json:"patchesUrl"`
	DocURL     string   `json:"docUrl"`
}

const (
	embeddedCoreBuildFile = "build/sidecar/core-build.json"
	slothRepoURL          = "https://github.com/Nemu-x/SlothClash"
)

func coreBuildInfoFromFS(bundle fs.FS) CoreBuildInfo {
	info := CoreBuildInfo{Patches: []string{}}
	if bundle != nil {
		if b, err := fs.ReadFile(bundle, embeddedCoreBuildFile); err == nil {
			var raw CoreBuildInfo
			if json.Unmarshal(b, &raw) == nil {
				info.Source = strings.TrimSpace(raw.Source)
				info.Ref = strings.TrimSpace(raw.Ref)
				if raw.Patches != nil {
					info.Patches = raw.Patches
				}
			}
		}
		info.Version = coreVersionFromFS(bundle)
	}
	// Link the patches as of the commit the release was built from, so the
	// page shows exactly the series this binary carries.
	ref := "main"
	if info.Ref != "" {
		ref = info.Ref
	}
	info.PatchesURL = slothRepoURL + "/tree/" + ref + "/core/patches/mihomo"
	info.DocURL = slothRepoURL + "/blob/" + ref + "/docs/core-patches.md"
	return info
}

// GetCoreBuild is the Wails-exposed description of the embedded core.
func (a *App) GetCoreBuild() CoreBuildInfo {
	_ = a
	return coreBuildInfoFromFS(bundledResources)
}
