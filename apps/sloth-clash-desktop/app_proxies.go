package main

import (
	"context"
	"errors"
	"io"
	"net/http"
	"os"
	"strconv"
	"strings"
	"time"
)

func (a *App) SelectProxyGroup(name string) (AppState, error) {
	a.mu.Lock()
	defer a.mu.Unlock()
	if name == "" {
		return a.state, errors.New("group name is required")
	}
	upper := strings.ToUpper(name)
	if upper == "REJECT" || upper == "DIRECT" {
		return a.state, errors.New("unsafe auto group")
	}
	a.state.Proxy.ActiveGroup = name
	a.state.Proxy.LastGoodGroup = name
	// Persist the user's explicit pick on the active profile so it
	// survives reconnects AND app restarts. That's the "today MainGroup,
	// tomorrow still MainGroup" memory the user asked for. Auto-picked
	// fallbacks below (anchor / first-safe) never touch this field, so
	// the stored value always reflects genuine user intent.
	if idx := a.activeProfileIndexLocked(); idx >= 0 {
		if strings.TrimSpace(a.profiles[idx].LastGoodGroup) != name {
			a.profiles[idx].LastGoodGroup = name
			a.state.Profile.Profiles = a.profiles
			if err := a.persistProfilesLocked(); err != nil {
				debugLog(
					"select-proxy-group",
					"H5",
					"app.go:SelectProxyGroup",
					"failed to persist sticky pick",
					map[string]any{"error": err.Error(), "group": name},
				)
			}
		}
	}
	a.state.UpdatedAt = time.Now().Unix()
	return a.state, nil
}

// restoreStickyGroupLocked is the full auto-pick story after we removed the
// old multi-tier picker (anchor / first-safe heuristics were destabilising
// reconnects and causing the "залипания" the user reported). The logic is
// deliberately minimal:
//
//   - If the active profile has a persisted LastGoodGroup (the user's last
//     manual SelectProxyGroup click for this profile) AND that group is
//     still present in the current /proxies snapshot, copy it into
//     ActiveGroup so the Proxies screen keeps highlighting it across
//     reconnects, app restarts, traffic-mode flips and mode changes.
//   - Otherwise, leave ActiveGroup untouched. First-ever connects land on
//     an empty ActiveGroup (UI shows "—") until the user clicks a group
//     on the Proxies screen — that click persists onto the profile and
//     this routine picks it up on every subsequent connect.
//
// Built-in policy tokens (GLOBAL / DIRECT / REJECT / PASS) are filtered
// so we never land Rule mode on a non-pickable outbound even if a stale
// sticky value somehow names one.
//
// This is NOT an automatic group picker. There is no fallback to any
// "first safe group" or to the terminal MATCH target. That was the
// piece that kept surprising the user; removing it is the whole point.
// Caller must hold a.mu.
func (a *App) restoreStickyGroupLocked() {
	sticky := a.activeProfileLastGoodGroupLocked()
	if sticky == "" {
		return
	}
	switch strings.ToUpper(sticky) {
	case "GLOBAL", "DIRECT", "REJECT", "REJECT-DROP", "PASS":
		return
	}
	stickyLower := strings.ToLower(sticky)
	for _, g := range a.state.Proxy.Groups {
		if strings.EqualFold(strings.ToLower(strings.TrimSpace(g.Name)), stickyLower) {
			a.state.Proxy.ActiveGroup = g.Name
			a.state.UpdatedAt = time.Now().Unix()
			debugLog(
				"sticky-restore",
				"H5",
				"app.go:restoreStickyGroupLocked",
				"restored sticky pick",
				map[string]any{
					"group":       g.Name,
					"groupsCount": len(a.state.Proxy.Groups),
				},
			)
			return
		}
	}
}

func (a *App) RefreshProxies() (AppState, error) {
	if err := a.pullProxiesIntoState(); err != nil {
		return a.GetAppState(), err
	}
	a.mu.Lock()
	defer a.mu.Unlock()
	a.state.UpdatedAt = time.Now().Unix()
	return a.state, nil
}

