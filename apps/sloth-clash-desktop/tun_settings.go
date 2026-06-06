package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"sync"
)

// TunSettings mirrors clash-verge-rev's user-facing TUN fields (src/components/setting/mods/tun-viewer.tsx)
// and is overlaid on top of the subscription's `tun:` block during runtime
// config generation. A zero/empty value means "inherit the subscription /
// template default"; only explicitly set fields override.
type TunSettings struct {
	Stack               string   `json:"stack,omitempty"`               // "" = inherit; gvisor|system|mixed
	AutoRoute           *bool    `json:"autoRoute,omitempty"`           // nil = inherit
	AutoDetectInterface *bool    `json:"autoDetectInterface,omitempty"` // nil = inherit
	StrictRoute         *bool    `json:"strictRoute,omitempty"`         // nil = inherit
	DNSHijack           []string `json:"dnsHijack,omitempty"`           // nil / empty = inherit
	MTU                 int      `json:"mtu,omitempty"`                 // 0 = inherit
	Device              string   `json:"device,omitempty"`              // "" = inherit
}

// TrafficSettings groups packet-pipeline knobs that are not strictly part of
// the tun: block but heavily impact throughput and UDP packet loss (sniffer,
// find-process-mode). clash-verge-rev does not force these either; we keep
// them user-controllable and default to "inherit Mihomo defaults".
type TrafficSettings struct {
	SnifferEnabled  *bool  `json:"snifferEnabled,omitempty"`  // nil = inherit
	FindProcessMode string `json:"findProcessMode,omitempty"` // "" = inherit; off|strict|always
}

// DesktopPrefs holds app-level preferences persisted to prefs.json alongside profiles.json.
type DesktopPrefs struct {
	TUN       TunSettings       `json:"tun"`
	Traffic   TrafficSettings   `json:"traffic"`
	Privacy   PrivacySettings   `json:"privacy"`
	AppUpdate AppUpdateSettings `json:"appUpdate"`
	// Lang is the current UI language ("en"/"ru"/"zh"/""). Frontend pushes
	// this on i18n init / change so the native tray menu can localize its
	// labels without a separate IPC roundtrip on each redraw.
	Lang string `json:"lang,omitempty"`
}

// PrivacySettings holds opt-out toggles for client metadata sent to subscription
// providers. The HWID header (x-hwid) is on by default because most providers
// rate-limit / classify by it; the toggle is for users who specifically want to
// strip it (private trials, paranoid threat models, etc).
type PrivacySettings struct {
	// HwidEnabled controls the x-hwid HTTP header on subscription import /
	// refresh. nil OR true → header is sent (default). false → header is
	// omitted. Other identity headers (x-device-os, x-ver-os, x-device-model,
	// x-app-version) are unaffected and still sent — they are not
	// device-unique.
	HwidEnabled *bool `json:"hwidEnabled,omitempty"`
}

// IsHwidEnabled returns true when the x-hwid header should be sent. The
// default (nil pointer or absent field on disk) is true: HWID enabled.
func (p PrivacySettings) IsHwidEnabled() bool {
	if p.HwidEnabled == nil {
		return true
	}
	return *p.HwidEnabled
}

// AppUpdateSettings controls the app's own auto-update checker (distinct from
// per-profile subscription auto-update).
type AppUpdateSettings struct {
	// AutoCheckEnabled gates the background update check. nil OR true → enabled
	// (default); false → no background check (the manual "Check for updates"
	// button still works).
	AutoCheckEnabled *bool `json:"autoCheckEnabled,omitempty"`
}

// IsAutoCheckEnabled returns true when the background update check should run.
// The default (nil pointer or absent field on disk) is true.
func (s AppUpdateSettings) IsAutoCheckEnabled() bool {
	if s.AutoCheckEnabled == nil {
		return true
	}
	return *s.AutoCheckEnabled
}

const slothPrefsFile = "prefs.json"

var (
	prefsMu      sync.RWMutex
	prefsCurrent DesktopPrefs
	prefsLoaded  bool
)

func prefsStorePath() (string, error) {
	root, err := slothDataRoot()
	if err != nil {
		return "", err
	}
	return filepath.Join(root, slothPrefsFile), nil
}

