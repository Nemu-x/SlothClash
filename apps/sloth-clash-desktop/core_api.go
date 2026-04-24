package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"
)

// Core control API — thin wrappers over Mihomo's /configs endpoint. Most
// runtime-config mutations (Connect/Disconnect, traffic-mode toggle, template
// save, subscription refresh) go through coreReloadConfigFile (PUT /configs
// force-reload): matches clash-verge-rev's CoreManager::update_config flow
// and keeps a long-lived core across every state transition.
//
// coreSetTunEnabledAt (PATCH /configs tun.enable) is intentionally kept for
// one case only: the pre-shutdown safe teardown of wintun/utun. On app quit
// we flip tun.enable=false on the API before the process is killed so the
// adapter unwinds cleanly instead of being stranded in an "on" zombie state
// (see shutdown() in app.go). All other flows use PUT force-reload.

const (
	coreTunToggleTimeout    = 10 * time.Second
	coreConfigReloadTimeout = 30 * time.Second
)

// coreSetTunEnabledAt flips tun.enable on the running core via PATCH /configs.
// Use only for pre-shutdown graceful adapter teardown; normal runtime flows
// should go through coreReloadConfigFileAt instead.
func coreSetTunEnabledAt(ctx context.Context, listen, secret string, enabled bool) error {
	if strings.TrimSpace(listen) == "" {
		return errors.New("core not configured")
	}
	payload := map[string]any{
		"tun": map[string]any{
			"enable": enabled,
		},
	}
	raw, err := json.Marshal(payload)
	if err != nil {
		return err
	}
	cctx, cancel := context.WithTimeout(ctx, coreTunToggleTimeout)
	defer cancel()
	resp, err := coreDoWithEndpoint(cctx, listen, secret, http.MethodPatch, "/configs", bytes.NewReader(raw))
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	b, _ := io.ReadAll(io.LimitReader(resp.Body, 64*1024))
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		msg := strings.TrimSpace(string(b))
		if msg == "" {
			msg = resp.Status
		}
		return fmt.Errorf("PATCH /configs tun.enable=%v: HTTP %d: %s", enabled, resp.StatusCode, msg)
	}
	return nil
}

// coreReloadConfigFileAt tells the running core to re-read the given YAML file
// and merge it into its live state. This is the runtime-config hot-reload
// entry point used by every non-shutdown flow (Connect, Disconnect,
// SetTrafficMode, template save, subscription refresh) — verbatim parity with
// clash-verge-rev's reload_config call.
//
// absPath must be readable by whoever owns the core process — on Windows this
// is LocalSystem (via sloth-clash-service), on macOS it is the user.
func coreReloadConfigFileAt(ctx context.Context, listen, secret, absPath string) error {
	if strings.TrimSpace(listen) == "" {
		return errors.New("core not configured")
	}
	if strings.TrimSpace(absPath) == "" {
		return errors.New("reload path is required")
	}
	q := url.Values{}
	q.Set("path", absPath)
	q.Set("force", "true")
	cctx, cancel := context.WithTimeout(ctx, coreConfigReloadTimeout)
	defer cancel()
	resp, err := coreDoWithEndpoint(cctx, listen, secret, http.MethodPut, "/configs?"+q.Encode(), bytes.NewReader([]byte("{}")))
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	b, _ := io.ReadAll(io.LimitReader(resp.Body, 64*1024))
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		msg := strings.TrimSpace(string(b))
		if msg == "" {
			msg = resp.Status
		}
		return fmt.Errorf("PUT /configs force reload %q: HTTP %d: %s", absPath, resp.StatusCode, msg)
	}
	return nil
}

// coreSetModeAt is a thin alias over the existing PATCH mode helper so all
// /configs interactions live next to each other. Keeps compatibility with
// callers that already use applyCoreModeHTTPWithGlobal for mode+global sync.
func coreSetModeAt(ctx context.Context, listen, secret, mode string) error {
	if strings.TrimSpace(listen) == "" {
		return errors.New("core not configured")
	}
	mode = strings.ToLower(strings.TrimSpace(mode))
	if mode != "rule" && mode != "global" && mode != "direct" {
		return fmt.Errorf("invalid mode %q", mode)
	}
	raw, err := json.Marshal(map[string]any{"mode": mode})
	if err != nil {
		return err
	}
	cctx, cancel := context.WithTimeout(ctx, coreTunToggleTimeout)
	defer cancel()
	resp, err := coreDoWithEndpoint(cctx, listen, secret, http.MethodPatch, "/configs", bytes.NewReader(raw))
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	b, _ := io.ReadAll(io.LimitReader(resp.Body, 64*1024))
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		msg := strings.TrimSpace(string(b))
		if msg == "" {
			msg = resp.Status
		}
		return fmt.Errorf("PATCH /configs mode=%s: HTTP %d: %s", mode, resp.StatusCode, msg)
	}
	return nil
}
