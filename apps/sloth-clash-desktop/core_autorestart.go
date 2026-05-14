package main

import (
	"fmt"
	"strings"
	"time"
)

// coreAutoRestartBackoffs is the exponential schedule we walk through when
// trying to recover from an unexpected Mihomo exit. Three attempts cover the
// common cases (transient port collision, OOM during a config push) without
// hammering the system if the failure is persistent. Values mirror what
// clash-verge-rev uses for its CoreManager respawn loop.
var coreAutoRestartBackoffs = []time.Duration{
	1 * time.Second,
	4 * time.Second,
	16 * time.Second,
}

// attemptCoreAutoRestart walks the backoff schedule and rebuilds the core for
// the profile that was active when the previous process died. It's called
// from the cmd.Wait goroutine in core_manager.go after `Connection.Status`
// has already been set to "error".
//
// It bails (no further attempts) when:
//   - the user clicked Disconnect (coreStopIntentional flips to true);
//   - the user activated a different profile (coreActiveProfileID rotates);
//   - the profile was deleted between attempts.
//
// On the first attempt we move Connection.Status from "error" to
// "reconnecting" so the UI shows what is happening; on success the normal
// connect flow flips it back to "connected".
func (a *App) attemptCoreAutoRestart(profileID, traffic string) {
	defer func() {
		if r := recover(); r != nil {
			a.traceEvent("core.autorestart.panic", "fail", 0, map[string]any{
				"profileId": profileID,
				"panic":     fmt.Sprintf("%v", r),
			})
		}
	}()

	if strings.TrimSpace(profileID) == "" {
		return
	}
	enableTun := traffic == "tun"

	for i, wait := range coreAutoRestartBackoffs {
		attempt := i + 1
		total := len(coreAutoRestartBackoffs)

		// Sleep BEFORE the attempt — the core just died, give the system a
		// moment to release ports / file locks before we hammer Start again.
		time.Sleep(wait)

		// Re-validate that an auto-restart is still appropriate. The user may
		// have clicked Disconnect, switched profile, or relaunched Connect
		// themselves while we were sleeping.
		a.mu.Lock()
		if a.coreStopIntentional {
			a.mu.Unlock()
			return
		}
		if a.state.Core.Running {
			// Something else (manual Connect, parallel restart) already
			// brought a core back. Stand down.
			a.mu.Unlock()
			return
		}
		activeID := strings.TrimSpace(a.state.Profile.ActiveProfileID)
		if activeID != profileID {
			a.mu.Unlock()
			return
		}
		// Locate the profile fresh — its template may have been edited.
		var active Profile
		found := false
		for _, p := range a.profiles {
			if p.ID == profileID {
				active = p
				found = true
				break
			}
		}
		if !found {
			a.mu.Unlock()
			return
		}
		a.state.Connection.Status = ConnReconnecting
		a.state.Connection.Health = ""
		a.state.Connection.LastError = fmt.Sprintf(
			"core died, auto-restart %d/%d", attempt, total,
		)
		a.state.UpdatedAt = time.Now().Unix()
		a.mu.Unlock()
		a.emitAppStateChanged()
		a.traceEvent("core.autorestart.attempt", "ok", 0, map[string]any{
			"profileId": profileID,
			"attempt":   attempt,
			"of":        total,
			"enableTun": enableTun,
		})

		// gen=0 — this restart is not tied to any Connect job; the helper
		// uses it only to abort if the user cancels (which we check above).
		if err := a.ensureCoreForProfile(active, 0, enableTun); err != nil {
			a.traceEvent("core.autorestart.ensure_failed", "fail", 0, map[string]any{
				"profileId": profileID,
				"attempt":   attempt,
				"error":     err.Error(),
			})
			continue
		}
		if err := a.applyRuntimeConfig(active, traffic, enableTun); err != nil {
			a.traceEvent("core.autorestart.apply_failed", "fail", 0, map[string]any{
				"profileId": profileID,
				"attempt":   attempt,
				"error":     err.Error(),
			})
			continue
		}

		// Success — flip status back to connected. Mirror what
		// finishConnectJobOK does so the UI sees a clean ready state.
		a.mu.Lock()
		a.state.Connection.Status = ConnConnected
		a.state.Connection.LastError = ""
		a.state.Connection.LastWarning = ""
		a.markConnectionReadyLocked()
		a.state.UpdatedAt = time.Now().Unix()
		a.mu.Unlock()
		a.emitAppStateChanged()
		a.traceEvent("core.autorestart.success", "ok", 0, map[string]any{
			"profileId": profileID,
			"attempt":   attempt,
		})
		return
	}

	// All attempts exhausted — leave the user a clear next step.
	a.mu.Lock()
	a.state.Connection.Status = ConnError
	a.state.Connection.LastError = fmt.Sprintf(
		"core exited and %d auto-restarts failed; click Connect to retry",
		len(coreAutoRestartBackoffs),
	)
	a.state.UpdatedAt = time.Now().Unix()
	a.mu.Unlock()
	a.emitAppStateChanged()
	a.traceEvent("core.autorestart.exhausted", "fail", 0, map[string]any{
		"profileId": profileID,
		"attempts":  len(coreAutoRestartBackoffs),
	})
}
