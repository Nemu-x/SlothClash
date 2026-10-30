package main

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"runtime"
	"strconv"
	"strings"

	"SlothClashDesktop/internal/runtimecfg"
)

type DesyncSettings struct {
	Enabled    bool `json:"enabled"`
	YouTube    bool `json:"youtube"`
	Discord    bool `json:"discord"`
	DiscordUDP bool `json:"discordUDP"`
	// Custom is the user's custom domains. Absent in prefs written before it
	// existed (= none). Only the AddDesyncCustomDomain family changes it;
	// SetDesyncSettings keeps the stored list.
	Custom []DesyncCustomEntry `json:"custom,omitempty"`
}

type DesyncStatus struct {
	Settings         DesyncSettings `json:"settings"`
	Platform         string         `json:"platform"`
	CoreSupported    bool           `json:"coreSupported"`
	UDPCoreSupported bool           `json:"udpCoreSupported"`
	Connected        bool           `json:"connected"`
	Mode             string         `json:"mode"`
	Traffic          string         `json:"traffic"`
	// DesyncOnly reports that the active profile is a Desync-only one: no
	// subscription reserve exists, so a route that finds no strategy goes
	// direct (no_strategy) instead of to the reserve.
	DesyncOnly bool `json:"desyncOnly"`
}

// currentDesyncSettings is the active profile's effective settings
// (desync_profile.go).
func currentDesyncSettings() DesyncSettings {
	return effectiveDesyncSettings(currentDesktopPrefs().Desync)
}

// effectiveDesyncSettings sanitizes the stored custom list.
func effectiveDesyncSettings(s DesyncSettings) DesyncSettings {
	s.Custom = sanitizeDesyncCustom(s.Custom)
	return s
}

// desyncCoreSupported reports a core with the "type: desync" outbound
// (patch 0023), which every generated strategy is.
func desyncCoreSupported() bool {
	return embeddedCoreHas("desync-outbound")
}

// desyncTransferCoreSupported reports the transfer test (patch 0025): probes
// then read the response body over HTTP/2 instead of stopping at the headers.
func desyncTransferCoreSupported() bool {
	return embeddedCoreHas("transfer-test")
}

// desyncUDPCoreSupported also needs the UDP burst and voice probes (0019).
func desyncUDPCoreSupported() bool {
	return desyncCoreSupported() && embeddedCoreHas("desync-udp")
}

func embeddedCoreHas(token string) bool {
	for _, feature := range embeddedCoreFeatures() {
		if feature == token {
			return true
		}
	}
	return false
}

func validateDesyncSettings(s DesyncSettings, platform string, coreSupported bool) (DesyncSettings, error) {
	if s.Enabled {
		if !coreSupported {
			return s, desyncErr("unsupported_core")
		}
		if !desyncHasSelection(s) {
			return s, desyncErr("no_selection")
		}
		if (s.Discord || s.DiscordUDP) && platform != "windows" {
			return s, desyncErr("discord_platform")
		}
	}
	return s, nil
}

func (a *App) GetDesyncStatus() DesyncStatus {
	a.mu.RLock()
	connected := a.state.Connection.Status == "connected" && a.state.Core.Running
	mode := a.state.Mode.Current
	traffic := a.state.Traffic
	desyncOnly := a.activeProfileIsDesyncOnlyLocked()
	a.mu.RUnlock()
	return DesyncStatus{Settings: currentDesyncSettings(), Platform: runtime.GOOS, CoreSupported: desyncCoreSupported(), UDPCoreSupported: desyncUDPCoreSupported(), Connected: connected, Mode: mode, Traffic: traffic, DesyncOnly: desyncOnly}
}

// SetDesyncSettings applies the switches of next (on/off, YouTube, Discord)
// to the active profile. Its custom list is kept: it has its own methods.
//
// Turning off the last selected service while Desync is on turns Desync off,
// as removing or switching off the last custom entry does
// (updateDesyncCustom): an enabled Desync with nothing selected is not a
// valid state. Switching Desync on with nothing selected is refused
// (no_selection). A Desync-only profile stays on and may select nothing.
func (a *App) SetDesyncSettings(next DesyncSettings) (DesyncStatus, error) {
	a.desyncCtl.invalidate()
	next, err := a.updateActiveDesync(func(current DesyncSettings, desyncOnly bool) (DesyncSettings, error) {
		// A settings object the UI read before an add/remove must not roll
		// the list back.
		next.Custom = current.Custom
		check := next
		switch {
		case desyncOnly:
			next.Enabled = true
			check.Enabled = desyncHasSelection(next)
		case next.Enabled && current.Enabled && !desyncHasSelection(next):
			next.Enabled = false
			check.Enabled = false
		}
		if _, err := validateDesyncSettings(check, runtime.GOOS, desyncCoreSupported()); err != nil {
			return current, err
		}
		if check.Enabled && next.DiscordUDP && !desyncUDPCoreSupported() {
			return current, desyncErr("unsupported_udp_core")
		}
		return next, nil
	})
	if err != nil {
		return a.GetDesyncStatus(), err
	}
	a.appendRuntimeDiag("desync.settings", fmt.Sprintf("enabled=%t youtube=%t discord=%t discord_udp=%t", next.Enabled, next.YouTube, next.Discord, next.DiscordUDP))
	a.triggerRuntimeReloadForPrefs()
	return a.GetDesyncStatus(), nil
}

