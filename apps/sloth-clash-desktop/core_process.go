package main

import (
	"context"
	"os/exec"
	"sync"
)

// coreProcess groups the bookkeeping for the running Mihomo core (App.core).
//
// Locking (unchanged by the grouping; the struct has no lock of its own):
//   - every field except lifecycleMu is guarded by App.mu: write under
//     a.mu.Lock, read under a.mu.Lock or a.mu.RLock;
//   - lifecycleMu is not guarded by anything: it serializes the start/restart
//     paths (ensureCoreForProfileEx, forceRestartCoreForProfile) across
//     concurrent callers. Those must not be entered with App.mu held, so the
//     lock order is lifecycleMu -> App.mu.
type coreProcess struct {
	secret          string
	listen          string
	overPipe        bool // mihomo started by sloth_clash_service; stop goes via IPC. API still uses listen (TCP).
	cmd             *exec.Cmd
	cancel          context.CancelFunc
	stopIntentional bool
	procToken       uint64
	// activeProfileID is the profile the running core was started for.
	// In the reload model (v0.3+) Mihomo keeps running across Connect/Disconnect;
	// we only tear it down when the profile itself changes (ActivateProfile with a
	// different ID) or on app shutdown.
	activeProfileID string
	lifecycleMu     sync.Mutex // serializes ensureCoreForProfile across concurrent callers
}
