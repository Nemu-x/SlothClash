package main

import (
	"context"
	"errors"
	"strings"
	"time"

	"SlothClashDesktop/companion"

	wailsrt "github.com/wailsapp/wails/v2/pkg/runtime"
)

// Remote buttons of a paired agent (PROTOCOL.md §8.6–§8.19): one binding per
// button. The device page shows only the tabs whose capability the agent
// advertises; an error from the agent reads "[code] message".

const companionDeviceEvent = "companion:event"

// companionCall runs fn against the manager with a deadline.
func companionCall[T any](h *companionHost, timeout time.Duration, fn func(context.Context, *companion.Manager) (T, error)) (T, error) {
	m, err := h.manager()
	if err != nil {
		var zero T
		return zero, err
	}
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()
	return fn(ctx, m)
}

// companionDo is companionCall for buttons without a result.
func companionDo(h *companionHost, timeout time.Duration, fn func(context.Context, *companion.Manager) error) error {
	_, err := companionCall(h, timeout, func(ctx context.Context, m *companion.Manager) (struct{}, error) {
		return struct{}{}, fn(ctx, m)
	})
	return err
}

func (a *App) CompanionProxies(deviceID string) ([]companion.ProxyGroupView, error) {
	return companionCall(&a.companion, 10*time.Second, func(ctx context.Context, m *companion.Manager) ([]companion.ProxyGroupView, error) {
		return m.Proxies(ctx, deviceID)
	})
}

func (a *App) CompanionSelectProxy(deviceID, group, node string) (*companion.SelectResult, error) {
	return companionCall(&a.companion, 10*time.Second, func(ctx context.Context, m *companion.Manager) (*companion.SelectResult, error) {
		return m.SelectProxy(ctx, deviceID, group, node)
	})
}

func (a *App) CompanionUnfixProxy(deviceID, group string) (*companion.SelectResult, error) {
	return companionCall(&a.companion, 10*time.Second, func(ctx context.Context, m *companion.Manager) (*companion.SelectResult, error) {
		return m.UnfixProxy(ctx, deviceID, group)
	})
}

// CompanionTestDelay tests one node, or every node of the group when node is "".
func (a *App) CompanionTestDelay(deviceID, group, node string) ([]companion.DelayView, error) {
	return companionCall(&a.companion, 45*time.Second, func(ctx context.Context, m *companion.Manager) ([]companion.DelayView, error) {
		return m.TestDelay(ctx, deviceID, group, node)
	})
}

func (a *App) CompanionMode(deviceID string) (*companion.ModeView, error) {
	return companionCall(&a.companion, 10*time.Second, func(ctx context.Context, m *companion.Manager) (*companion.ModeView, error) {
		return m.Mode(ctx, deviceID)
	})
}

func (a *App) CompanionSetMode(deviceID, mode string) (string, error) {
	return companionCall(&a.companion, 10*time.Second, func(ctx context.Context, m *companion.Manager) (string, error) {
		return m.SetMode(ctx, deviceID, mode)
	})
}

func (a *App) CompanionProfiles(deviceID string) (*companion.ProfilesView, error) {
	return companionCall(&a.companion, 10*time.Second, func(ctx context.Context, m *companion.Manager) (*companion.ProfilesView, error) {
		return m.Profiles(ctx, deviceID)
	})
}

func (a *App) CompanionSelectProfile(deviceID, id string) (string, error) {
	return companionCall(&a.companion, 30*time.Second, func(ctx context.Context, m *companion.Manager) (string, error) {
		return m.SelectProfile(ctx, deviceID, id)
	})
}

// CompanionUpdateProfile refreshes one remote profile, or all when id is "".
func (a *App) CompanionUpdateProfile(deviceID, id string) error {
	return companionDo(&a.companion, 90*time.Second, func(ctx context.Context, m *companion.Manager) error {
		return m.UpdateProfile(ctx, deviceID, id)
	})
}

func (a *App) CompanionDeleteProfile(deviceID, id string) error {
	return companionDo(&a.companion, 15*time.Second, func(ctx context.Context, m *companion.Manager) error {
		return m.DeleteProfile(ctx, deviceID, id)
	})
}

func (a *App) CompanionProviders(deviceID string) ([]companion.ProviderView, error) {
	return companionCall(&a.companion, 10*time.Second, func(ctx context.Context, m *companion.Manager) ([]companion.ProviderView, error) {
		return m.Providers(ctx, deviceID)
	})
}

func (a *App) CompanionUpdateProvider(deviceID, kind, name string) error {
	return companionDo(&a.companion, 60*time.Second, func(ctx context.Context, m *companion.Manager) error {
		return m.UpdateProvider(ctx, deviceID, kind, name)
	})
}

func (a *App) CompanionHealthCheckProvider(deviceID, name string) error {
	return companionDo(&a.companion, 60*time.Second, func(ctx context.Context, m *companion.Manager) error {
		return m.HealthCheckProvider(ctx, deviceID, name)
	})
}

