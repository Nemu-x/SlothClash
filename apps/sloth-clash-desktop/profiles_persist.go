package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
)

const slothProfilesFile = "profiles.json"

type profilesPersisted struct {
	ActiveProfileID   string    `json:"activeProfileId"`
	Profiles          []Profile `json:"profiles"`
	Traffic           string    `json:"traffic,omitempty"`
	Mode              string    `json:"mode,omitempty"`
	LastNonDirectMode string    `json:"lastNonDirectMode,omitempty"`
}

func profilesStorePath() (string, error) {
	root, err := slothDataRoot()
	if err != nil {
		return "", err
	}
	return filepath.Join(root, slothProfilesFile), nil
}

func (a *App) loadProfilesFromDisk() {
	p, err := profilesStorePath()
	if err != nil {
		return
	}
	b, err := os.ReadFile(p)
	if err != nil || len(strings.TrimSpace(string(b))) == 0 {
		return
	}
	var disk profilesPersisted
	if err := json.Unmarshal(b, &disk); err != nil {
		return
	}
	a.mu.Lock()
	defer a.mu.Unlock()
	if len(disk.Profiles) > 0 {
		a.profiles = disk.Profiles
		a.state.Profile.Profiles = disk.Profiles
	}
	if disk.ActiveProfileID != "" {
		a.state.Profile.ActiveProfileID = disk.ActiveProfileID
	}
	switch strings.ToLower(strings.TrimSpace(disk.Traffic)) {
	case "tun":
		a.state.Traffic = "tun"
	case "proxy":
		a.state.Traffic = "proxy"
	}
	switch strings.ToLower(strings.TrimSpace(disk.Mode)) {
	case "rule", "global", "direct":
		a.state.Mode.Current = strings.ToLower(strings.TrimSpace(disk.Mode))
	}
	switch strings.ToLower(strings.TrimSpace(disk.LastNonDirectMode)) {
	case "rule", "global":
		a.state.Mode.LastNonDirectMode = strings.ToLower(strings.TrimSpace(disk.LastNonDirectMode))
	}
	if a.state.Mode.Current == "direct" && strings.TrimSpace(a.state.Mode.LastNonDirectMode) == "" {
		a.state.Mode.LastNonDirectMode = "rule"
	}
	// Hydrate the active profile's sticky pick into ProxyState so the
	// UI and the auto-select routine can read it synchronously on next
	// connect — no waiting for /proxies, no heuristics. This is what
	// makes "today MainGroup → tomorrow MainGroup" work across restarts.
	activeID := strings.TrimSpace(a.state.Profile.ActiveProfileID)
	if activeID != "" {
		for i := range a.profiles {
			if a.profiles[i].ID == activeID {
				a.state.Proxy.LastGoodGroup = strings.TrimSpace(a.profiles[i].LastGoodGroup)
				break
			}
		}
	}
}

// persistProfilesLocked writes profiles.json. Caller must hold a.mu (write lock).
func (a *App) persistProfilesLocked() error {
	p, err := profilesStorePath()
	if err != nil {
		return err
	}
	root, err := slothDataRoot()
	if err != nil {
		return err
	}
	if err := os.MkdirAll(root, 0o755); err != nil {
		return err
	}
	disk := profilesPersisted{
		ActiveProfileID:   a.state.Profile.ActiveProfileID,
		Profiles:          a.profiles,
		Traffic:           a.state.Traffic,
		Mode:              a.state.Mode.Current,
		LastNonDirectMode: a.state.Mode.LastNonDirectMode,
	}
	b, err := json.MarshalIndent(disk, "", "  ")
	if err != nil {
		return err
	}
	return atomicWriteFile(p, b, 0o644)
}
