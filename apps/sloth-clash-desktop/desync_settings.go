package main

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"runtime"
	"strings"
	"time"

	"SlothClashDesktop/internal/desync"
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
}

type DesyncStatus struct {
	Settings         DesyncSettings `json:"settings"`
	Platform         string         `json:"platform"`
	CoreSupported    bool           `json:"coreSupported"`
	UDPCoreSupported bool           `json:"udpCoreSupported"`
	Connected        bool           `json:"connected"`
	Mode             string         `json:"mode"`
	Traffic          string         `json:"traffic"`
}

type DesyncProbeResult struct {
	Name    string `json:"name"`
	URL     string `json:"url"`
	Route   string `json:"route"`
	OK      bool   `json:"ok"`
	DelayMS int    `json:"delayMS"`
	Error   string `json:"error,omitempty"`
}

func currentDesyncSettings() DesyncSettings {
	return effectiveDesyncSettings(currentDesktopPrefs().Desync)
}

// effectiveDesyncSettings fills the defaults a stored (possibly never-touched)
// desync config leaves at zero.
func effectiveDesyncSettings(s DesyncSettings) DesyncSettings {
	if s == (DesyncSettings{}) {
		return DesyncSettings{YouTube: true, Discord: runtime.GOOS == "windows", DiscordTTL: 4, DiscordUDPTTL: 4, DiscordUDPRepeats: desyncDefaultUDPRepeats}
	}
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
		if !s.YouTube && !s.Discord && !s.DiscordUDP {
			return s, fmt.Errorf("select YouTube or Discord before enabling desync")
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
	a.mu.RUnlock()
	return DesyncStatus{Settings: currentDesyncSettings(), Platform: runtime.GOOS, CoreSupported: desyncCoreSupported(), UDPCoreSupported: desyncUDPCoreSupported(), Connected: connected, Mode: mode, Traffic: traffic}
}

func (a *App) SetDesyncSettings(next DesyncSettings) (DesyncStatus, error) {
	a.desyncCtl.invalidate()
	next, err := validateDesyncSettings(next, runtime.GOOS, desyncCoreSupported())
	if err != nil {
		return a.GetDesyncStatus(), err
	}
	if next.Enabled && next.DiscordUDP && !desyncUDPCoreSupported() {
		return a.GetDesyncStatus(), fmt.Errorf("the embedded core does not support desync UDP")
	}
	prefsMu.Lock()
	prefsCurrent.Desync = next
	snapshot := prefsCurrent
	savePrefsBestEffort(snapshot)
	prefsMu.Unlock()
	a.appendRuntimeDiag("desync.settings", fmt.Sprintf("enabled=%t youtube=%t discord=%t ttl=%d discord_udp=%t udp_ttl=%d udp_repeats=%d", next.Enabled, next.YouTube, next.Discord, next.DiscordTTL, next.DiscordUDP, next.DiscordUDPTTL, next.DiscordUDPRepeats))
	a.triggerRuntimeReloadForPrefs()
	return a.GetDesyncStatus(), nil
}

func probeDesyncOutbound(ctx context.Context, endpoint, secret, route, target string) (int, error) {
	q := url.Values{"url": {target}, "timeout": {"10000"}, "expected": {"200-299"}}
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

// Fixed URLs are sent through the named core outbound. This checks that the
// running patched core can use the method; it is not a browser/login/voice test.
func (a *App) RunDesyncDiagnostics() ([]DesyncProbeResult, error) {
	status := a.GetDesyncStatus()
	if !status.Connected {
		return nil, fmt.Errorf("connect SlothClash before running diagnostics")
	}
	if !status.Settings.Enabled {
		return nil, fmt.Errorf("enable desync before running diagnostics")
	}
	if !strings.EqualFold(status.Mode, "rule") {
		return nil, fmt.Errorf("select Rule mode so service routes are active")
	}
	a.mu.RLock()
	endpoint := a.effectiveCoreEndpointLocked()
	secret := a.core.secret
	a.mu.RUnlock()
	var checks []DesyncProbeResult
	if status.Settings.YouTube {
		checks = append(checks, DesyncProbeResult{Name: "YouTube HTTPS", URL: "https://www.youtube.com/generate_204", Route: desync.YouTubeRoute})
	}
	if status.Settings.Discord {
		checks = append(checks, DesyncProbeResult{Name: "Discord API", URL: "https://discord.com/api/v10/gateway", Route: desync.DiscordRoute}, DesyncProbeResult{Name: "Discord CDN", URL: "https://cdn.discordapp.com/embed/avatars/0.png", Route: desync.DiscordRoute})
	}
	parent := a.ctx
	if parent == nil {
		parent = context.Background()
	}
	ctx, cancel := context.WithTimeout(parent, 35*time.Second)
	defer cancel()
	for i := range checks {
		probeCtx, probeCancel := context.WithTimeout(ctx, 11*time.Second)
		var group desyncUDPProxy
		err := autoCoreJSON(probeCtx, endpoint, secret, http.MethodGet, "/proxies/"+url.PathEscape(checks[i].Route), nil, &group)
		delay := 0
		if err == nil && group.Now == desyncProfileSelection {
			var result desyncProfileProbe
			result, err = probeDesyncProfile(probeCtx, endpoint, secret, checks[i].Route, "")
			delay = int(result.DurationMS)
			checks[i].Route = "profile"
		} else if err == nil {
			delay, err = probeDesyncOutbound(probeCtx, endpoint, secret, checks[i].Route, checks[i].URL)
		}
		probeCancel()
		checks[i].DelayMS = delay
		checks[i].OK = err == nil
		if err != nil {
			checks[i].Error = err.Error()
		}
		a.appendRuntimeDiag("desync.probe", fmt.Sprintf("route=%s target=%s ok=%t delay_ms=%d error=%q", checks[i].Route, checks[i].Name, checks[i].OK, delay, checks[i].Error))
	}
	return checks, nil
}
