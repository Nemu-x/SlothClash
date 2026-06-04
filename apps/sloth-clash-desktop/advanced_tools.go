package main

import (
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"time"

	wailsrt "github.com/wailsapp/wails/v2/pkg/runtime"
)

// AdvancedPaths is what the Advanced screen renders in its "Open folder"
// card. Each entry resolves to an existing on-disk path (or an empty string
// when the path is not yet provisioned, e.g. before the first Connect).
type AdvancedPaths struct {
	DataRoot     string `json:"dataRoot"`
	RuntimeDir   string `json:"runtimeDir"`
	ProfilesJSON string `json:"profilesJson"`
	PrefsJSON    string `json:"prefsJson"`
	DebugLog     string `json:"debugLog"`
	ServiceLog   string `json:"serviceLog"`
	GeoDir       string `json:"geoDir"`
	ActiveConfig string `json:"activeConfig"`
}

// AdvancedGeoStatus is a compact view of what the bundled geo data looks
// like on disk. The Advanced UI uses this to show a "last refreshed"
// hint next to the "Re-extract" action so the user knows whether they
// need to re-extract after an app update.
type AdvancedGeoStatus struct {
	GeoIPPath     string `json:"geoIpPath"`
	GeoIPSize     int64  `json:"geoIpSize"`
	GeoIPModified int64  `json:"geoIpModified"`
	GeoSitePath   string `json:"geoSitePath"`
	GeoSiteSize   int64  `json:"geoSiteSize"`
	GeoSiteModified int64 `json:"geoSiteModified"`
}

// GetAdvancedPaths exposes the canonical paths the Advanced screen lets the
// user open in their file manager. We resolve everything lazily so the UI
// reflects the live state, not a snapshot taken at startup.
func (a *App) GetAdvancedPaths() AdvancedPaths {
	out := AdvancedPaths{}
	root, err := slothDataRoot()
	if err != nil {
		return out
	}
	out.DataRoot = root
	out.RuntimeDir = filepath.Join(root, "runtime")
	out.ProfilesJSON = filepath.Join(root, slothProfilesFile)
	out.PrefsJSON = filepath.Join(root, slothPrefsFile)
	out.DebugLog = filepath.Join(root, "debug-cb9690.log")
	out.ServiceLog = filepath.Join(root, "service.log")

	a.mu.RLock()
	activeID := strings.TrimSpace(a.state.Profile.ActiveProfileID)
	a.mu.RUnlock()
	if activeID != "" {
		dataDir := filepath.Join(out.RuntimeDir, activeID)
		// Geo data files now live at the workdir root (Verge-style), so
		// "Open geo dir" surfaces the profile's runtime folder directly —
		// the user sees geoip.dat / geosite.dat / Country.mmdb next to
		// config.yaml without an extra subdirectory hop.
		out.GeoDir = dataDir
		out.ActiveConfig = filepath.Join(dataDir, "config.yaml")
	}
	return out
}

// GetAdvancedGeoStatus reports the on-disk state of bundled geo data so the
// Advanced UI can surface a useful "Re-extract" affordance.
func (a *App) GetAdvancedGeoStatus() AdvancedGeoStatus {
	out := AdvancedGeoStatus{}
	a.mu.RLock()
	activeID := strings.TrimSpace(a.state.Profile.ActiveProfileID)
	a.mu.RUnlock()
	if activeID == "" {
		return out
	}
	root, err := slothDataRoot()
	if err != nil {
		return out
	}
	dataDir := filepath.Join(root, "runtime", activeID)
	if st, err := os.Stat(filepath.Join(dataDir, "geoip.dat")); err == nil {
		out.GeoIPPath = filepath.Join(dataDir, "geoip.dat")
		out.GeoIPSize = st.Size()
		out.GeoIPModified = st.ModTime().Unix()
	}
	if st, err := os.Stat(filepath.Join(dataDir, "geosite.dat")); err == nil {
		out.GeoSitePath = filepath.Join(dataDir, "geosite.dat")
		out.GeoSiteSize = st.Size()
		out.GeoSiteModified = st.ModTime().Unix()
	}
	return out
}

// OpenPathInExplorer opens the given on-disk path in the OS file manager.
// Used by the Advanced "Open folder" buttons. Empty / non-existent paths are
// silently ignored so the UI can stay declarative.
//
// For files, we open the containing directory and select the file when
// possible (Windows /select arg; macOS -R arg). Directories open directly.
func (a *App) OpenPathInExplorer(path string) error {
	_ = a
	p := strings.TrimSpace(path)
	if p == "" {
		return errors.New("path is empty")
	}
	info, err := os.Stat(p)
	if err != nil {
		// File may not exist yet (e.g. service.log on a fresh install). Fall
		// back to opening the parent directory if THAT exists, otherwise
		// give up — never spawn a shell on a non-existent target.
		dir := filepath.Dir(p)
		if di, derr := os.Stat(dir); derr == nil && di.IsDir() {
			return launchOpenDir(dir)
		}
		return fmt.Errorf("path not found: %s", p)
	}
	if info.IsDir() {
		return launchOpenDir(p)
	}
	return launchOpenFile(p)
}

