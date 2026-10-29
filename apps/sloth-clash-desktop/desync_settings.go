package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"runtime"
	"strings"

	"SlothClashDesktop/internal/runtimecfg"
)

const desyncDefaultUDPRepeats = 1

type DesyncSettings struct {
	Enabled           bool `json:"enabled"`
	YouTube           bool `json:"youtube"`
	Discord           bool `json:"discord"`
	DiscordTTL        int  `json:"discordTTL"`
	DiscordUDP        bool `json:"discordUDP"`
	DiscordUDPTTL     int  `json:"discordUDPTTL"`
	DiscordUDPRepeats int  `json:"discordUDPRepeats"`
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

// effectiveDesyncSettings sanitizes the custom list and fills the defaults a
// stored config leaves at zero.
func effectiveDesyncSettings(s DesyncSettings) DesyncSettings {
	s.Custom = sanitizeDesyncCustom(s.Custom)
	if s.DiscordTTL == 0 {
		s.DiscordTTL = 4
	}
	if s.DiscordUDPTTL == 0 {
		s.DiscordUDPTTL = 4
	}
	if s.DiscordUDPRepeats == 0 {
		s.DiscordUDPRepeats = desyncDefaultUDPRepeats
	}
	return s
}

func desyncCoreSupported() bool {
	for _, feature := range embeddedCoreFeatures() {
		if feature == "desync" {
			return true
		}
	}
	return false
}

func desyncUDPCoreSupported() bool {
	for _, feature := range embeddedCoreFeatures() {
		if feature == "desync-udp" {
			return true
		}
	}
	return false
}

func validateDesyncSettings(s DesyncSettings, platform string, coreSupported bool) (DesyncSettings, error) {
	if s.DiscordTTL == 0 {
		s.DiscordTTL = 4
	}
	if s.DiscordTTL < 1 || s.DiscordTTL > 255 {
		if s.Enabled {
			return s, fmt.Errorf("Discord TTL must be between 1 and 255")
		}
		s.DiscordTTL = 4
	}
	if s.DiscordUDPTTL == 0 {
		s.DiscordUDPTTL = 4
	}
	if s.DiscordUDPRepeats == 0 {
		s.DiscordUDPRepeats = desyncDefaultUDPRepeats
	}
	if s.DiscordUDPTTL < 1 || s.DiscordUDPTTL > 255 {
		if s.Enabled && s.DiscordUDP {
			return s, fmt.Errorf("Discord UDP TTL must be between 1 and 255")
		}
		s.DiscordUDPTTL = 4
	}
	if s.DiscordUDPRepeats < 1 || s.DiscordUDPRepeats > 16 {
		if s.Enabled && s.DiscordUDP {
			return s, fmt.Errorf("Discord UDP repeats must be between 1 and 16")
		}
		s.DiscordUDPRepeats = desyncDefaultUDPRepeats
	}
	if s.Enabled {
		if !coreSupported {
			return s, fmt.Errorf("the embedded core does not support desync")
		}
		if !desyncHasSelection(s) {
			return s, fmt.Errorf("select YouTube, Discord or a custom domain before enabling desync")
		}
		if (s.Discord || s.DiscordUDP) && platform != "windows" {
			return s, fmt.Errorf("the Discord fake method currently requires Windows")
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
func (a *App) SetDesyncSettings(next DesyncSettings) (DesyncStatus, error) {
	a.desyncCtl.invalidate()
	next, err := a.updateActiveDesync(func(current DesyncSettings, desyncOnly bool) (DesyncSettings, error) {
		// A settings object the UI read before an add/remove must not roll
		// the list back.
		next.Custom = current.Custom
		if desyncOnly {
			next.Enabled = true
		}
		next, err := validateDesyncSettings(next, runtime.GOOS, desyncCoreSupported())
		if err != nil {
			return current, err
		}
		if next.Enabled && next.DiscordUDP && !desyncUDPCoreSupported() {
			return current, fmt.Errorf("the embedded core does not support desync UDP")
		}
		return next, nil
	})
	if err != nil {
		return a.GetDesyncStatus(), err
	}
	a.appendRuntimeDiag("desync.settings", fmt.Sprintf("enabled=%t youtube=%t discord=%t ttl=%d discord_udp=%t udp_ttl=%d udp_repeats=%d", next.Enabled, next.YouTube, next.Discord, next.DiscordTTL, next.DiscordUDP, next.DiscordUDPTTL, next.DiscordUDPRepeats))
	a.triggerRuntimeReloadForPrefs()
	return a.GetDesyncStatus(), nil
}

// errNoDesyncProfile: Desync settings belong to a profile, and none is
// active.
var errNoDesyncProfile = errors.New("no active profile for the Desync settings")

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

func probeDesyncOutbound(ctx context.Context, endpoint, secret, route, target string) (int, error) {
	return probeDesyncOutboundMode(ctx, endpoint, secret, route, target, desyncProbeStrict)
}

func probeDesyncOutboundMode(ctx context.Context, endpoint, secret, route, target string, mode desyncProbeMode) (int, error) {
	q := url.Values{"url": {target}, "timeout": {"10000"}, "expected": {mode.expected()}}
	path := "/proxies/" + url.PathEscape(route) + "/delay?" + q.Encode()
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
