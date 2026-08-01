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

// ConnectionSettings holds knobs that change how the local proxy is EXPOSED,
// as opposed to how traffic is processed. Kept separate from TrafficSettings
// because the security posture differs: these decide who can reach us.
type ConnectionSettings struct {
	// AllowLan opens the mixed-port proxy to the local network. nil/false =
	// localhost only, which is the safe default we ship: an open proxy on the
	// LAN lets any device on the network egress through the user's tunnel.
	// Turning it on is an explicit, informed user choice.
	AllowLan *bool `json:"allowLan,omitempty"`
	// DNSIPv6 controls `dns.ipv6`: whether the resolver answers AAAA queries.
	// nil = off. Off by default because a broken/half-working IPv6 path is a
	// classic source of "some sites hang" reports; users on real IPv6 networks
	// turn it on. Pairs with dns.fake-ip-range6 in the generated config.
	DNSIPv6 *bool `json:"dnsIpv6,omitempty"`
	// SmartDNS controls `dns.respect-rules`: resolve proxied domains through the
	// proxy (no DNS leak / ISP poisoning for them) while direct domains stay
	// local. nil = off, matching verge's default; enabling it also requires
	// proxy-server-nameserver, which the overlay fills in automatically.
	SmartDNS *bool `json:"smartDns,omitempty"`
	// MixedPort pins the local mixed-port to a fixed value ("lock the port").
	// nil/0/out-of-range = auto: we pick a random free port on every core start
	// (the default), which avoids collisions with stale binds but changes across
	// reconnects / subscription switches. Users who point external tools at a
	// specific 127.0.0.1:<port> lock it here so it stays constant. Overrides the
	// port from the subscription config.
	MixedPort *int `json:"mixedPort,omitempty"`
}

// IsAllowLanEnabled reports the effective value (default: false).
func (c ConnectionSettings) IsAllowLanEnabled() bool {
	return c.AllowLan != nil && *c.AllowLan
}

// IsDNSIPv6Enabled reports the effective value (default: false).
func (c ConnectionSettings) IsDNSIPv6Enabled() bool {
	return c.DNSIPv6 != nil && *c.DNSIPv6
}

// IsSmartDNSEnabled reports the effective value (default: false).
func (c ConnectionSettings) IsSmartDNSEnabled() bool {
	return c.SmartDNS != nil && *c.SmartDNS
}

// FixedMixedPort returns the user-pinned mixed-port and true when a valid one is
// set (1–65535); otherwise (0, false), meaning "auto — pick a random free port".
func (c ConnectionSettings) FixedMixedPort() (int, bool) {
	if c.MixedPort == nil {
		return 0, false
	}
	p := *c.MixedPort
	if p < 1 || p > 65535 {
		return 0, false
	}
	return p, true
}