func (a *App) SetProxyNode(groupName, proxyName string) (AppState, error) {
	groupName = strings.TrimSpace(groupName)
	proxyName = strings.TrimSpace(proxyName)
	if groupName == "" || proxyName == "" {
		return a.GetAppState(), errors.New("group and proxy name are required")
	}

	var listen, secret string
	a.mu.Lock()
	listen = a.effectiveCoreEndpointLocked()
	if listen == "" {
		a.mu.Unlock()
		return a.GetAppState(), errors.New("core not running")
	}
	secret = a.core.secret
	a.mu.Unlock()

	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	if err := putProxySelectionAt(ctx, listen, secret, groupName, proxyName); err != nil {
		return a.GetAppState(), err
	}
	if err := a.pullProxiesIntoState(); err != nil {
		return a.GetAppState(), err
	}

	a.mu.Lock()
	defer a.mu.Unlock()
	a.state.UpdatedAt = time.Now().Unix()
	return a.state, nil
}

// FetchRulesOverview reads rules from the embedded Sloth core when connected; otherwise
// falls back to SLOTH_CLASH_CONTROLLER / SLOTH_CLASH_SECRET (e.g. external Verge).
func (a *App) FetchRulesOverview() RulesOverview {
	a.mu.RLock()
	conn := strings.TrimSpace(a.state.Connection.Status)
	running := a.state.Core.Running
	ep := a.effectiveCoreEndpointLocked()
	secret := a.core.secret
	a.mu.RUnlock()
	// Do not require Core.Running: it can lag in the UI; "connected" + controller endpoint is enough (Verge-style).
	if ep != "" && (conn == "connected" || running) {
		return a.rulesOverviewFetch(ep, secret)
	}

	base := strings.TrimSpace(os.Getenv("SLOTH_CLASH_CONTROLLER"))
	if base == "" {
		return RulesOverview{LastError: "connect Sloth or set SLOTH_CLASH_CONTROLLER for external core"}
	}
	if !strings.HasPrefix(base, "http://") && !strings.HasPrefix(base, "https://") {
		base = "http://" + base
	}
	base = strings.TrimRight(base, "/")
	envSecret := strings.TrimSpace(os.Getenv("SLOTH_CLASH_SECRET"))

	client := &http.Client{Timeout: 4 * time.Second}
	out := RulesOverview{Controller: base}

	req, err := http.NewRequestWithContext(context.Background(), http.MethodGet, base+"/rules", nil)
	if err != nil {
		out.LastError = err.Error()
		return out
	}
	if envSecret != "" {
		req.Header.Set("Authorization", "Bearer "+envSecret)
	}
	resp, err := client.Do(req)
	if err != nil {
		out.LastError = "GET /rules: " + err.Error()
		return out
	}
	func() {
		defer resp.Body.Close()
		if resp.StatusCode < 200 || resp.StatusCode >= 300 {
			b, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
			out.LastError = "GET /rules: HTTP " + strconv.Itoa(resp.StatusCode) + " " + strings.TrimSpace(string(b))
			return
		}
		body, rerr := readRulesOverviewJSON(resp.Body)
		if rerr != nil {
			out.LastError = "GET /rules: " + rerr.Error()
			return
		}
		out.Reachable = true
		out.RulesBody = string(body)
	}()

	if out.LastError != "" {
		return out
	}

	req2, err := http.NewRequestWithContext(context.Background(), http.MethodGet, base+"/providers/rules", nil)
	if err != nil {
		return out
	}
	if envSecret != "" {
		req2.Header.Set("Authorization", "Bearer "+envSecret)
	}
	resp2, err := client.Do(req2)
	if err != nil {
		out.LastError = "GET /providers/rules: " + err.Error()
		return out
	}
	defer resp2.Body.Close()
	if resp2.StatusCode < 200 || resp2.StatusCode >= 300 {
		out.LastError = "GET /providers/rules: HTTP " + strconv.Itoa(resp2.StatusCode)
		return out
	}
	body2, err := readRulesOverviewJSON(resp2.Body)
	if err != nil {
		out.LastError = "GET /providers/rules: " + err.Error()
		return out
	}
	out.RuleProvidersBody = string(body2)
	return out
}
