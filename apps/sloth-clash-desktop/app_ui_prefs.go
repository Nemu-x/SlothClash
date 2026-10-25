package main

import (
	"errors"
	"os"
	"runtime"
	"strings"
	"time"

	wailsrt "github.com/wailsapp/wails/v2/pkg/runtime"
)

func trayEnabled() bool {
	if v := strings.TrimSpace(strings.ToLower(os.Getenv("SLOTH_DISABLE_TRAY"))); v == "1" || v == "true" || v == "yes" {
		return false
	}
	if v := strings.TrimSpace(strings.ToLower(os.Getenv("SLOTH_ENABLE_EXPERIMENTAL_TRAY"))); v != "" {
		return v == "1" || v == "true" || v == "yes"
	}
	// Enabled by default on platforms where we ship a tray backend
	// (Windows: fyne systray + bundled .ico; macOS: NSStatusBar via cgo).
	// Stub builds return false from trayBackendAvailable().
	return runtime.GOOS == "darwin" || runtime.GOOS == "windows"
}

func trayRuntimeEnabled() bool {
	return trayEnabled() && trayBackendAvailable()
}

func (a *App) GetTrayAvailability() bool {
	return trayRuntimeEnabled() && trayIsReady()
}

// NavigateUIScreen switches the web UI to a main navigation screen. Used by the
// native macOS menu bar tray; the frontend listens for `app:navigate`.
func (a *App) NavigateUIScreen(screen string) {
	screen = strings.ToLower(strings.TrimSpace(screen))
	switch screen {
	case "home", "proxies", "profiles", "connections", "rules", "logs", "advanced", "settings":
	default:
		return
	}
	a.mu.Lock()
	a.state.UI.ActiveScreen = screen
	a.state.UpdatedAt = time.Now().Unix()
	a.mu.Unlock()
	if a.ctx != nil {
		wailsrt.WindowShow(a.ctx)
		wailsrt.WindowUnminimise(a.ctx)
		wailsrt.EventsEmit(a.ctx, "app:navigate", map[string]string{"screen": screen})
	}
	a.emitAppStateChanged()
}

func (a *App) SetCloseToTrayPreference(enabled bool) AppState {
	a.mu.Lock()
	a.closeToTray = enabled
	a.state.UpdatedAt = time.Now().Unix()
	out := a.state
	a.mu.Unlock()
	return out
}

// SetLaunchOnStartupPreference registers (or clears) the current binary in
// the platform autostart store. On Windows this writes to
// HKCU\Software\Microsoft\Windows\CurrentVersion\Run; on other platforms
// the call returns an informative error. The toggle is persisted by the
// OS itself, so the app does not need to mirror state on disk.
func (a *App) SetLaunchOnStartupPreference(enabled bool) error {
	return setLaunchOnStartup(enabled)
}

// GetLaunchOnStartupPreference reflects what the OS autostart store has
// registered for the current executable. Returns false if the entry is
// missing, points at a different binary, or the store is unreadable.
func (a *App) GetLaunchOnStartupPreference() bool {
	v, _ := getLaunchOnStartup()
	return v
}

// GetPreferredLanguage returns installer/system preferred UI language for first-run.
func (a *App) GetPreferredLanguage() string {
	lang := strings.TrimSpace(detectPreferredLanguage())
	if lang == "ru" || lang == "zh" || lang == "en" {
		return lang
	}
	return ""
}

func (a *App) GetUpdateState() UpdateState {
	a.mu.RLock()
	defer a.mu.RUnlock()
	return a.update
}

func (a *App) SetUpdateChannel(channel string) (UpdateState, error) {
	a.mu.Lock()
	defer a.mu.Unlock()
	if channel != "stable" {
		return a.update, errors.New("invalid update channel")
	}
	a.update.Channel = channel
	return a.update, nil
}
