package main

// Connect / Disconnect lifecycle and post-connect warmup. Split out of
// app.go for readability — the entire flow lives in one place: state
// transitions, the async runConnectJob, the post-warmup pulls (proxies,
// mode, system proxy), and the Disconnect path that keeps the core alive
// for an instant Connect retry.

import (
	"context"
	"errors"
	"fmt"
	"runtime"
	"strconv"
	"strings"
	"time"
)

func (a *App) Connect() (AppState, error) {
	a.mu.Lock()
	if len(a.profiles) == 0 {
		a.mu.Unlock()
		return a.GetAppState(), errors.New("no profiles — import a subscription first")
	}
	if a.state.Profile.ActiveProfileID == "" {
		a.mu.Unlock()
		return a.GetAppState(), errors.New("no active profile — pick a profile under Profiles")
	}
	var active Profile
	found := false
	for _, p := range a.profiles {
		if p.ID == a.state.Profile.ActiveProfileID {
			active = p
			found = true
			break
		}
	}
	if !found {
		a.mu.Unlock()
		return a.GetAppState(), errors.New("active profile not found")
	}
	switch strings.TrimSpace(a.state.Connection.Status) {
	case ConnConnected:
		a.mu.Unlock()
		return a.GetAppState(), nil
	case ConnConnecting:
		// Idempotent: second tap while the async job runs (Verge-style UX, no scary error banner).
		a.mu.Unlock()
		return a.GetAppState(), nil
	}
	a.state.Connection.Status = ConnConnecting
	a.state.Connection.Health = ""
	a.state.Connection.LastError = ""
	a.state.Connection.LastWarning = ""
	a.state.UpdatedAt = time.Now().Unix()
	gen := a.connectGen.Add(1)
	a.mu.Unlock()
	a.traceEvent("pipeline.connect.requested", "ok", 0, map[string]any{
		"profileId": active.ID,
		"gen":       gen,
	})

	go a.runConnectJob(active, gen)
	a.emitAppStateChanged()
	return a.GetAppState(), nil
}