func (a *App) CompanionConnections(deviceID string) (*companion.ConnectionsView, error) {
	return companionCall(&a.companion, 10*time.Second, func(ctx context.Context, m *companion.Manager) (*companion.ConnectionsView, error) {
		return m.Connections(ctx, deviceID)
	})
}

// CompanionCloseConnections closes one connection, or all when id is "", and
// returns how many were closed.
func (a *App) CompanionCloseConnections(deviceID, id string) (int, error) {
	return companionCall(&a.companion, 10*time.Second, func(ctx context.Context, m *companion.Manager) (int, error) {
		return m.CloseConnections(ctx, deviceID, id)
	})
}

func (a *App) CompanionTraffic(deviceID string) (*companion.TrafficView, error) {
	return companionCall(&a.companion, 10*time.Second, func(ctx context.Context, m *companion.Manager) (*companion.TrafficView, error) {
		return m.Traffic(ctx, deviceID)
	})
}

func (a *App) CompanionLogs(deviceID, level string, limit int) ([]companion.LogLineView, error) {
	return companionCall(&a.companion, 10*time.Second, func(ctx context.Context, m *companion.Manager) ([]companion.LogLineView, error) {
		return m.Logs(ctx, deviceID, level, limit)
	})
}

func (a *App) CompanionSettings(deviceID string) ([]companion.SettingView, error) {
	return companionCall(&a.companion, 10*time.Second, func(ctx context.Context, m *companion.Manager) ([]companion.SettingView, error) {
		return m.Settings(ctx, deviceID)
	})
}

// CompanionSetSetting sends value as given: a bool for a bool setting, a string
// for an enum. The agent applies it the way its own settings screen does, and a
// failure there (no helper service, …) comes back as an error.
func (a *App) CompanionSetSetting(deviceID, key string, value any) (*companion.SettingView, error) {
	return companionCall(&a.companion, 30*time.Second, func(ctx context.Context, m *companion.Manager) (*companion.SettingView, error) {
		return m.SetSetting(ctx, deviceID, key, value)
	})
}

func (a *App) CompanionActions(deviceID string) ([]companion.ActionView, error) {
	return companionCall(&a.companion, 10*time.Second, func(ctx context.Context, m *companion.Manager) ([]companion.ActionView, error) {
		return m.Actions(ctx, deviceID)
	})
}

func (a *App) CompanionRunAction(deviceID, id string) error {
	return companionDo(&a.companion, 60*time.Second, func(ctx context.Context, m *companion.Manager) error {
		return m.RunAction(ctx, deviceID, id)
	})
}

// CompanionWatchDevice streams the agent's events while its page is open, as
// "companion:event" (companion.EventView): status for the header, changed
// {resource} to refetch that resource, traffic, logs, and the stream's own
// open / down / unauthorized. Reconnects with backoff; idempotent per device.
// The page stops it with CompanionUnwatchDevice when it closes.
func (a *App) CompanionWatchDevice(deviceID string) error {
	return a.companion.watchDevice(deviceID, a.safeGo, func(e companion.EventView) {
		if a.ctx != nil {
			wailsrt.EventsEmit(a.ctx, companionDeviceEvent, e)
		}
	})
}

// CompanionUnwatchDevice stops the event stream of a device page.
func (a *App) CompanionUnwatchDevice(deviceID string) {
	a.companion.unwatchDevice(deviceID)
}

// watchDevice starts the device's event stream on a goroutine launched by
// spawn, handing each event to emit. Idempotent per device.
func (h *companionHost) watchDevice(deviceID string, spawn func(name string, fn func()), emit func(companion.EventView)) error {
	deviceID = strings.TrimSpace(deviceID)
	if deviceID == "" {
		return errors.New("device id is required")
	}
	m, err := h.manager()
	if err != nil {
		return err
	}
	h.mu.Lock()
	if h.deviceWatch == nil {
		h.deviceWatch = map[string]context.CancelFunc{}
	}
	if _, running := h.deviceWatch[deviceID]; running {
		h.mu.Unlock()
		return nil
	}
	ctx, cancel := context.WithCancel(context.Background())
	h.deviceWatch[deviceID] = cancel
	h.mu.Unlock()

	spawn("companion-watch", func() {
		defer func() {
			// The watch ended by itself (token revoked): free the slot so the
			// page can start a new one. A cancelled watch was already removed.
			if ctx.Err() == nil {
				h.mu.Lock()
				delete(h.deviceWatch, deviceID)
				h.mu.Unlock()
			}
			cancel()
		}()
		m.Watch(ctx, deviceID, companion.WatchOptions{Traffic: true, Logs: true, LogLevel: "info"}, emit)
	})
	return nil
}

// unwatchDevice stops the event stream of a device page.
func (h *companionHost) unwatchDevice(deviceID string) {
	deviceID = strings.TrimSpace(deviceID)
	h.mu.Lock()
	defer h.mu.Unlock()
	if cancel := h.deviceWatch[deviceID]; cancel != nil {
		cancel()
		delete(h.deviceWatch, deviceID)
	}
}