// errNoDesyncProfile: Desync settings belong to a profile, and none is
// active.
var errNoDesyncProfile = desyncErr("no_profile")

// updateActiveDesync applies change to the active profile's effective Desync
// settings (a copy; desyncOnly tells whether it is a Desync-only profile)
// and stores the result in the profile, profiles.json and the live copy.
// A failed save changes nothing.
func (a *App) updateActiveDesync(change func(current DesyncSettings, desyncOnly bool) (DesyncSettings, error)) (DesyncSettings, error) {
	a.mu.Lock()
	defer a.mu.Unlock()
	idx := a.activeProfileIndexLocked()
	if idx < 0 {
		return DesyncSettings{}, errNoDesyncProfile
	}
	p := a.profiles[idx]
	current := profileDesyncSettings(p)
	current.Custom = append([]DesyncCustomEntry(nil), current.Custom...)
	next, err := change(current, profileIsDesyncOnly(p))
	if err != nil {
		return current, err
	}
	stored := next
	a.profiles[idx].Desync = &stored
	if err := a.persistProfilesLocked(); err != nil {
		a.profiles[idx].Desync = p.Desync
		return current, err
	}
	a.syncActiveDesyncLocked()
	return next, nil
}

// desyncProbeMode is what a probe through a Desync outbound has to prove.
type desyncProbeMode int

const (
	// desyncProbeStrict: a successful HTTP status from a fixed service URL
	// (YouTube, Discord). An error page does not count.
	desyncProbeStrict desyncProbeMode = iota
	// desyncProbeReachable (custom domains): any HTTP response over TLS that
	// verified for the URL's host. The user's site has no known-good URL, so
	// the status says nothing about DPI; the verified certificate proves the
	// connection reached the real server rather than a block page.
	desyncProbeReachable
)

// expected is the core delay API's status filter for the mode. The core
// (adapter.URLTest) always verifies TLS against the system roots for the
// URL's host, does not follow redirects, and with patch 0020 fails a response
// outside this range.
func (m desyncProbeMode) expected() string {
	if m == desyncProbeReachable {
		return "100-599"
	}
	return "200-299"
}

// desyncCustomTarget is the probe URL of a custom domain.
func desyncCustomTarget(domain string) string { return "https://" + domain + "/" }

// desyncProbeMaxBytes is how much of a response body a transfer probe reads.
// Networks that freeze a connection let its first tens of kilobytes through
// (about 16 KB is the reported threshold), so a probe reading past that sees
// the freeze where the headers alone would pass.
const desyncProbeMaxBytes = 64 << 10

// desyncVolumeMinBytes is how much a volume target must deliver: twice the
// reported freeze threshold, small enough for a 5 s probe on a slow link.
const desyncVolumeMinBytes = 32 << 10

// desyncVolumeTargets are the service probe URLs whose bodies are known to
// exceed desyncVolumeMinBytes (discord.com/login about 72 KB, the YouTube home
// page about 1 MB, both measured 2026-10-30), so a short body there is a
// failure rather than a small page.
var desyncVolumeTargets = map[string]bool{
	"https://discord.com/login": true,
	"https://www.youtube.com/":  true,
}

func probeDesyncOutbound(ctx context.Context, endpoint, secret, route, target string) (int, error) {
	return probeDesyncOutboundMode(ctx, endpoint, secret, route, target, desyncProbeStrict)
}

// probeDesyncOutboundMode probes route against target through the core. With
// the transfer test (0025) the core sends GET with ALPN h2 and http/1.1 and
// reads up to desyncProbeMaxBytes of the body, failing on a stall; a volume
// target must also deliver desyncVolumeMinBytes. Without it, the delay test
// checks the response headers only.
func probeDesyncOutboundMode(ctx context.Context, endpoint, secret, route, target string, mode desyncProbeMode) (int, error) {
	q := url.Values{"url": {target}, "timeout": {"10000"}, "expected": {mode.expected()}}
	path := "/proxies/" + url.PathEscape(route) + "/delay?" + q.Encode()
	if desyncTransferCoreSupported() {
		q.Set("max-bytes", strconv.Itoa(desyncProbeMaxBytes))
		q.Set("stall", "2000") // a frozen connection never resumes; fail before the caller's 5-6 s
		if desyncVolumeTargets[target] {
			q.Set("min-bytes", strconv.Itoa(desyncVolumeMinBytes))
		}
		path = "/proxies/" + url.PathEscape(route) + "/transfer?" + q.Encode()
	}
	resp, err := coreDoWithEndpoint(ctx, endpoint, secret, http.MethodGet, path, nil)
	if err != nil {
		return 0, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		body, readErr := io.ReadAll(io.LimitReader(resp.Body, 4096))
		if readErr != nil {
			return 0, fmt.Errorf("core probe: HTTP %d: %w", resp.StatusCode, readErr)
		}
		var failure struct {
			Message string `json:"message"`
		}
		_ = json.Unmarshal(body, &failure)
		message := strings.TrimSpace(failure.Message)
		if message == "" && !json.Valid(body) {
			message = strings.TrimSpace(string(body))
		}
		if message != "" {
			return 0, fmt.Errorf("core probe: HTTP %d: %s", resp.StatusCode, truncateString(runtimecfg.RedactCredentialQuery(message), 1024))
		}
		return 0, fmt.Errorf("core probe: HTTP %d", resp.StatusCode)
	}
	var body struct {
		Delay int `json:"delay"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
		return 0, err
	}
	return body.Delay, nil
}