func loadDesktopPrefs() {
	p, err := prefsStorePath()
	if err != nil {
		return
	}
	b, err := os.ReadFile(p)
	if err != nil || len(b) == 0 {
		prefsMu.Lock()
		prefsLoaded = true
		prefsMu.Unlock()
		return
	}
	var disk DesktopPrefs
	if err := json.Unmarshal(b, &disk); err != nil {
		prefsMu.Lock()
		prefsLoaded = true
		prefsMu.Unlock()
		return
	}
	prefsMu.Lock()
	prefsCurrent = disk
	prefsLoaded = true
	prefsMu.Unlock()
}

func saveDesktopPrefsLocked(prefs DesktopPrefs) error {
	p, err := prefsStorePath()
	if err != nil {
		return err
	}
	root, err := slothDataRoot()
	if err != nil {
		return err
	}
	if err := os.MkdirAll(root, 0o755); err != nil {
		return err
	}
	b, err := json.MarshalIndent(prefs, "", "  ")
	if err != nil {
		return err
	}
	return atomicWriteFile(p, b, 0o644)
}

func currentDesktopPrefs() DesktopPrefs {
	prefsMu.RLock()
	defer prefsMu.RUnlock()
	return prefsCurrent
}

// normalizeTunStack accepts gvisor / system / mixed (case-insensitive) and returns
// the canonical lowercase form. Any other value is rejected → "" (inherit).
func normalizeTunStack(v string) string {
	switch strings.ToLower(strings.TrimSpace(v)) {
	case "gvisor":
		return "gvisor"
	case "system":
		return "system"
	case "mixed":
		return "mixed"
	default:
		return ""
	}
}

// normalizeFindProcessMode accepts off / strict / always (case-insensitive) and
// returns the canonical lowercase form; any other value is rejected → "".
func normalizeFindProcessMode(v string) string {
	switch strings.ToLower(strings.TrimSpace(v)) {
	case "off":
		return "off"
	case "strict":
		return "strict"
	case "always":
		return "always"
	default:
		return ""
	}
}

// GetDesktopPrefs is the Wails-exposed getter for the Settings UI.
func (a *App) GetDesktopPrefs() DesktopPrefs {
	_ = a
	return currentDesktopPrefs()
}

// SetTunSettings is the Wails-exposed setter for the TUN section of the
// Settings UI. The update is persisted to prefs.json and the running core (if
// any) is reloaded via the standard applyRuntimeConfig → PUT /configs path so
// the new stack / auto-route / dns-hijack values take effect without a core
// restart, mirroring clash-verge-rev's update_clash_config flow.
func (a *App) SetTunSettings(next TunSettings) DesktopPrefs {
	next.Stack = normalizeTunStack(next.Stack)
	if next.MTU < 0 {
		next.MTU = 0
	}
	next.Device = strings.TrimSpace(next.Device)
	next.DNSHijack = sanitizeDNSHijack(next.DNSHijack)

	prefsMu.Lock()
	prefsCurrent.TUN = next
	snapshot := prefsCurrent
	_ = saveDesktopPrefsLocked(snapshot)
	prefsMu.Unlock()

	a.triggerRuntimeReloadForPrefs()
	return snapshot
}

// SetUiLanguage is called by the frontend at i18n init and whenever the user
// changes the UI language. The pref is persisted so subsequent app launches
// build the tray menu in the right language even before the webview attaches.
func (a *App) SetUiLanguage(lang string) DesktopPrefs {
	_ = a
	switch lang {
	case "en", "ru", "zh":
	default:
		lang = ""
	}
	prefsMu.Lock()
	prefsCurrent.Lang = lang
	snapshot := prefsCurrent
	_ = saveDesktopPrefsLocked(snapshot)
	prefsMu.Unlock()
	return snapshot
}

// SetHwidEnabled toggles whether the x-hwid HTTP header is included in
// subscription import / refresh requests. The value is persisted to
// prefs.json; no runtime reload is needed because the change only affects
// outgoing subscription HTTP, not the running mihomo config.
func (a *App) SetHwidEnabled(enabled bool) DesktopPrefs {
	_ = a
	v := enabled
	prefsMu.Lock()
	prefsCurrent.Privacy.HwidEnabled = &v
	snapshot := prefsCurrent
	_ = saveDesktopPrefsLocked(snapshot)
	prefsMu.Unlock()
	return snapshot
}

// SetAppAutoUpdateEnabled toggles the background app update checker. Persisted
// to prefs.json and honored by updateCheckLoop without a restart (the manual
// "Check for updates" action is unaffected).
func (a *App) SetAppAutoUpdateEnabled(enabled bool) DesktopPrefs {
	_ = a
	v := enabled
	prefsMu.Lock()
	prefsCurrent.AppUpdate.AutoCheckEnabled = &v
	snapshot := prefsCurrent
	_ = saveDesktopPrefsLocked(snapshot)
	prefsMu.Unlock()
	return snapshot
}

