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
	// ModeBeforeDesyncOnly is the mode a Desync-only profile overrode with
	// Rule; it is restored when a profile with servers is active again.
	ModeBeforeDesyncOnly string `json:"modeBeforeDesyncOnly,omitempty"`
	// DesyncPerProfile marks that the legacy global Desync settings were
	// copied into the profiles (migrateLegacyDesyncLocked). Absent in files
	// written before settings were per profile.
	DesyncPerProfile bool `json:"desyncPerProfile,omitempty"`
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
		// No profiles: nothing for the legacy Desync settings to move into.
		a.mu.Lock()
		a.desyncPerProfile = true
		a.syncActiveDesyncLocked()
		a.mu.Unlock()
		return
	}
	var disk profilesPersisted
	if err := json.Unmarshal(b, &disk); err != nil {
		return
	}
	a.mu.Lock()
	defer a.mu.Unlock()
	// After the active profile is known: its settings become the live ones.
	defer a.syncActiveDesyncLocked()
	if len(disk.Profiles) > 0 {
		// A hand-edited Desync-only profile loses what it cannot carry.
		for i := range disk.Profiles {
			sanitizeDesyncOnlyProfile(&disk.Profiles[i])
		}
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
	switch m := strings.ToLower(strings.TrimSpace(disk.ModeBeforeDesyncOnly)); m {
	case "global", "direct":
		a.modeBeforeDesyncOnly = m
	}
	a.enforceDesyncOnlyModeLocked()
	a.desyncPerProfile = disk.DesyncPerProfile
	if !a.desyncPerProfile && a.migrateLegacyDesyncLocked() {
		// Best effort: on failure the copy stays in memory and is saved
		// with the next change; the legacy object stays until then.
		_ = a.persistProfilesLocked()
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

		ModeBeforeDesyncOnly: a.modeBeforeDesyncOnly,
		DesyncPerProfile:     a.desyncPerProfile,
	}
	b, err := json.MarshalIndent(disk, "", "  ")
	if err != nil {
		return err
	}
	if err := atomicWriteFile(p, b, 0o644); err != nil {
		return err
	}
	if a.desyncPerProfile {
		// The profiles on disk carry the legacy settings' copy now.
		dropLegacyDesyncPrefs()
	}
	return nil
}

// pruneOrphanRuntimeDirs deletes `runtime/<profile-id>/` directories that no
// longer belong to any profile. Profile deletion cannot always remove them
// immediately (on Windows the core may still hold `core.log` open), so they
// accumulate — one dir per deleted profile, each carrying config, providers,
// geo dats and logs. Runs at startup, before any core is booted.
//
// Only `profile-*` directories are considered; anything else under runtime/
// (e.g. `logs/`) is left alone. Best-effort: failures are ignored.
func (a *App) pruneOrphanRuntimeDirs() {
	root, err := slothDataRoot()
	if err != nil {
		return
	}
	a.mu.RLock()
	live := make(map[string]struct{}, len(a.profiles))
	for _, p := range a.profiles {
		live[p.ID] = struct{}{}
	}
	a.mu.RUnlock()

	if removed := pruneOrphanRuntimeDirsIn(filepath.Join(root, "runtime"), live); removed > 0 {
		a.traceEvent("runtime.prune", "ok", 0, map[string]any{"removed": removed})
	}
}

// pruneOrphanRuntimeDirsIn is the pure half: remove every `profile-*` dir under
// runtimeDir whose id is not in live. Returns how many were removed.
func pruneOrphanRuntimeDirsIn(runtimeDir string, live map[string]struct{}) int {
	entries, err := os.ReadDir(runtimeDir)
	if err != nil {
		return 0
	}
	removed := 0
	for _, e := range entries {
		if !e.IsDir() || !strings.HasPrefix(e.Name(), "profile-") {
			continue
		}
		if _, alive := live[e.Name()]; alive {
			continue
		}
		if err := os.RemoveAll(filepath.Join(runtimeDir, e.Name())); err == nil {
			removed++
		}
	}
	return removed
}