func launchOpenDir(path string) error {
	switch runtime.GOOS {
	case "windows":
		// IMPORTANT: do NOT apply hideWindowSysProcAttr / CREATE_NO_WINDOW
		// here. That flag is for console child processes (sc.exe, mihomo)
		// where a flashing terminal would be ugly. For explorer.exe — a
		// GUI shell — CREATE_NO_WINDOW prevents the new Explorer window
		// from rendering at all, so the user clicks and nothing happens.
		// explorer.exe has no console, so leaving SysProcAttr unset is
		// safe and silent.
		//
		// Note: explorer.exe returns exit code 1 even on success — that's
		// documented Windows behavior and we don't care because cmd.Start()
		// does not wait for completion.
		return exec.Command("explorer.exe", path).Start()
	case "darwin":
		return exec.Command("open", path).Start()
	default:
		return exec.Command("xdg-open", path).Start()
	}
}

func launchOpenFile(path string) error {
	switch runtime.GOOS {
	case "windows":
		// `/select,` highlights the file in Explorer rather than executing
		// it. Per MSDN the separator after `/select` must be a comma and
		// the path must be passed as a single argument; exec.Command's
		// arg splitting handles that for us.
		// Same SysProcAttr rationale as launchOpenDir: never CREATE_NO_WINDOW
		// for a GUI process.
		return exec.Command("explorer.exe", "/select,"+path).Start()
	case "darwin":
		return exec.Command("open", "-R", path).Start()
	default:
		return exec.Command("xdg-open", filepath.Dir(path)).Start()
	}
}

// ReExtractBundledResources rewrites geo/ + Country.mmdb from the embedded
// bundle into the active profile's runtime directory. Used after an app
// update or when the user suspects the on-disk geo files have been
// corrupted / stale.
func (a *App) ReExtractBundledResources() error {
	a.mu.RLock()
	activeID := strings.TrimSpace(a.state.Profile.ActiveProfileID)
	a.mu.RUnlock()
	if activeID == "" {
		return errors.New("no active profile — activate one on Profiles first")
	}
	root, err := slothDataRoot()
	if err != nil {
		return err
	}
	dataDir := filepath.Join(root, "runtime", activeID)
	if err := a.reExtractGeoInDataDir(dataDir); err != nil {
		return err
	}
	a.appendRuntimeDiag("geo.reextract", "ok")
	return nil
}

// ResetSubscriptionCache deletes the cached subscription body for the active
// profile so the next Refresh forces a clean network fetch. Useful when a
// flaky subscription server returned a partial body that the pipeline keeps
// rejecting from cache.
func (a *App) ResetSubscriptionCache() error {
	a.mu.RLock()
	activeID := strings.TrimSpace(a.state.Profile.ActiveProfileID)
	activeType := ""
	for _, p := range a.profiles {
		if p.ID == activeID {
			activeType = p.Type
			break
		}
	}
	a.mu.RUnlock()
	if activeID == "" {
		return errors.New("no active profile")
	}
	// A local profile keeps its only copy of the config in the body cache —
	// there is no URL to re-fetch from, so wiping it would destroy the profile.
	if activeType == "local" {
		return errors.New("local profiles have no remote cache to reset")
	}
	root, err := slothDataRoot()
	if err != nil {
		return err
	}
	cachePath := filepath.Join(root, "runtime", activeID, "subscription.cache.yaml")
	if err := os.Remove(cachePath); err != nil && !os.IsNotExist(err) {
		return err
	}
	a.appendRuntimeDiag("subscription.cache.reset", "deleted")
	return nil
}

// RestartCore tears the running Mihomo process down and lets the next
// Connect (or the on-going boot-active-profile-in-background routine) spawn
// a fresh one. Behaves as a "factory reload" for users who suspect the
// running core's state has drifted from the YAML on disk.
func (a *App) RestartCore() error {
	a.mu.RLock()
	activeID := strings.TrimSpace(a.state.Profile.ActiveProfileID)
	wasConnected := a.state.Connection.Status == ConnConnected
	traffic := strings.TrimSpace(a.state.Traffic)
	a.mu.RUnlock()
	if activeID == "" {
		return errors.New("no active profile")
	}
	a.connectGen.Add(1) // invalidate any in-flight Connect job
	a.mu.Lock()
	a.stopCoreLocked()
	a.mu.Unlock()
	a.appendRuntimeDiag("core.restart.requested", fmt.Sprintf(
		"prevConnected=%v traffic=%s", wasConnected, traffic,
	))

	// If the user was connected, kick a fresh Connect so the restart is
	// observable as a brief flicker rather than landing in "disconnected".
	if wasConnected {
		time.Sleep(150 * time.Millisecond)
		go func() {
			_, _ = a.Connect()
		}()
	} else if ctx := a.ctx; ctx != nil {
		// Otherwise just nudge the UI so the user sees Core: stopped.
		go wailsrt.EventsEmit(ctx, "app:state")
	}
	return nil
}
