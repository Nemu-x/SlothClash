package main

import (
	"path/filepath"
	"strings"
)

// ActiveBranding is the Wails-facing view of the active profile's branding:
// the validated manifest plus disk-cached logos as data: URIs. Zero-value
// (nil manifest) means stock UI.
type ActiveBranding struct {
	Manifest         *BrandManifest `json:"manifest"`
	LogoDataURI      string         `json:"logoDataUri,omitempty"`
	LogoLightDataURI string         `json:"logoLightDataUri,omitempty"`
}

// GetActiveBranding returns the branding of the currently active profile.
// Reads only local cache — never the network — so it is safe to call on every
// profile switch.
func (a *App) GetActiveBranding() ActiveBranding {
	a.mu.Lock()
	id := strings.TrimSpace(a.state.Profile.ActiveProfileID)
	a.mu.Unlock()
	if id == "" {
		return ActiveBranding{}
	}
	root, err := slothDataRoot()
	if err != nil {
		return ActiveBranding{}
	}
	dataDir := filepath.Join(root, "runtime", id)
	m := readBrandManifest(dataDir)
	if m == nil {
		return ActiveBranding{}
	}
	return ActiveBranding{
		Manifest:         m,
		LogoDataURI:      readBrandLogoDataURI(dataDir, false),
		LogoLightDataURI: readBrandLogoDataURI(dataDir, true),
	}
}