func (a *App) runConnectJob(active Profile, gen uint64) {
	// Reload-model Connect (aligned with clash-verge-rev CoreManager::init +
	// update_config):
	//   1. Compute effective TUN intent from user state. This is the single
	//      source of truth inlined into YAML tun.enable, so the running core
	//      sees a stable value on every subsequent hot reload.
	//   2. Ensure a Mihomo is running for this profile. If a cold start is
	//      needed, startEmbeddedCore writes YAML with enableTun baked in so
	//      the process comes up in the final state without a follow-up PATCH.
	//   3. If the core was already up, applyRuntimeConfig regenerates YAML
	//      and pushes PUT /configs?force=true — wintun is not torn down when
	//      the intent was already matching.
	//
	// Every major step is wrapped in a trace so debug-cb9690.log shows
	// exactly where Connect stalls. A panic here used to silently strand the
	// status at "connecting" with no Connect-job left to advance it; recover
	// surfaces that condition explicitly via the trace and marks the
	// connection as failed so the UI can move on.
	defer func() {
		if r := recover(); r != nil {
			a.traceEvent("pipeline.connect.panic", "fail", 0, map[string]any{
				"gen":   gen,
				"panic": fmt.Sprintf("%v", r),
			})
			a.finishConnectJobFailed(gen, fmt.Errorf("connect goroutine panicked: %v", r))
		}
	}()

	a.mu.RLock()
	traffic := strings.TrimSpace(a.state.Traffic)
	a.mu.RUnlock()
	if traffic != "proxy" && traffic != "tun" {
		traffic = "tun"
	}
	enableTun := traffic == "tun"

	ensureStart := time.Now()
	a.traceEvent("pipeline.connect.ensure_core", "start", 0, map[string]any{
		"gen": gen, "profileId": active.ID, "enableTun": enableTun,
	})
	coldStarted, err := a.ensureCoreForProfileEx(active, gen, enableTun)
	if err != nil {
		a.traceEvent("pipeline.connect.ensure_core", "fail", time.Since(ensureStart), map[string]any{
			"gen": gen, "error": err.Error(),
		})
		a.finishConnectJobFailed(gen, err)
		return
	}
	a.traceEvent("pipeline.connect.ensure_core", "ok", time.Since(ensureStart), map[string]any{
		"gen": gen, "coldStarted": coldStarted,
	})
	if a.connectGen.Load() != gen {
		a.traceEvent("pipeline.connect.aborted", "skip", 0, map[string]any{
			"gen": gen, "where": "after_ensure_core",
		})
		return
	}

	// applyRuntimeConfig regenerates YAML and pushes PUT /configs?force=true.
	// On a cold start we skip it: ensureCoreForProfileEx already wrote the
	// canonical YAML to disk and mihomo loaded it on startup, so a follow-up
	// force-reload would just trigger a second full init (rule-providers
	// re-download, sniffer re-bind, mixed-port re-listen) for no behaviour
	// change — and the second init has to wait for providers, which is what
	// gave Connect a multi-second hitch on heavy subscriptions. On a hot path
	// (core reused, same profile) we still run apply so pending template /
	// subscription edits and the user's traffic intent reach the live core in
	// one atomic reload.
	if !coldStarted {
		applyStart := time.Now()
		a.traceEvent("pipeline.connect.apply_runtime", "start", 0, map[string]any{
			"gen": gen, "traffic": traffic, "enableTun": enableTun,
		})
		if err := a.applyRuntimeConfig(active, traffic, enableTun); err != nil {
			a.traceEvent("pipeline.connect.apply_runtime", "fail", time.Since(applyStart), map[string]any{
				"gen": gen, "error": err.Error(),
			})
			a.finishConnectJobFailed(gen, fmt.Errorf("apply runtime config: %w", err))
			return
		}
		a.traceEvent("pipeline.connect.apply_runtime", "ok", time.Since(applyStart), map[string]any{
			"gen": gen,
		})
	} else {
		a.traceEvent("pipeline.connect.apply_runtime", "skip", 0, map[string]any{
			"gen": gen, "reason": "cold_start_yaml_already_loaded",
		})
	}
	if a.connectGen.Load() != gen {
		a.traceEvent("pipeline.connect.aborted", "skip", 0, map[string]any{
			"gen": gen, "where": "after_apply_runtime",
		})
		return
	}

	// Treat "core is listening with the desired intent applied" as connected
	// immediately; /proxies warmup runs in the background below.
	a.finishConnectJobOK(gen)
	if a.connectGen.Load() != gen {
		a.traceEvent("pipeline.connect.aborted", "skip", 0, map[string]any{
			"gen": gen, "where": "after_finish_ok",
		})
		return
	}

	warmupStart := time.Now()
	a.traceEvent("pipeline.connect.warmup", "start", 0, map[string]any{"gen": gen})
	if err := a.connectAfterCoreStarts(gen); err != nil {
		if errors.Is(err, errConnectAborted) {
			a.traceEvent("pipeline.connect.warmup", "cancelled", time.Since(warmupStart), map[string]any{
				"gen": gen,
			})
			return
		}
		a.traceEvent("pipeline.connect.warmup", "fail", time.Since(warmupStart), map[string]any{
			"gen": gen, "error": err.Error(),
		})
		a.finishPostConnectWarmupFailed(gen, err)
		return
	}
	a.traceEvent("pipeline.connect.warmup", "ok", time.Since(warmupStart), map[string]any{
		"gen": gen,
	})
	a.emitAppStateChanged()
	go func() { _, _ = a.RefreshHomeInsight() }()
}

func (a *App) finishConnectJobFailed(gen uint64, err error) {
	if errors.Is(err, errConnectAborted) {
		return
	}
	var notify bool
	a.mu.Lock()
	if a.connectGen.Load() == gen && a.state.Connection.Status == ConnConnecting {
		// In the reload model we no longer tear down the core on connect
		// failure: if ensureCoreForProfile itself failed, startEmbeddedCore
		// already cleaned up internally; if the applyRuntimeConfig step
		// (PUT /configs?force=true) failed, the core is still healthy and
		// a retry should succeed without paying another cold-start.
		a.state.Connection.Status = ConnError
		a.state.Connection.Health = ""
		a.state.Connection.LastError = err.Error()
		a.state.UpdatedAt = time.Now().Unix()
		notify = true
	}
	a.mu.Unlock()
	if notify {
		a.traceEvent("pipeline.connect.done", "fail", 0, map[string]any{
			"gen":   gen,
			"error": err.Error(),
		})
		a.emitAppStateChanged()
	}
}

