package main

import (
	"context"
	"errors"
	"strings"
	"time"

	"SlothClashDesktop/companion"

	wailsrt "github.com/wailsapp/wails/v2/pkg/runtime"
)

const companionAgentsEvent = "companion:agents"

// companionManager lazily creates the LAN companion controller (keychain-backed
// token store). Retries on failure (does not cache a failed init).
func (a *App) companionManager() (*companion.Manager, error) {
	a.companionMu.Lock()
	defer a.companionMu.Unlock()
	if a.companionMgr != nil {
		return a.companionMgr, nil
	}
	root, err := slothDataRoot()
	if err != nil {
		return nil, err
	}
	m, err := companion.NewManager(root, companion.NewKeyringTokenStore())
	if err != nil {
		return nil, err
	}
	a.companionMgr = m
	return m, nil
}

// CompanionDiscover browses the LAN once and returns the merged agent list.
func (a *App) CompanionDiscover() ([]companion.AgentInfo, error) {
	m, err := a.companionManager()
	if err != nil {
		return nil, err
	}
	ctx, cancel := context.WithTimeout(context.Background(), 4*time.Second)
	defer cancel()
	return m.Discover(ctx)
}

// CompanionListAgents returns the merged paired+discovered list without re-browsing.
func (a *App) CompanionListAgents() ([]companion.AgentInfo, error) {
	m, err := a.companionManager()
	if err != nil {
		return nil, err
	}
	return m.ListAgents(), nil
}

// CompanionPairByString pairs from a pasted clashctl-pair://… string.
func (a *App) CompanionPairByString(s string) (companion.AgentInfo, error) {
	m, err := a.companionManager()
	if err != nil {
		return companion.AgentInfo{}, err
	}
	return m.PairByString(s)
}

// CompanionPairByPin pairs with a discovered agent using its 6-digit PIN.
func (a *App) CompanionPairByPin(deviceID, pin string) (companion.AgentInfo, error) {
	m, err := a.companionManager()
	if err != nil {
		return companion.AgentInfo{}, err
	}
	ctx, cancel := context.WithTimeout(context.Background(), 12*time.Second)
	defer cancel()
	return m.PairByPin(ctx, deviceID, pin)
}

// CompanionStatus reads a paired agent's status.
func (a *App) CompanionStatus(deviceID string) (*companion.StatusView, error) {
	m, err := a.companionManager()
	if err != nil {
		return nil, err
	}
	ctx, cancel := context.WithTimeout(context.Background(), 8*time.Second)
	defer cancel()
	return m.Status(ctx, deviceID)
}

// CompanionPower sets a paired agent's power state ("on"/"off"/"toggle").
func (a *App) CompanionPower(deviceID, action string) (*companion.PowerResult, error) {
	m, err := a.companionManager()
	if err != nil {
		return nil, err
	}
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	return m.Power(ctx, deviceID, action)
}

// CompanionShareSubscription pushes this controller's active subscription URL to the agent.
func (a *App) CompanionShareSubscription(deviceID string) error {
	m, err := a.companionManager()
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
	m, err := a.companionManager()
	if err != nil {
		return err
	}
	ctx, cancel := context.WithTimeout(context.Background(), 8*time.Second)
	defer cancel()
	return m.Rename(ctx, deviceID, name)
}

// CompanionUnpair removes a paired agent and deletes its token.
func (a *App) CompanionUnpair(deviceID string) error {
	m, err := a.companionManager()
	if err != nil {
		return err
	}
	return m.Unpair(deviceID)
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
	a.companionMu.Lock()
	if a.companionWatchCancel != nil {
		a.companionMu.Unlock()
		return
	}
	ctx, cancel := context.WithCancel(context.Background())
	a.companionWatchCancel = cancel
	a.companionMu.Unlock()

	go func() {
		ticker := time.NewTicker(5 * time.Second)
		defer ticker.Stop()
		emit := func() {
			m, err := a.companionManager()
			if err != nil {
				return
			}
			bctx, bcancel := context.WithTimeout(ctx, 4*time.Second)
			agents, derr := m.Discover(bctx)
			bcancel()
			if derr == nil && a.ctx != nil {
				wailsrt.EventsEmit(a.ctx, companionAgentsEvent, agents)
			}
		}
		emit()
		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				emit()
			}
		}
	}()
}

// CompanionStopDiscovery stops the background browse loop.
func (a *App) CompanionStopDiscovery() {
	a.companionMu.Lock()
	defer a.companionMu.Unlock()
	if a.companionWatchCancel != nil {
		a.companionWatchCancel()
		a.companionWatchCancel = nil
	}
}
