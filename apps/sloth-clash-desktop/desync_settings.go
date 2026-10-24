package main

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"runtime"
	"strings"
	"time"
)

const desyncYouTubeRoute = "SLOTH-DESYNC-YOUTUBE"
const desyncDiscordRoute = "SLOTH-DESYNC-DISCORD"
const desyncManagedKey = "_sloth-desync-managed"

var desyncYouTubeDomains = []string{"youtube.com", "youtu.be", "googlevideo.com", "ytimg.com", "youtubei.googleapis.com", "youtube.googleapis.com"}
var desyncDiscordDomains = []string{"discord.com", "discord.gg", "discordapp.com", "discordapp.net", "discord.media", "discordcdn.com"}

type DesyncSettings struct {
	Enabled    bool `json:"enabled"`
	YouTube    bool `json:"youtube"`
	Discord    bool `json:"discord"`
	DiscordTTL int  `json:"discordTTL"`
}

type DesyncStatus struct {
	Settings      DesyncSettings `json:"settings"`
	Platform      string         `json:"platform"`
	CoreSupported bool           `json:"coreSupported"`
	Connected     bool           `json:"connected"`
	Mode          string         `json:"mode"`
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
	s := currentDesktopPrefs().Desync
	if s == (DesyncSettings{}) {
		return DesyncSettings{YouTube: true, Discord: runtime.GOOS == "windows", DiscordTTL: 4}
	}
	if s.DiscordTTL == 0 {
		s.DiscordTTL = 4
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
	if s.Enabled {
		if !coreSupported {
			return s, fmt.Errorf("the embedded core does not support desync")
		}
		if !s.YouTube && !s.Discord {
			return s, fmt.Errorf("select YouTube or Discord before enabling desync")
		}
		if s.Discord && platform != "windows" {
			return s, fmt.Errorf("the Discord fake method currently requires Windows")
		}
	}
	return s, nil
}

func (a *App) GetDesyncStatus() DesyncStatus {
	a.mu.RLock()
	connected := a.state.Connection.Status == "connected" && a.state.Core.Running
	mode := a.state.Mode.Current
	a.mu.RUnlock()
	return DesyncStatus{Settings: currentDesyncSettings(), Platform: runtime.GOOS, CoreSupported: desyncCoreSupported(), Connected: connected, Mode: mode}
}

func (a *App) SetDesyncSettings(next DesyncSettings) (DesyncStatus, error) {
	next, err := validateDesyncSettings(next, runtime.GOOS, desyncCoreSupported())
	if err != nil {
		return a.GetDesyncStatus(), err
	}
	prefsMu.Lock()
	prefsCurrent.Desync = next
	snapshot := prefsCurrent
	savePrefsBestEffort(snapshot)
	prefsMu.Unlock()
	a.appendRuntimeDiag("desync.settings", fmt.Sprintf("enabled=%t youtube=%t discord=%t ttl=%d", next.Enabled, next.YouTube, next.Discord, next.DiscordTTL))
	a.triggerRuntimeReloadForPrefs()
	return a.GetDesyncStatus(), nil
}

func desyncRules(domains []string, route string) []string {
	var rules []string
	for _, domain := range domains {
		rules = append(rules, "AND,((NETWORK,udp),(DST-PORT,443),(DOMAIN-SUFFIX,"+domain+")),REJECT")
		rules = append(rules, "AND,((NETWORK,tcp),(DOMAIN-SUFFIX,"+domain+")),"+route)
	}
	return rules
}

// The profile body is never edited. Only generated runtime entries are
// replaced; user scripts cannot silently remove an explicitly enabled route.
func applyDesyncOverlay(m map[string]any, s DesyncSettings) {
	managedRules := map[string]bool{}
	for _, rule := range append(desyncRules(desyncYouTubeDomains, desyncYouTubeRoute), desyncRules(desyncDiscordDomains, desyncDiscordRoute)...) {
		managedRules[rule] = true
	}
	proxies := []any{}
	removed := false
	for _, value := range desyncSlice(m["proxies"]) {
		entry, ok := value.(map[string]any)
		if ok && entry[desyncManagedKey] == true {
			removed = true
			continue
		}
		proxies = append(proxies, value)
	}
	rules := []any{}
	for _, value := range desyncSlice(m["rules"]) {
		if text, ok := value.(string); ok && managedRules[text] {
			removed = true
			continue
		}
		rules = append(rules, value)
	}
	if !s.Enabled && !removed {
		return
	}
	generated := []any{}
	if s.Enabled && s.YouTube {
		proxies = append(proxies, map[string]any{"name": desyncYouTubeRoute, "type": "direct", desyncManagedKey: true, "desync": map[string]any{"tlsrec": "3+s"}})
		for _, rule := range desyncRules(desyncYouTubeDomains, desyncYouTubeRoute) {
			generated = append(generated, rule)
		}
	}
	if s.Enabled && s.Discord {
		ttl := s.DiscordTTL
		if ttl == 0 {
			ttl = 4
		}
		proxies = append(proxies, map[string]any{"name": desyncDiscordRoute, "type": "direct", desyncManagedKey: true, "desync": map[string]any{"tlsrec": "1+s", "fake": map[string]any{"ttl": ttl, "prefix": -1}}})
		for _, rule := range desyncRules(desyncDiscordDomains, desyncDiscordRoute) {
			generated = append(generated, rule)
		}
	}
	// Do not create empty keys in a profile when the feature has never been used.
	if _, present := m["proxies"]; present || len(proxies) > 0 {
		m["proxies"] = proxies
	}
	if _, present := m["rules"]; present || len(generated) > 0 {
		m["rules"] = append(generated, rules...)
	}
}

func desyncSlice(value any) []any { values, _ := value.([]any); return values }

func probeDesyncOutbound(ctx context.Context, endpoint, secret, route, target string) (int, error) {
	q := url.Values{"url": {target}, "timeout": {"10000"}}
	path := "/proxies/" + url.PathEscape(route) + "/delay?" + q.Encode()
	resp, err := coreDoWithEndpoint(ctx, endpoint, secret, http.MethodGet, path, nil)
	if err != nil {
		return 0, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
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
	secret := a.coreSecret
	a.mu.RUnlock()
	var checks []DesyncProbeResult
	if status.Settings.YouTube {
		checks = append(checks, DesyncProbeResult{Name: "YouTube HTTPS", URL: "https://www.youtube.com/generate_204", Route: desyncYouTubeRoute})
	}
	if status.Settings.Discord {
		checks = append(checks, DesyncProbeResult{Name: "Discord API", URL: "https://discord.com/api/v10/gateway", Route: desyncDiscordRoute}, DesyncProbeResult{Name: "Discord CDN", URL: "https://cdn.discordapp.com/embed/avatars/0.png", Route: desyncDiscordRoute})
	}
	parent := a.ctx
	if parent == nil {
		parent = context.Background()
	}
	ctx, cancel := context.WithTimeout(parent, 35*time.Second)
	defer cancel()
	for i := range checks {
		probeCtx, probeCancel := context.WithTimeout(ctx, 11*time.Second)
		delay, err := probeDesyncOutbound(probeCtx, endpoint, secret, checks[i].Route, checks[i].URL)
		probeCancel()
		checks[i].DelayMS = delay
		checks[i].OK = err == nil
		if err != nil {
			checks[i].Error = err.Error()
		}
		a.appendRuntimeDiag("desync.probe", fmt.Sprintf("route=%s target=%s ok=%t delay_ms=%d", checks[i].Route, checks[i].Name, checks[i].OK, delay))
	}
	return checks, nil
}