func (a *App) finishConnectJobOK(gen uint64) {
	var notify bool
	a.mu.Lock()
	curGen := a.connectGen.Load()
	curStatus := a.state.Connection.Status
	if curGen == gen && curStatus == ConnConnecting {
		a.state.Connection.Status = ConnConnected
		// Health stays empty until post-connect warmup (sysproxy/TUN/mode) finishes
		// so UI does not show "ready/protected" while OS/core may still mismatch intent.
		a.state.Connection.Health = ""
		a.state.Connection.Since = time.Now().Unix()
		a.state.Connection.LastError = ""
		a.state.UpdatedAt = time.Now().Unix()
		notify = true
	}
	a.mu.Unlock()
	if notify {
		a.traceEvent("pipeline.connect.done", "ok", 0, map[string]any{"gen": gen})
		a.emitAppStateChanged()
	} else {
		// Visibility: when the OK marker silently does nothing the user used
		// to be stuck on "Connecting" with no way to tell why. Surface the
		// skip so debug-cb9690.log shows whether it was a stale gen or a
		// status drift caused by a parallel state transition.
		a.traceEvent("pipeline.connect.done", "skip", 0, map[string]any{
			"gen":       gen,
			"curGen":    curGen,
			"curStatus": curStatus,
			"reason":    "gen-mismatch-or-status-drift",
		})
	}
}

// finishPostConnectWarmupFailed runs after we already marked "connected" but non-critical
// warmup steps (mode/proxy sync/system proxy) failed. Keep session alive and surface warning.
func (a *App) finishPostConnectWarmupFailed(gen uint64, err error) {
	var notify bool
	a.mu.Lock()
	if a.connectGen.Load() == gen && a.state.Connection.Status == ConnConnected {
		msg := strings.TrimSpace(err.Error())
		if msg != "" && !isIgnorableWarmupWarning(msg) {
			a.markConnectionDegradedLocked("Post-connect warmup issue: " + msg)
		}
		a.state.UpdatedAt = time.Now().Unix()
		notify = true
	}
	a.mu.Unlock()
	if notify {
		a.emitAppStateChanged()
	}
}

func appendConnectionWarningLocked(current string, next string) string {
	msg := strings.TrimSpace(next)
	if msg == "" || isIgnorableWarmupWarning(msg) {
		return current
	}
	if strings.TrimSpace(current) == "" {
		return msg
	}
	return strings.TrimSpace(current) + " | " + msg
}

func (a *App) markConnectionReadyLocked() {
	if a.state.Connection.Health == "broken" {
		return
	}
	a.state.Connection.Health = "ready"
	a.state.Core.Lifecycle = "running"
	a.appendRuntimeDiag("connection.ready", "")
}

func (a *App) markConnectionDegradedLocked(reason string) {
	if a.state.Connection.Health == "broken" {
		return
	}
	if strings.TrimSpace(reason) != "" {
		a.state.Connection.LastWarning = appendConnectionWarningLocked(a.state.Connection.LastWarning, reason)
	}
	if strings.TrimSpace(a.state.Connection.LastWarning) == "" {
		return
	}
	a.state.Connection.Health = "degraded"
	a.state.Core.Lifecycle = "degraded"
	a.appendRuntimeDiag("connection.degraded", reason)
}

func (a *App) markConnectionBrokenLocked(reason string) {
	msg := strings.TrimSpace(reason)
	if msg == "" {
		msg = "connection broken"
	}
	a.state.Connection.Health = "broken"
	a.state.Connection.LastWarning = msg
	a.state.Core.Lifecycle = "degraded"
	a.appendRuntimeDiag("connection.broken", msg)
}

func isIgnorableWarmupWarning(msg string) bool {
	s := strings.ToLower(strings.TrimSpace(msg))
	if s == "" {
		return true
	}
	if strings.Contains(s, "exit status 4") {
		return true
	}
	if strings.Contains(s, "parameters were not valid") {
		return true
	}
	return false
}

func formatTunTakeoverWarning(err error) string {
	msg := strings.TrimSpace(err.Error())
	if msg == "" {
		msg = "unknown error"
	}
	return "TUN: could not stop the other app's Windows service (often needs admin rights, or that app restarts the service). Staying connected — if routing is wrong, switch to System proxy or stop the conflicting service in Windows Services. Technical: " + msg
}

