package main

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// App-facing surface for the per-profile JavaScript override: save, clear,
// preview, and the recorder that stores what the last generation did with it.

// ProfileScriptPreview is what the editor gets back from a dry run: the config
// this profile generates with the candidate script and without it, plus the
// script's own outcome. Generated entirely off the connect path — nothing here
// touches the live runtime config or the running core.
type ProfileScriptPreview struct {
	// WithScript is the generated YAML with the candidate script applied. On a
	// script failure it equals WithoutScript, which is exactly what a real
	// generation would have used.
	WithScript string `json:"withScript"`
	// WithoutScript is the same generation with no script at all — the baseline
	// to diff against.
	WithoutScript string `json:"withoutScript"`
	// Changed is false when the script made no difference to the output.
	Changed bool `json:"changed"`
	// Result carries applied/error/line/column and captured console output.
	Result scriptResult `json:"result"`
}

// SetProfileScriptOverride stores the profile's JavaScript override.
//
// Saving clears the previously recorded failure: the old error belongs to the
// old script, and leaving it on the profile would badge a script the user has
// already fixed.
func (a *App) SetProfileScriptOverride(profileID string, script string) (AppState, error) {
	profileID = strings.TrimSpace(profileID)
	if profileID == "" {
		return a.GetAppState(), errors.New("profile id is required")
	}
	if len(script) > maxProfileScriptBytes {
		return a.GetAppState(), fmt.Errorf("script is %d bytes, over the %d byte limit", len(script), maxProfileScriptBytes)
	}

	a.mu.Lock()
	found := false
	connected := a.state.Connection.Status == "connected"
	for i := range a.profiles {
		if a.profiles[i].ID != profileID {
			continue
		}
		found = true
		a.profiles[i].ScriptOverride = script
		a.profiles[i].ScriptError = ""
		a.profiles[i].ScriptErrorLine = 0
		a.profiles[i].ScriptErrorColumn = 0
		a.profiles[i].ScriptConsole = nil
		a.profiles[i].ScriptConsoleCut = false
		a.profiles[i].ScriptLastDurationMS = 0
		// Same as the other editors: an edited profile must be regenerated on
		// the next connect rather than reusing a hand-edited config.yaml.
		a.profiles[i].SkipAutoConfig = false
		a.profiles[i].LastUpdated = time.Now().Unix()
		break
	}
	if !found {
		a.mu.Unlock()
		return a.GetAppState(), errors.New("profile not found")
	}
	a.state.Profile.Profiles = a.profiles
	a.state.UpdatedAt = time.Now().Unix()
	active := a.state.Profile.ActiveProfileID == profileID
	if err := a.persistProfilesLocked(); err != nil {
		a.mu.Unlock()
		return a.GetAppState(), err
	}
	a.mu.Unlock()

	if active && connected {
		// A script change rewrites the whole config, so it must reach live
		// traffic the same way a rules change does.
		a.reconnectFlushConns.Store(true)
		go a.reconnectActiveProfile()
	}
	a.emitAppStateChanged()
	return a.GetAppState(), nil
}

// ClearProfileScriptOverride removes the script, restoring generation exactly as
// it was before any script was set.
func (a *App) ClearProfileScriptOverride(profileID string) (AppState, error) {
	return a.SetProfileScriptOverride(profileID, "")
}

// recordProfileScriptResult stores what a generation did with the script so the
// UI can badge the profile and show the reason without re-running anything.
// A no-op when no script ran, so un-scripted profiles never touch state.json.
func (a *App) recordProfileScriptResult(profileID string, res scriptResult) {
	if !res.Ran || strings.TrimSpace(profileID) == "" {
		return
	}
	a.mu.Lock()
	changed := false
	for i := range a.profiles {
		if a.profiles[i].ID != profileID {
			continue
		}
		p := &a.profiles[i]
		if p.ScriptError != res.Err || p.ScriptErrorLine != res.Line || p.ScriptErrorColumn != res.Column {
			changed = true
		}
		p.ScriptError = res.Err
		p.ScriptErrorLine = res.Line
		p.ScriptErrorColumn = res.Column
		p.ScriptConsole = res.Console
		p.ScriptConsoleCut = res.ConsoleTruncated
		p.ScriptLastDurationMS = res.DurationMS
		break
	}
	if !changed {
		a.mu.Unlock()
		return
	}
	a.state.Profile.Profiles = a.profiles
	a.state.UpdatedAt = time.Now().Unix()
	if err := a.persistProfilesLocked(); err != nil {
		debugLog("profiles", "JS-2", "profile_script_api.go:recordProfileScriptResult",
			"failed to persist script result", map[string]any{"error": err.Error()})
	}
	a.mu.Unlock()
	a.emitAppStateChanged()
}

