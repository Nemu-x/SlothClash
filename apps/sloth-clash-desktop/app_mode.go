package main

import (
	"context"
	"errors"
	"runtime"
	"strings"
	"time"
)

func (a *App) SetMode(mode string) (AppState, error) {
	if mode != "rule" && mode != "global" && mode != "direct" {
		return a.GetAppState(), errors.New("invalid mode")
	}

	a.mu.Lock()
	connected := a.state.Connection.Status == "connected"
	listen := a.effectiveCoreEndpointLocked()
	secret := a.core.secret
	activeGroup := strings.TrimSpace(a.state.Proxy.ActiveGroup)
	a.mu.Unlock()

	if connected && listen != "" {
		modeCtx, modeCancel := context.WithTimeout(context.Background(), coreModeApplyTimeout)
		err := applyCoreModeHTTPWithGlobal(modeCtx, listen, secret, mode, activeGroup)
		modeCancel()
		if err != nil {
			return a.GetAppState(), err
		}
		if err := a.pullProxiesIntoState(); err != nil {
			return a.GetAppState(), err
		}
	}

	a.mu.Lock()
	defer a.mu.Unlock()
	if mode != "direct" {
		a.state.Mode.LastNonDirectMode = mode
	}
	a.state.Mode.Current = mode
	if mode == "global" {
		a.state.Proxy.ActiveGroup = "GLOBAL"
	} else {
		// Leaving global mode: UI focus should not stay on "GLOBAL" in
		// rule/direct. Drop it and let the per-profile sticky pick take
		// over (no-op if the user never picked a group for this profile).
		if strings.EqualFold(strings.TrimSpace(a.state.Proxy.ActiveGroup), "GLOBAL") {
			a.state.Proxy.ActiveGroup = ""
		}
		a.restoreStickyGroupLocked()
	}
	a.state.UpdatedAt = time.Now().Unix()
	if err := a.persistProfilesLocked(); err != nil {
		return a.state, err
	}
	return a.state, nil
}

func (a *App) SetTrafficMode(mode string) (AppState, error) {
	a.mu.Lock()
	if mode != "proxy" && mode != "tun" {
		a.mu.Unlock()
		return a.GetAppState(), errors.New("invalid traffic mode")
	}
	if mode == "tun" && !a.state.Service.Installed {
		a.mu.Unlock()
		return a.GetAppState(), errors.New("service required")
	}
	prev := a.state.Traffic
	connected := a.state.Connection.Status == "connected"
	needsReload := connected && prev != mode
	a.state.Traffic = mode
	if err := a.persistProfilesLocked(); err != nil {
		a.mu.Unlock()
		return a.GetAppState(), err
	}
	if needsReload {
		if mode == "tun" {
			// Moving proxy → tun: clear OS proxy so DNS hijack routes through TUN.
			a.clearSystemProxyLocked()
		} else {
			// Moving tun → proxy: apply OS proxy for direct app traffic.
			_ = a.applySystemProxyIfNeededLocked()
		}
	}
	a.state.UpdatedAt = time.Now().Unix()
	activeID := strings.TrimSpace(a.state.Profile.ActiveProfileID)
	var active Profile
	activeFound := false
	for _, p := range a.profiles {
		if p.ID == activeID {
			active = p
			activeFound = true
			break
		}
	}
	listen := a.effectiveCoreEndpointLocked()
	a.mu.Unlock()

	if needsReload && activeFound && strings.TrimSpace(listen) != "" {
		// Reload-model traffic switch (aligned with clash-verge-rev
		// toggle_tun_mode → patch_verge → update_config → reload_config):
		// regenerate YAML with the new enableTun value and push PUT /configs
		// force-reload. Mihomo brings TUN up or down without restarting; we
		// never thrash wintun with PATCH flips.
		enableTun := mode == "tun"
		if err := a.applyRuntimeConfig(active, mode, enableTun); err != nil {
			// Rollback (audit D3-1): the reload failed, so the core did NOT switch —
			// but we already flipped state.Traffic AND the OS proxy toward the new
			// mode. A failed proxy→tun would otherwise strand the user with the OS
			// proxy cleared and no TUN (no egress at all). Undo both, then
			// best-effort re-apply the PREVIOUS runtime config so the core returns
			// to the working prev mode. If the rollback reload also fails, surface
			// broken; if it succeeds, the user stays connected as before.
			a.mu.Lock()
			a.state.Traffic = prev
			if prev == "tun" {
				a.clearSystemProxyLocked()
			} else {
				_ = a.applySystemProxyIfNeededLocked()
			}
			_ = a.persistProfilesLocked()
			a.state.UpdatedAt = time.Now().Unix()
			a.mu.Unlock()

			rollbackErr := a.applyRuntimeConfig(active, prev, prev == "tun")
			a.mu.Lock()
			if rollbackErr != nil && a.state.Connection.Status == "connected" {
				a.markConnectionBrokenLocked("Traffic mode could not be applied to the running core, and reverting to the previous mode also failed: " + strings.TrimSpace(err.Error()))
			}
			a.mu.Unlock()
			a.emitAppStateChanged()
			return a.GetAppState(), err
		}
	}
	return a.GetAppState(), nil
}

func (a *App) EnsureTunReady() TunSetupResult {
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.state.Service.Installed {
		a.state.Traffic = "tun"
		a.state.UpdatedAt = time.Now().Unix()
		if err := a.persistProfilesLocked(); err != nil {
			debugLog("profiles", "A4", "app.go", "failed to persist profiles",
				map[string]any{"error": err.Error()})
		}
		return TunSetupResult{Success: true, Message: "TUN enabled", InstallAction: false}
	}
	if !privilegedServiceSupported(runtime.GOOS) {
		return TunSetupResult{
			Success:       false,
			Message:       privilegedServiceUnsupportedMessage,
			InstallAction: false,
		}
	}
	return TunSetupResult{
		Success:       false,
		Message:       "Service required. Install service to continue or use Proxy mode.",
		InstallAction: true,
	}
}

func (a *App) GetTunStatus() ServiceState {
	a.refreshServiceStatus()
	a.mu.RLock()
	defer a.mu.RUnlock()
	return a.state.Service
}