// SetTrafficSettings is the Wails-exposed setter for sniffer / find-process-mode.
func (a *App) SetTrafficSettings(next TrafficSettings) DesktopPrefs {
	next.FindProcessMode = normalizeFindProcessMode(next.FindProcessMode)

	prefsMu.Lock()
	prefsCurrent.Traffic = next
	snapshot := prefsCurrent
	_ = saveDesktopPrefsLocked(snapshot)
	prefsMu.Unlock()

	a.triggerRuntimeReloadForPrefs()
	return snapshot
}

func sanitizeDNSHijack(in []string) []string {
	if len(in) == 0 {
		return nil
	}
	seen := make(map[string]bool, len(in))
	out := make([]string, 0, len(in))
	for _, s := range in {
		trimmed := strings.TrimSpace(s)
		if trimmed == "" || seen[trimmed] {
			continue
		}
		seen[trimmed] = true
		out = append(out, trimmed)
	}
	if len(out) == 0 {
		return nil
	}
	return out
}

// applyUserTunOverlay writes the user's TunSettings onto the generated tun:
// block. Empty / nil fields are skipped (no override), mirroring Verge Rev's
// `revise!` approach. Called after ensureTunOverlayForTraffic has set
// tun.enable and the base template.
func applyUserTunOverlay(m map[string]any, tun TunSettings) {
	rawTun, _ := m["tun"].(map[string]any)
	if rawTun == nil {
		rawTun = map[string]any{}
	}
	if tun.Stack != "" {
		rawTun["stack"] = tun.Stack
	}
	if tun.AutoRoute != nil {
		rawTun["auto-route"] = *tun.AutoRoute
	}
	if tun.AutoDetectInterface != nil {
		rawTun["auto-detect-interface"] = *tun.AutoDetectInterface
	}
	if tun.StrictRoute != nil {
		rawTun["strict-route"] = *tun.StrictRoute
	}
	if len(tun.DNSHijack) > 0 {
		arr := make([]any, 0, len(tun.DNSHijack))
		for _, h := range tun.DNSHijack {
			arr = append(arr, h)
		}
		rawTun["dns-hijack"] = arr
	}
	if tun.MTU > 0 {
		rawTun["mtu"] = tun.MTU
	}
	if tun.Device != "" {
		rawTun["device"] = tun.Device
	}
	m["tun"] = rawTun
}

// applyUserTrafficOverlay writes the user's sniffer / find-process-mode
// preferences on top of whatever the subscription ships. A nil pointer or ""
// leaves the subscription value (or Mihomo's internal default) untouched.
func applyUserTrafficOverlay(m map[string]any, traffic TrafficSettings) {
	if traffic.SnifferEnabled != nil {
		rawSniffer, _ := m["sniffer"].(map[string]any)
		if rawSniffer == nil {
			rawSniffer = map[string]any{}
		}
		rawSniffer["enable"] = *traffic.SnifferEnabled
		m["sniffer"] = rawSniffer
	}
	if traffic.FindProcessMode != "" {
		m["find-process-mode"] = traffic.FindProcessMode
	}
}

// triggerRuntimeReloadForPrefs is called from Set* methods to propagate pref
// changes to the running Mihomo core via the standard applyRuntimeConfig path.
// This is a best-effort reload: if no profile is active or the core is not
// running, it is a no-op (the new prefs will apply on the next Connect).
func (a *App) triggerRuntimeReloadForPrefs() {
	go func() {
		a.mu.RLock()
		activeID := strings.TrimSpace(a.state.Profile.ActiveProfileID)
		var active Profile
		for _, p := range a.profiles {
			if p.ID == activeID {
				active = p
				break
			}
		}
		traffic := a.state.Traffic
		connected := a.state.Connection.Status == "connected"
		a.mu.RUnlock()
		if activeID == "" || active.ID == "" {
			return
		}
		if err := a.applyRuntimeConfig(active, traffic, connected && traffic == "tun"); err != nil {
			debugLog("prefs", "H1", "tun_settings.go:triggerRuntimeReloadForPrefs", "apply runtime reload after prefs change failed", map[string]any{
				"error":     err.Error(),
				"profileId": active.ID,
			})
		}
	}()
}
