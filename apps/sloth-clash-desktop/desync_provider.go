package main

import (
	"net/http"
	"net/url"
	"path/filepath"
	"runtime"
	"strings"
	"sync/atomic"
	"time"
)

// ---------------------------------------------------------------------------
// Provider control of Desync on a subscription profile.
//
// Mixing Desync into a subscription changes how its services leave the
// machine and can break the provider's setup, so only the user enables it:
// the "Connect Desync" choice when adding the subscription, or Settings.
// The provider can only move in the safe direction, with one response header
// of an HTTPS subscription:
//
//	X-Desync-Desktop-Mode: off   switch Desync off once (when the value changes)
//	X-Desync-Desktop-Mode: deny  Desync off, and enabling it is refused
//
// Any other value, "on" included, is ignored. The profile stores the last
// mode it applied, so a refresh with the same value changes nothing (the
// user may switch Desync on again after an "off"); a response without the
// header clears the stored mode, which lifts a deny. Like branding, the header
// is desktop-only and honored only when the subscription URL is HTTPS; it is
// separate from X-Brand-Desktop-*, which never affects connections.
// ---------------------------------------------------------------------------

const (
	desyncProviderHeader = "X-Desync-Desktop-Mode"
	desyncProviderOff    = "off"
	desyncProviderDeny   = "deny"
)

// errDesyncProviderDenied: the active profile's provider forbids Desync.
var errDesyncProviderDenied = desyncErr("provider_denied")

// parseDesyncProviderMode reads the provider mode of a subscription response:
// "off", "deny", or "" (absent, unknown, or not an HTTPS subscription).
func parseDesyncProviderMode(subURL string, get func(string) string) string {
	u, err := url.Parse(strings.TrimSpace(subURL))
	if err != nil || !strings.EqualFold(u.Scheme, "https") || get == nil {
		return ""
	}
	switch v := strings.ToLower(strings.TrimSpace(get(desyncProviderHeader))); v {
	case desyncProviderOff, desyncProviderDeny:
		return v
	}
	return ""
}

// applyDesyncProviderMode is the transition from the stored mode to next. It
// reports whether the stored mode changes; a change to off or deny switches
// Desync off and keeps the selection. nil settings (never changed, the
// defaults, which are off) stay nil.
func applyDesyncProviderMode(settings *DesyncSettings, stored, next string) (*DesyncSettings, bool) {
	if stored == next {
		return settings, false
	}
	if (next == desyncProviderOff || next == desyncProviderDeny) && settings != nil && settings.Enabled {
		off := *settings
		off.Enabled = false
		return &off, true
	}
	return settings, true
}

// profileTakesDesyncProvider: only subscription profiles have a provider.
func profileTakesDesyncProvider(p Profile) bool {
	return strings.TrimSpace(p.URL) != "" && !profileIsDesyncOnly(p)
}

// desyncProviderRecorder is the App that receives the provider mode of every
// successful subscription fetch. Set in startup; nil (tests, before startup)
// drops them.
var desyncProviderRecorder atomic.Pointer[App]

// captureSubscriptionHeaders is the OnHeaders hook of every subscription
// client main builds: branding, then the Desync provider mode.
func captureSubscriptionHeaders(dataDir, subURL string, hdr http.Header) {
	persistBrandManifestFromHeaders(dataDir, subURL, hdr)
	if a := desyncProviderRecorder.Load(); a != nil && hdr != nil {
		a.recordDesyncProviderMode(dataDir, parseDesyncProviderMode(subURL, hdr.Get))
	}
}

// recordDesyncProviderMode applies a refresh's provider mode to the profile
// whose runtime directory is dataDir. When it switches Desync off on the
// active profile, the running configuration is regenerated.
func (a *App) recordDesyncProviderMode(dataDir, mode string) {
	root, err := slothDataRoot()
	if err != nil || strings.TrimSpace(dataDir) == "" {
		return
	}
	dataDir = filepath.Clean(dataDir)
	a.mu.Lock()
	idx := -1
	for i, p := range a.profiles {
		if filepath.Join(root, "runtime", p.ID) == dataDir {
			idx = i
			break
		}
	}
	if idx < 0 || !profileTakesDesyncProvider(a.profiles[idx]) {
		a.mu.Unlock()
		return
	}
	prev := a.profiles[idx]
	settings, changed := applyDesyncProviderMode(prev.Desync, prev.DesyncProviderMode, mode)
	if !changed {
		a.mu.Unlock()
		return
	}
	switchedOff := settings != prev.Desync
	a.profiles[idx].Desync = settings
	a.profiles[idx].DesyncProviderMode = mode
	a.state.Profile.Profiles = a.profiles
	if err := a.persistProfilesLocked(); err != nil {
		a.profiles[idx] = prev
		a.state.Profile.Profiles = a.profiles
		a.mu.Unlock()
		debugLog("desync", "DP-1", "desync_provider.go:recordDesyncProviderMode",
			"failed to persist the provider mode", map[string]any{"error": err.Error()})
		return
	}
	active := idx == a.activeProfileIndexLocked()
	if active {
		a.syncActiveDesyncLocked()
	}
	a.state.UpdatedAt = time.Now().Unix()
	a.mu.Unlock()
	a.appendRuntimeDiag("desync.provider", "mode="+desyncProviderModeLabel(mode))
	if active && switchedOff {
		a.desyncCtl.invalidate()
		a.triggerRuntimeReloadForPrefs()
	}
	a.emitAppStateChanged()
}

func desyncProviderModeLabel(mode string) string {
	if mode == "" {
		return "none"
	}
	return mode
}

// activeDesyncProviderModeLocked is the active profile's stored provider mode.
func (a *App) activeDesyncProviderModeLocked() string {
	if idx := a.activeProfileIndexLocked(); idx >= 0 {
		return a.profiles[idx].DesyncProviderMode
	}
	return ""
}

// desyncImportSelection is the Desync a subscription added with "Connect
// Desync" starts with: the user's Desync-only selection when they have one
// with something selected (they already chose what Desync covers), otherwise
// the defaults; switched on. Callers hold a.mu.
func (a *App) desyncImportSelectionLocked() DesyncSettings {
	for _, p := range a.profiles {
		if !profileIsDesyncOnly(p) {
			continue
		}
		s := profileDesyncSettings(p)
		if desyncHasSelection(s) {
			s.Custom = append([]DesyncCustomEntry(nil), s.Custom...)
			s.Enabled = true
			return s
		}
	}
	s := defaultDesyncSettings()
	s.Enabled = true
	return s
}

// desyncImportRunnable checks a "Connect Desync" selection the way
// SetDesyncSettings checks a switch-on; false leaves the new profile's Desync
// off.
func desyncImportRunnable(s DesyncSettings) (DesyncSettings, bool) {
	if !desyncHasSelection(s) {
		return s, false
	}
	if _, err := validateDesyncSettings(s, runtime.GOOS, desyncCoreSupported()); err != nil {
		return s, false
	}
	if s.DiscordUDP && !desyncUDPCoreSupported() {
		return s, false
	}
	return s, true
}
