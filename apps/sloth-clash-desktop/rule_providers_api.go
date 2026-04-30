package main

import (
	"context"
	"errors"
	"net/http"
	"net/url"
	"os"
	"strings"
	"time"
)

// UpdateRuleProvider triggers mihomo rule-provider refresh:
// PUT /providers/rules/{name}
// It uses the running embedded core endpoint when available, otherwise
// falls back to SLOTH_CLASH_CONTROLLER / SLOTH_CLASH_SECRET (external core mode).
func (a *App) UpdateRuleProvider(name string) error {
	name = strings.TrimSpace(name)
	if name == "" {
		return errors.New("provider name is required")
	}

	a.mu.RLock()
	conn := strings.TrimSpace(a.state.Connection.Status)
	running := a.state.Core.Running
	ep := a.effectiveCoreEndpointLocked()
	secret := a.coreSecret
	a.mu.RUnlock()

	// Mirror FetchRulesOverview semantics: connected OR running with an endpoint
	// is enough to treat the embedded core as the primary target.
	if ep == "" || (conn != "connected" && !running) {
		base := strings.TrimSpace(os.Getenv("SLOTH_CLASH_CONTROLLER"))
		if base == "" {
			return errors.New("connect Sloth or set SLOTH_CLASH_CONTROLLER for external core")
		}
		if !strings.HasPrefix(base, "http://") && !strings.HasPrefix(base, "https://") {
			base = "http://" + base
		}
		base = strings.TrimRight(base, "/")
		ep = strings.TrimPrefix(base, "http://")
		ep = strings.TrimPrefix(ep, "https://")
		secret = strings.TrimSpace(os.Getenv("SLOTH_CLASH_SECRET"))
	}

	ctx, cancel := context.WithTimeout(context.Background(), 18*time.Second)
	defer cancel()
	path := "/providers/rules/" + url.PathEscape(name)
	resp, err := coreDoWithEndpoint(ctx, ep, secret, http.MethodPut, path, nil)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return errors.New("update failed: HTTP " + resp.Status)
	}
	return nil
}