// DesktopPrefs holds app-level preferences persisted to prefs.json alongside profiles.json.
type DesktopPrefs struct {
	TUN        TunSettings        `json:"tun"`
	Traffic    TrafficSettings    `json:"traffic"`
	Connection   ConnectionSettings   `json:"connection"`
	Privacy      PrivacySettings      `json:"privacy"`
	AppUpdate    AppUpdateSettings    `json:"appUpdate"`
	Experimental ExperimentalSettings `json:"experimental"`
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

// ExperimentalSettings gates optional, off-by-default features so they don't
// clutter the UI for regular users.
type ExperimentalSettings struct {
	// CorpVpnEnabled reveals the Corporate VPN (OpenConnect sidecar) tab. Off by
	// default: OpenConnect is an optional power-user add-on downloaded on demand,
	// so a regular user should never see it unless they opt in here.
	CorpVpnEnabled *bool `json:"corpVpnEnabled,omitempty"`
}

// IsCorpVpnEnabled reports whether the Corporate VPN tab should be shown. The
// default (nil pointer or absent field) is false — opt-in only.
func (s ExperimentalSettings) IsCorpVpnEnabled() bool {
	return s.CorpVpnEnabled != nil && *s.CorpVpnEnabled
}

const slothPrefsFile = "prefs.json"

var (
	prefsMu      sync.RWMutex
	prefsCurrent DesktopPrefs
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
		return
	}
	var disk DesktopPrefs
	if err := json.Unmarshal(b, &disk); err != nil {
		return
	}
	prefsMu.Lock()
	prefsCurrent = disk
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

// savePrefsBestEffort persists desktop prefs and logs on failure instead of
// silently dropping the error (audit A4-1) — a full disk / ACL issue is at
// least visible in the debug log.
func savePrefsBestEffort(snapshot DesktopPrefs) {
	if err := saveDesktopPrefsLocked(snapshot); err != nil {
		debugLog("prefs", "A4", "tun_settings.go", "failed to persist desktop prefs",
			map[string]any{"error": err.Error()})
	}
}

func (a *App) GetDesktopPrefs() DesktopPrefs {
	_ = a
	return currentDesktopPrefs()
}

// SetExperimentalSettings is the Wails-exposed setter for the Experimental
// section of the Settings UI (e.g. revealing the Corporate VPN tab). Persisted
// to prefs.json; no core reload needed — it only toggles UI surface.
func (a *App) SetExperimentalSettings(next ExperimentalSettings) DesktopPrefs {
	_ = a
	prefsMu.Lock()
	prefsCurrent.Experimental = next
	snapshot := prefsCurrent
	savePrefsBestEffort(snapshot)
	prefsMu.Unlock()
	return snapshot
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
	savePrefsBestEffort(snapshot)
	prefsMu.Unlock()

	a.triggerRuntimeReloadForPrefs()
	return snapshot
}

// SetConnectionSettings is the Wails-exposed setter for the Connection section
// of the Settings UI. Same flow as SetTunSettings: persist, then reload the
// running core so `allow-lan` takes effect without a restart.
func (a *App) SetConnectionSettings(next ConnectionSettings) DesktopPrefs {
	prefsMu.Lock()
	prevPort, _ := prefsCurrent.Connection.FixedMixedPort()
	prefsCurrent.Connection = next
	snapshot := prefsCurrent
	savePrefsBestEffort(snapshot)
	prefsMu.Unlock()

	// allow-lan / DNS toggles apply via a hot-reload, but the mixed-port only
	// rebinds on a fresh listen — a hot-reload reuses the running port. So when
	// the port pin changed, do a full core restart; otherwise the usual reload.
	newPort, _ := next.FixedMixedPort()
	if newPort != prevPort {
		a.restartActiveProfileCore()
	} else {
		a.triggerRuntimeReloadForPrefs()
	}
	return snapshot
}

// restartActiveProfileCore does a full core restart for the active profile when
// connected. Used for settings that only take effect on a fresh listen (the
// fixed mixed-port), which a hot-reload cannot change. No-op when disconnected —
// the new value is picked up on the next connect.
func (a *App) restartActiveProfileCore() {
	a.mu.RLock()
	activeID := strings.TrimSpace(a.state.Profile.ActiveProfileID)
	connected := a.state.Connection.Status == "connected"
	traffic := strings.TrimSpace(a.state.Traffic)
	var active Profile
	for _, p := range a.profiles {
		if p.ID == activeID {
			active = p
			break
		}
	}
	a.mu.RUnlock()
	if !connected || active.ID == "" {
		return
	}
	go func() {
		if err := a.forceRestartCoreForProfile(active, 0, traffic == "tun"); err != nil {
			a.appendRuntimeDiag("mixed-port", "core restart after port change failed: "+err.Error())
		}
	}()
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
	savePrefsBestEffort(snapshot)
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
	savePrefsBestEffort(snapshot)
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
	savePrefsBestEffort(snapshot)
	prefsMu.Unlock()
	return snapshot
}

// SetTrafficSettings is the Wails-exposed setter for sniffer / find-process-mode.
func (a *App) SetTrafficSettings(next TrafficSettings) DesktopPrefs {
	next.FindProcessMode = normalizeFindProcessMode(next.FindProcessMode)

	prefsMu.Lock()
	prefsCurrent.Traffic = next
	snapshot := prefsCurrent
	savePrefsBestEffort(snapshot)
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