// PreviewProfileScript runs a candidate script through the REAL generation
// pipeline twice — with and without it — and returns both documents.
//
// It never writes the profile's runtime config, never restarts the core and
// never changes the active connection: both runs happen in throwaway
// directories seeded with the profile's cached subscription body, so the
// preview is offline and free.
func (a *App) PreviewProfileScript(profileID string, script string) (ProfileScriptPreview, error) {
	profileID = strings.TrimSpace(profileID)
	if profileID == "" {
		return ProfileScriptPreview{}, errors.New("profile id is required")
	}
	if len(script) > maxProfileScriptBytes {
		return ProfileScriptPreview{}, fmt.Errorf("script is %d bytes, over the %d byte limit", len(script), maxProfileScriptBytes)
	}

	a.mu.Lock()
	var target Profile
	found := false
	for i := range a.profiles {
		if a.profiles[i].ID == profileID {
			target = a.profiles[i]
			found = true
			break
		}
	}
	a.mu.Unlock()
	if !found {
		return ProfileScriptPreview{}, errors.New("profile not found")
	}

	root, err := slothDataRoot()
	if err != nil {
		return ProfileScriptPreview{}, err
	}
	liveDir := filepath.Join(root, "runtime", profileID)

	// Same ports/secret for both runs, so the diff shows the script's work and
	// not the random port we would otherwise pick twice.
	const previewMixedPort = 7890
	const previewSecret = "preview"

	withoutYAML, _, err := a.generatePreviewConfig(liveDir, target, "", previewMixedPort, previewSecret)
	if err != nil {
		return ProfileScriptPreview{}, err
	}
	withYAML, res, err := a.generatePreviewConfig(liveDir, target, script, previewMixedPort, previewSecret)
	if err != nil {
		return ProfileScriptPreview{}, err
	}

	return ProfileScriptPreview{
		WithScript:    withYAML,
		WithoutScript: withoutYAML,
		Changed:       withYAML != withoutYAML,
		Result:        res,
	}, nil
}

// generatePreviewConfig runs one generation into a throwaway directory seeded
// with the profile's cached subscription body and returns the YAML it produced.
func (a *App) generatePreviewConfig(liveDir string, profile Profile, script string, mixedPort int, secret string) (string, scriptResult, error) {
	tmpDir, err := os.MkdirTemp("", "sloth-script-preview-*")
	if err != nil {
		return "", scriptResult{}, err
	}
	defer os.RemoveAll(tmpDir)

	// Seed the cache so generation is served from disk instead of the network.
	if cached := readSubscriptionBodyCache(liveDir); len(strings.TrimSpace(string(cached))) > 0 {
		if err := atomicWriteFile(subscriptionBodyCachePath(tmpDir), cached, 0o644); err != nil {
			return "", scriptResult{}, err
		}
	}

	var res scriptResult
	// tun.enable=false and no external controller: a preview is a document, not
	// a session. The invariants that depend on those are asserted by the
	// pipeline tests, not by eyeballing a preview.
	if err := a.writeRuntimeConfigWithScript(
		tmpDir,
		profile.URL,
		profile.AgeSecretKey,
		profile.MergeTemplate,
		profile.ProxyTemplate,
		profile.RulesTemplate,
		script,
		0,
		mixedPort,
		secret,
		"proxy",
		false,
		false,
		&res,
	); err != nil {
		return "", res, err
	}

	out, err := os.ReadFile(filepath.Join(tmpDir, "config.yaml"))
	if err != nil {
		return "", res, err
	}
	return string(out), res, nil
}
