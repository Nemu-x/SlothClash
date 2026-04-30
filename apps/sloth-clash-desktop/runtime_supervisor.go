package main

import (
	"context"
	"fmt"
	"runtime"
	"strings"
	"time"
)

// runRuntimeSupervisorLoop performs bounded periodic checks: Windows system proxy
// reconcile (see maybeWindowsSysProxyReconcile) and a resume-style pass when the
// wall clock gap suggests the machine slept or the ticker was delayed.
func (a *App) runRuntimeSupervisorLoop(ctx context.Context) {
	ticker := time.NewTicker(45 * time.Second)
	defer ticker.Stop()
	lastTick := time.Now()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			gap := time.Since(lastTick)
			if gap > 75*time.Second {
				a.appendRuntimeDiag("network.resume", fmt.Sprintf("gap=%s", gap.Round(time.Second)))
				a.runNetworkResumePass()
			}
			lastTick = time.Now()
			a.maybeWindowsSysProxyReconcile()
		}
	}
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
