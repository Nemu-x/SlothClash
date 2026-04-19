package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
)

const slothProfilesFile = "profiles.json"

type profilesPersisted struct {
	ActiveProfileID string    `json:"activeProfileId"`
	Profiles        []Profile `json:"profiles"`
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
		ActiveProfileID: a.state.Profile.ActiveProfileID,
		Profiles:        a.profiles,
	}
	b, err := json.MarshalIndent(disk, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(p, b, 0o644)
}