// connectAfterCoreStarts runs pull-proxies, mode API, and system-proxy steps without holding a.mu
// during network I/O (avoids deadlocking GetAppState / Wails bridge).
func (a *App) connectAfterCoreStarts(gen uint64) error {
	if a.connectGen.Load() != gen {
		return errConnectAborted
	}
	var listen, secret, mode, traffic string
	a.mu.Lock()
	listen = a.effectiveCoreEndpointLocked()
	if listen == "" {
		a.mu.Unlock()
		return errors.New("core not running")
	}
	secret = a.coreSecret
	mode = strings.TrimSpace(a.state.Mode.Current)
	traffic = strings.TrimSpace(a.state.Traffic)
	if mode != "rule" && mode != "global" && mode != "direct" {
		mode = "rule"
	}
	a.mu.Unlock()
	if a.connectGen.Load() != gen {
		return errConnectAborted
	}

	if traffic == "tun" {
		stopped, err := takeoverConflictingTunServices()
		if a.connectGen.Load() != gen {
			return errConnectAborted
		}
		a.mu.Lock()
		if err != nil {
			// Do not tear down the session: stopping another vendor's service often needs admin
			// rights; users can still use proxy path or stop Verge's service manually.
			a.markConnectionDegradedLocked(formatTunTakeoverWarning(err))
		} else {
			a.state.Connection.LastWarning = ""
			if len(stopped) > 0 {
				a.tunTakenOver = append([]tunServiceTakeover(nil), stopped...)
			}
		}
		a.mu.Unlock()
	} else {
		a.mu.Lock()
		a.state.Connection.LastWarning = ""
		a.mu.Unlock()
	}

	// /proxies is often empty right after the core starts (providers still loading). Retry briefly
	// so Active group / node are not blank until unrelated UI (e.g. warnings) triggers another tick.
	// Do not fail whole connect flow on transient /proxies errors (some providers temporarily return
	// backend-specific errors like "exit status 4" during warmup).
	var proxiesWarmupErr error
	for attempt := 0; ; attempt++ {
		if a.connectGen.Load() != gen {
			return errConnectAborted
		}
		if err := a.pullProxiesIntoState(); err != nil {
			proxiesWarmupErr = err
			if attempt >= 5 {
				break
			}
			time.Sleep(220 * time.Millisecond)
			continue
		}
		if a.connectGen.Load() != gen {
			return errConnectAborted
		}
		proxiesWarmupErr = nil
		a.mu.RLock()
		n := len(a.state.Proxy.Groups)
		a.mu.RUnlock()
		if n > 0 || attempt >= 5 {
			break
		}
		time.Sleep(220 * time.Millisecond)
	}
	if a.connectGen.Load() != gen {
		return errConnectAborted
	}
	if proxiesWarmupErr != nil {
		a.mu.Lock()
		msg := "Proxy groups are still warming up: " + strings.TrimSpace(proxiesWarmupErr.Error())
		a.markConnectionDegradedLocked(msg)
		a.mu.Unlock()
	}

	a.mu.Lock()
	a.restoreStickyGroupLocked()
	activeGroup := strings.TrimSpace(a.state.Proxy.ActiveGroup)
	a.state.UpdatedAt = time.Now().Unix()
	a.mu.Unlock()
	// Push proxies + active group before mode/system-proxy steps (can be slow); fixes empty Home until warmup ends.
	a.emitAppStateChanged()
	if a.connectGen.Load() != gen {
		return errConnectAborted
	}

	modeCtx, modeCancel := context.WithTimeout(context.Background(), coreModeApplyTimeout)
	errMode := applyCoreModeHTTPWithGlobal(modeCtx, listen, secret, mode, activeGroup)
	modeCancel()
	if errMode != nil {
		a.mu.Lock()
		a.markConnectionDegradedLocked("Could not apply core mode immediately: " + strings.TrimSpace(errMode.Error()))
		a.mu.Unlock()
	}
	if a.connectGen.Load() != gen {
		return errConnectAborted
	}
	if err := a.pullProxiesIntoState(); err != nil {
		a.mu.Lock()
		msg := "Could not refresh proxy groups after mode apply: " + strings.TrimSpace(err.Error())
		a.markConnectionDegradedLocked(msg)
		a.mu.Unlock()
	}
	a.mu.Lock()
	if mode == "global" {
		a.state.Proxy.ActiveGroup = "GLOBAL"
	} else {
		a.restoreStickyGroupLocked()
	}
	a.state.UpdatedAt = time.Now().Unix()
	a.mu.Unlock()
	// Second pull carries updated `now` selections — push before system-proxy (can be slow).
	a.emitAppStateChanged()
	if a.connectGen.Load() != gen {
		return errConnectAborted
	}

	if err := a.clearSystemProxyFromSnapshot(); err != nil {
		a.mu.Lock()
		a.markConnectionDegradedLocked("Could not clear system proxy snapshot: " + strings.TrimSpace(err.Error()))
		a.mu.Unlock()
	}
	if a.connectGen.Load() != gen {
		return errConnectAborted
	}

	if err := a.applySystemProxyFromSnapshot(); err != nil {
		a.mu.Lock()
		a.markConnectionDegradedLocked("Could not apply system proxy snapshot: " + strings.TrimSpace(err.Error()))
		a.mu.Unlock()
	}

	a.mu.Lock()
	if a.connectGen.Load() == gen && a.state.Connection.Status == ConnConnected {
		h := strings.TrimSpace(a.state.Connection.Health)
		lw := strings.TrimSpace(a.state.Connection.LastWarning)
		switch h {
		case "broken":
			// leave as-is
		case "degraded":
			// leave as-is (warmup already classified the session)
		default:
			if lw != "" {
				if h != "degraded" {
					a.markConnectionDegradedLocked("")
				}
			} else {
				a.markConnectionReadyLocked()
			}
		}
	}
	a.mu.Unlock()
	return nil
}

