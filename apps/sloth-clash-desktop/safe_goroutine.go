package main

import (
	"context"
	"fmt"
	"runtime/debug"
	"time"
)

// A panic in ANY goroutine terminates the whole process (Go semantics). For a
// tray-resident VPN client that means "the app vanished and the tunnel dropped"
// with no log. The helpers here contain background panics: they log (name +
// stack) and keep the app alive instead of crashing it (audit G1).

// safeGo runs fn in a new goroutine with panic recovery. Use for one-off
// background work (insight refresh, subscription refresh, event handlers).
func (a *App) safeGo(name string, fn func()) {
	go func() {
		defer recoverGoroutine(name)
		fn()
	}()
}

// runGuardedLoop runs a long-lived loop (supervisor, update-check,
// auto-update) with panic recovery AND auto-restart: a panic in one iteration
// is logged and the loop is restarted (after a short pause) so the self-healing
// machinery survives a transient hiccup instead of dying silently. Exits only
// when ctx is cancelled.
func (a *App) runGuardedLoop(ctx context.Context, name string, loop func(context.Context)) {
	for {
		func() {
			defer recoverGoroutine(name)
			loop(ctx) // normally blocks until ctx.Done()
		}()
		select {
		case <-ctx.Done():
			return
		default:
			// loop returned early (panic or unexpected) — pause, then restart.
			time.Sleep(1 * time.Second)
		}
	}
}

// recoverGoroutine is the shared deferred recover body.
func recoverGoroutine(name string) {
	if r := recover(); r != nil {
		debugLog("panic", "G1", name, "recovered background goroutine panic",
			map[string]any{
				"panic": fmt.Sprintf("%v", r),
				"stack": string(debug.Stack()),
			})
	}
}
