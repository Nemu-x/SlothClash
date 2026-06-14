package main

import (
	"context"
	"fmt"
	"runtime"
	"strings"
	"sync/atomic"
	"time"
)

// coreHealthFailThreshold is how many CONSECUTIVE failed core health probes the
// watchdog tolerates before declaring the core dead and triggering recovery.
// At the supervisor's 45s cadence this means ~90s of unresponsiveness, which is
// comfortably longer than a heavy `PUT /configs?force=true` reload stall
// (capped at 30s), so a one-off reload hiccup self-resolves (the next probe
// succeeds and resets the counter) instead of causing a false restart.
const coreHealthFailThreshold = 2

type coreHealthAction int

const (
	coreHealthNoop     coreHealthAction = iota // healthy, or not monitoring
	coreHealthCountFail                        // probe failed but still under threshold
	coreHealthRestart                          // N consecutive fails — recover
)

// decideCoreHealth is the pure decision for the health watchdog: given whether
// we are monitoring (connected + endpoint present), the latest probe result,
// and the running consecutive-failure count, return the action and the new
// count. Pure (no I/O) so the restart trigger is unit-testable without a live
// core. A healthy probe or "not monitoring" always resets the counter.
func decideCoreHealth(monitoring, probeOK bool, consecutiveFails int) (coreHealthAction, int) {
	if !monitoring || probeOK {
		return coreHealthNoop, 0
	}
	n := consecutiveFails + 1
	if n >= coreHealthFailThreshold {
		return coreHealthRestart, 0
	}
	return coreHealthCountFail, n
}

// runRuntimeSupervisorLoop performs bounded periodic checks: Windows system proxy
// reconcile (see maybeWindowsSysProxyReconcile) and a resume-style pass when the
// wall clock gap suggests the machine slept or the ticker was delayed.
func (a *App) runRuntimeSupervisorLoop(ctx context.Context) {
	ticker := time.NewTicker(45 * time.Second)
	defer ticker.Stop()
	lastTick := time.Now()
	var coreFails int
	var restartInProgress atomic.Bool
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			gap := time.Since(lastTick)
			if gap > 75*time.Second {
				a.appendRuntimeDiag("network.resume", fmt.Sprintf("gap=%s", gap.Round(time.Second)))
				a.runNetworkResumePass()
				// A resume pass already probed/handled the core; don't double-count.
				coreFails = 0
			}
			lastTick = time.Now()
			a.maybeWindowsSysProxyReconcile()
			coreFails = a.runCoreHealthWatch(coreFails, &restartInProgress)
		}
	}
}

// runCoreHealthWatch is one supervisor-tick liveness check of the running core.
// While connected, it probes the controller (/version); after
// coreHealthFailThreshold CONSECUTIVE failures it declares the core dead and
// triggers the same bounded auto-restart used for an embedded-core crash. This
// closes the gap where a service-core death (Windows main path — mihomo in the
// SYSTEM service, no cmd.Wait) went undetected and unrecovered. Returns the
// updated consecutive-fail count.
func (a *App) runCoreHealthWatch(consecutiveFails int, restartInProgress *atomic.Bool) int {
	gen := a.connectGen.Load()
	a.mu.RLock()
	monitoring := strings.TrimSpace(a.state.Connection.Status) == ConnConnected &&
		!a.coreStopIntentional && a.state.Core.Running
	listen := a.effectiveCoreEndpointLocked()
	secret := a.coreSecret
	profileID := strings.TrimSpace(a.coreActiveProfileID)
	traffic := strings.TrimSpace(a.state.Traffic)
	a.mu.RUnlock()

	// Not monitoring, no endpoint, or a recovery already running → stand down and reset.
	if !monitoring || strings.TrimSpace(listen) == "" || restartInProgress.Load() {
		return 0
	}

	_, probeErr := fetchVersionAt(listen, secret)
	action, next := decideCoreHealth(true, probeErr == nil, consecutiveFails)
	if action != coreHealthRestart {
		return next
	}

	// Declare dead → recover. Bail if the session was superseded meanwhile.
	if a.connectGen.Load() != gen || profileID == "" {
		return 0
	}
	if !restartInProgress.CompareAndSwap(false, true) {
		return 0
	}
	// Mirror the embedded cmd.Wait death transition so attemptCoreAutoRestart
	// (which stands down if Core.Running is still true) actually proceeds.
	a.mu.Lock()
	a.state.Core.Running = false
	a.state.Core.Lifecycle = "degraded"
	a.state.Connection.Status = ConnError
	a.state.Connection.Health = ""
	a.state.Connection.LastError = "core stopped responding"
	a.state.Core.LastError = "health watchdog: core unresponsive"
	a.state.UpdatedAt = time.Now().Unix()
	a.mu.Unlock()
	a.emitAppStateChanged()
	a.traceEvent("core.health.dead", "fail", 0, map[string]any{"profileId": profileID})

	go func() {
		defer restartInProgress.Store(false)
		a.attemptCoreAutoRestart(profileID, traffic)
	}()
	return 0
}

func (a *App) runNetworkResumePass() {
	gen := a.connectGen.Load()
	a.mu.RLock()
	if strings.TrimSpace(a.state.Connection.Status) != "connected" {
		a.mu.RUnlock()
		return
	}
	listen := a.effectiveCoreEndpointLocked()
	secret := a.coreSecret
	traffic := strings.TrimSpace(a.state.Traffic)
	a.mu.RUnlock()
	if strings.TrimSpace(listen) == "" {
		return
	}
	_, err := fetchVersionAt(listen, secret)
	if err != nil {
		if a.connectGen.Load() != gen {
			return
		}
		a.mu.Lock()
		if a.connectGen.Load() == gen && a.state.Connection.Status == "connected" {
			a.markConnectionDegradedLocked("Controller unreachable after wake or network change: " + strings.TrimSpace(err.Error()))
		}
		a.mu.Unlock()
		a.emitAppStateChanged()
		return
	}
	go func() { _, _ = a.RefreshHomeInsight() }()
	if runtime.GOOS == "windows" && traffic == "proxy" {
		a.maybeWindowsSysProxyReconcile()
	}
}