func (a *App) Disconnect() AppState {
	gen := a.connectGen.Add(1)
	a.traceEvent("pipeline.disconnect.requested", "ok", 0, map[string]any{
		"gen":        gen,
		"prevStatus": a.state.Connection.Status,
	})
	// Reload-model Disconnect (aligned with clash-verge-rev toggle_tun_mode):
	// do not tear down Mihomo. Transition state to disconnected, clear the OS
	// system proxy, and regenerate YAML with enableTun=false + PUT /configs
	// force-reload. The core keeps running so the next Connect is instant and
	// wintun comes down cleanly (no PATCH / force-kill).
	a.mu.Lock()
	prevTraffic := strings.TrimSpace(a.state.Traffic)
	a.clearSystemProxyLocked()
	a.restoreTakenOverTunServicesLocked()
	a.state.Connection.Status = ConnDisconnected
	a.state.Connection.Health = ""
	a.state.Connection.Since = 0
	a.state.Connection.LastError = ""
	a.state.Connection.LastWarning = ""
	a.state.Proxy.Groups = []ProxyGroup{}
	a.state.Insight = HomeInsight{}
	a.state.UpdatedAt = time.Now().Unix()
	listen := a.effectiveCoreEndpointLocked()
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
	a.mu.Unlock()
	a.emitAppStateChanged()

	if prevTraffic == "tun" && activeFound && strings.TrimSpace(listen) != "" {
		// Windows-specific safety net for occasional stale wintun state:
		// try explicit tun.disable before async YAML force-reload. This is
		// best-effort and ignored on failure; normal flow remains reload-based.
		if runtime.GOOS == "windows" {
			go func(ep, sec string) {
				dctx, cancel := context.WithTimeout(context.Background(), 4*time.Second)
				defer cancel()
				_ = coreSetTunEnabledAt(dctx, ep, sec, false)
			}(listen, a.coreSecret)
		}
		// Regeneration + PUT runs off the Wails bridge so Disconnect returns
		// immediately. If the user clicks Connect again before the reload
		// resolves, runConnectJob will push a fresh YAML with enableTun=true
		// through the same path; force-reload is idempotent and the final
		// state is always whichever regeneration arrived last.
		go func(p Profile, savedTraffic string, disconnectGen uint64) {
			a.appendRuntimeDiag("tun.disconnect", "yaml reload scheduled")
			if err := a.applyRuntimeConfigWithGen(p, savedTraffic, false, disconnectGen); err != nil {
				if errors.Is(err, errConnectAborted) {
					return
				}
				a.appendRuntimeDiag("tun.reload", "failed: "+strings.TrimSpace(err.Error()))
				debugLog(
					"gen-"+strconv.FormatUint(disconnectGen, 10),
					"H1",
					"app.go:disconnect-reload",
					"failed to apply disconnected YAML; next Connect will override",
					map[string]any{"error": err.Error()},
				)
				return
			}
			a.appendRuntimeDiag("tun.reload", "ok")
		}(active, prevTraffic, gen)
	}
	return a.GetAppState()
}
