package main

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"time"

	"gopkg.in/yaml.v3"
)

// GetProfilePaths returns runtime directory paths for a profile id.
func (a *App) GetProfilePaths(profileID string) ProfilePaths {
	profileID = strings.TrimSpace(profileID)
	out := ProfilePaths{}
	if profileID == "" {
		return out
	}
	root, err := slothDataRoot()
	if err != nil {
		return out
	}
	dd := filepath.Join(root, "runtime", profileID)
	out.DataDir = dd
	out.ConfigPath = filepath.Join(dd, "config.yaml")
	return out
}

// ReadProfileConfig reads runtime/<id>/config.yaml when it exists.
func (a *App) ReadProfileConfig(profileID string) ProfileConfigPeek {
	profileID = strings.TrimSpace(profileID)
	if profileID == "" {
		return ProfileConfigPeek{LastError: "profile id is required"}
	}
	p := a.GetProfilePaths(profileID).ConfigPath
	if p == "" {
		return ProfileConfigPeek{LastError: "could not resolve config path"}
	}
	b, err := os.ReadFile(p)
	if err != nil {
		return ProfileConfigPeek{Path: p, LastError: err.Error()}
	}
	return ProfileConfigPeek{Path: p, Body: string(b)}
}

// WriteProfileConfig replaces config.yaml for a profile (must be valid YAML mapping).
func (a *App) WriteProfileConfig(profileID string, content string) (AppState, error) {
	profileID = strings.TrimSpace(profileID)
	if profileID == "" {
		return a.GetAppState(), errors.New("profile id is required")
	}
	content = strings.TrimSpace(content)
	if content == "" {
		return a.GetAppState(), errors.New("config body is empty")
	}
	var check map[string]any
	if err := yaml.Unmarshal([]byte(content), &check); err != nil {
		return a.GetAppState(), err
	}
	if len(check) == 0 {
		return a.GetAppState(), errors.New("config must be a non-empty YAML mapping")
	}

	p := a.GetProfilePaths(profileID).ConfigPath
	if p == "" {
		return a.GetAppState(), errors.New("could not resolve config path")
	}
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		return a.GetAppState(), err
	}
	if err := os.WriteFile(p, []byte(content), 0o644); err != nil {
		return a.GetAppState(), err
	}

	a.mu.Lock()
	found := false
	for i := range a.profiles {
		if a.profiles[i].ID == profileID {
			a.profiles[i].SkipAutoConfig = true
			a.state.Profile.Profiles = a.profiles
			found = true
			break
		}
	}
	if !found {
		a.mu.Unlock()
		return a.GetAppState(), errors.New("profile not found")
	}
	a.state.UpdatedAt = time.Now().Unix()
	active := a.state.Profile.ActiveProfileID == profileID
	connected := a.state.Connection.Status == "connected"
	persistErr := a.persistProfilesLocked()
	a.mu.Unlock()
	if persistErr != nil {
		return a.GetAppState(), persistErr
	}

	if active && connected {
		go a.reconnectActiveProfile()
	}
	a.emitAppStateChanged()
	return a.GetAppState(), nil
}

func (a *App) reconnectActiveProfile() {
	// Coalesce bursts of reconnect triggers (profile edit, traffic switch, auto-update).
	if !a.reconnectInFlight.CompareAndSwap(false, true) {
		a.reconnectQueued.Store(true)
		return
	}
	go func() {
		defer a.reconnectInFlight.Store(false)
		for {
			a.reconnectQueued.Store(false)
			time.Sleep(150 * time.Millisecond)
			a.Disconnect()
			genAfterDisconnect := a.connectGen.Load()
			time.Sleep(300 * time.Millisecond)
			// If connect generation changed after we disconnected, a newer explicit user action
			// happened (most often manual disconnect). Do not auto-reconnect that cycle.
			if a.connectGen.Load() != genAfterDisconnect {
				if !a.reconnectQueued.Load() {
					return
				}
				continue
			}
			_, _ = a.Connect()
			if !a.reconnectQueued.Load() {
				return
			}
		}
	}()
}
