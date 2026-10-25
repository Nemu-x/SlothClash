package main

import (
	"context"
	"errors"
	"strings"
	"sync"
	"time"

	"SlothClashDesktop/companion"

	wailsrt "github.com/wailsapp/wails/v2/pkg/runtime"
)

const companionAgentsEvent = "companion:agents"

// companionHost owns the LAN companion controller state: the lazily created
// manager, the discovery loop and the per-device event streams. One mutex
// guards all of it; the Wails bindings on App delegate here.
type companionHost struct {
	mu          sync.Mutex
	mgr         *companion.Manager
	watchCancel context.CancelFunc
	// deviceWatch cancels the event stream of each device page that is
	// open (CompanionWatchDevice), keyed by deviceId.
	deviceWatch map[string]context.CancelFunc
}

// manager lazily creates the LAN companion controller (keychain-backed
// token store). Retries on failure (does not cache a failed init).
func (h *companionHost) manager() (*companion.Manager, error) {
	h.mu.Lock()
	defer h.mu.Unlock()
	if h.mgr != nil {
		return h.mgr, nil
	}
	root, err := slothDataRoot()
	if err != nil {
		return nil, err
	}
	m, err := companion.NewManager(root, companion.NewKeyringTokenStore())
	if err != nil {
		return nil, err
	}
	h.mgr = m
	return m, nil
}

// startDiscovery starts the background browse loop; emit receives each
// successful browse result. Idempotent while a loop is running.
func (h *companionHost) startDiscovery(emit func([]companion.AgentInfo)) {
	h.mu.Lock()
	if h.watchCancel != nil {
		h.mu.Unlock()
		return
	}
	ctx, cancel := context.WithCancel(context.Background())
	h.watchCancel = cancel
	h.mu.Unlock()

	go func() {
		ticker := time.NewTicker(5 * time.Second)
		defer ticker.Stop()
		browse := func() {
			m, err := h.manager()
			if err != nil {
				return
			}
			bctx, bcancel := context.WithTimeout(ctx, 4*time.Second)
			agents, derr := m.Discover(bctx)
			bcancel()
			if derr == nil {
				emit(agents)
			}
		}
		browse()
		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				browse()
			}
		}
	}()
}

// stopDiscovery stops the background browse loop.
func (h *companionHost) stopDiscovery() {
	h.mu.Lock()
	defer h.mu.Unlock()
	if h.watchCancel != nil {
		h.watchCancel()
		h.watchCancel = nil
	}
}

// CompanionDiscover browses the LAN once and returns the merged agent list.
func (a *App) CompanionDiscover() ([]companion.AgentInfo, error) {
	m, err := a.companion.manager()
	if err != nil {
		return nil, err
	}
	ctx, cancel := context.WithTimeout(context.Background(), 4*time.Second)
	defer cancel()
	return m.Discover(ctx)
}

// CompanionListAgents returns the merged paired+discovered list without re-browsing.
func (a *App) CompanionListAgents() ([]companion.AgentInfo, error) {
	m, err := a.companion.manager()
	if err != nil {
		return nil, err
	}
	return m.ListAgents(), nil
}

// CompanionPairByString pairs from a pasted clashctl-pair://… string.
func (a *App) CompanionPairByString(s string) (companion.AgentInfo, error) {
	m, err := a.companion.manager()
	if err != nil {
		return companion.AgentInfo{}, err
	}
	return m.PairByString(s)
}

// CompanionPairByPin pairs with a discovered agent using its 6-digit PIN.
func (a *App) CompanionPairByPin(deviceID, pin string) (companion.AgentInfo, error) {
	m, err := a.companion.manager()
	if err != nil {
		return companion.AgentInfo{}, err
	}
	ctx, cancel := context.WithTimeout(context.Background(), 12*time.Second)
	defer cancel()
	return m.PairByPin(ctx, deviceID, pin)
}

// CompanionStatus reads a paired agent's status.
func (a *App) CompanionStatus(deviceID string) (*companion.StatusView, error) {
	m, err := a.companion.manager()
	if err != nil {
		return nil, err
	}
	ctx, cancel := context.WithTimeout(context.Background(), 8*time.Second)
	defer cancel()
	return m.Status(ctx, deviceID)
}

// CompanionPower sets a paired agent's power state ("on"/"off"/"toggle").
func (a *App) CompanionPower(deviceID, action string) (*companion.PowerResult, error) {
	m, err := a.companion.manager()
	if err != nil {
		return nil, err
	}
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	return m.Power(ctx, deviceID, action)
}

// CompanionShareSubscription pushes this controller's active subscription URL to the agent.
func (a *App) CompanionShareSubscription(deviceID string) error {
	m, err := a.companion.manager()
	if err != nil {
		return err
	}
	url, name := a.activeSubscription()
	if strings.TrimSpace(url) == "" {
		return errors.New("no active subscription to share")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	return m.ShareSubscription(ctx, deviceID, url, name)
}

// CompanionRename renames a paired agent.
func (a *App) CompanionRename(deviceID, name string) error {
	m, err := a.companion.manager()
	if err != nil {
		return err
	}
	ctx, cancel := context.WithTimeout(context.Background(), 8*time.Second)
	defer cancel()
	return m.Rename(ctx, deviceID, name)
}

// CompanionUnpair removes a paired agent and deletes its token.
func (a *App) CompanionUnpair(deviceID string) error {
	a.CompanionUnwatchDevice(deviceID)
	m, err := a.companion.manager()
	if err != nil {
		return err
	}
	ctx, cancel := context.WithTimeout(context.Background(), 8*time.Second)
	defer cancel()
	// The agent is asked to revoke our token first; the local removal happens
	// even when it is unreachable (forgetting cannot be blocked by the other side).
	agentErr, err := m.Unpair(ctx, deviceID)
	if agentErr != nil {
		debugLog("companion", "CMP-UNPAIR", "companion_api.go:CompanionUnpair",
			"agent did not confirm the unpair; forgot it locally anyway",
			map[string]any{"error": agentErr.Error()})
	}
	return err
}

// activeSubscription returns the active profile's subscription URL + name.
func (a *App) activeSubscription() (string, string) {
	a.mu.RLock()
	defer a.mu.RUnlock()
	id := strings.TrimSpace(a.state.Profile.ActiveProfileID)
	for _, p := range a.profiles {
		if p.ID == id {
			return p.URL, p.Name
		}
	}
	return "", ""
}

// CompanionStartDiscovery starts a background browse loop that emits the
// "companion:agents" event with the live agent list, so the UI refreshes
// without frontend polling. Idempotent; stop with CompanionStopDiscovery.
func (a *App) CompanionStartDiscovery() {
	a.companion.startDiscovery(func(agents []companion.AgentInfo) {
		if a.ctx != nil {
			wailsrt.EventsEmit(a.ctx, companionAgentsEvent, agents)
		}
	})
}

// CompanionStopDiscovery stops the background browse loop.
func (a *App) CompanionStopDiscovery() {
	a.companion.stopDiscovery()
}
