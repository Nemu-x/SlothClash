package main

import (
	"bufio"
	"errors"
	"net/http"
	"path/filepath"
	"strings"

	wailsrt "github.com/wailsapp/wails/v2/pkg/runtime"
)

// ApplyBrandDebugHeaders (dev builds only) feeds hand-typed header lines
// ("X-Brand-Desktop-Enabled: true", one per line, # comments allowed) through
// the EXACT production capture path — parseBrandManifest → validation →
// persist → logo cache — for the active profile, so branding can be exercised
// without a real panel. Empty input (or input without the gate) clears the
// manifest, same as a panel turning branding off. The synthetic https:// URL
// deliberately satisfies the HTTPS-only trust rule.
//
// The override lives until the next real subscription refresh overwrites it —
// self-healing by design, a debug session can't permanently mask real headers.
func (a *App) ApplyBrandDebugHeaders(raw string) (ActiveBranding, error) {
	if a.ctx == nil || wailsrt.Environment(a.ctx).BuildType != "dev" {
		return ActiveBranding{}, errors.New("brand debug headers are available in dev builds only")
	}
	a.mu.Lock()
	id := strings.TrimSpace(a.state.Profile.ActiveProfileID)
	a.mu.Unlock()
	if id == "" {
		return ActiveBranding{}, errors.New("no active profile")
	}
	root, err := slothDataRoot()
	if err != nil {
		return ActiveBranding{}, err
	}
	dataDir := filepath.Join(root, "runtime", id)

	hdr := http.Header{}
	sc := bufio.NewScanner(strings.NewReader(raw))
	for sc.Scan() {
		line := strings.TrimSpace(sc.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		k, v, ok := strings.Cut(line, ":")
		if !ok {
			continue
		}
		if key := strings.TrimSpace(k); key != "" {
			hdr.Set(key, strings.TrimSpace(v))
		}
	}
	persistBrandManifestFromHeaders(dataDir, "https://brand-debug.local/sub", hdr)
	return a.GetActiveBranding(), nil
}
