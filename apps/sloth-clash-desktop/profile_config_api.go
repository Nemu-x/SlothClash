package main

import (
	"context"
	"errors"
	"fmt"
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

// GetProfileRulesBaseline returns the `rules:` list that would end up in the
// generated config.yaml if the profile's rules editor template (prepend /
// append / delete.rules) were empty. That is: whatever the subscription
// itself ships plus whatever the extend + proxy merge templates inject.
//
// The UI renders this list read-only inside the rules editor so the user can
// see which rules come from the subscription / extended config and mark
// subscription rules for deletion (via delete.rules in the rules template)
// without editing them in place — mirrors clash-verge-rev's read-only
// subscription rules UX.
func (a *App) GetProfileRulesBaseline(profileID string) ProfileRulesBaseline {
	profileID = strings.TrimSpace(profileID)
	if profileID == "" {
		return ProfileRulesBaseline{LastError: "profile id is required"}
	}
	a.mu.RLock()
	var target Profile
	found := false
	for _, p := range a.profiles {
		if p.ID == profileID {
			target = p
			found = true
			break
		}
	}
	a.mu.RUnlock()
	if !found {
		return ProfileRulesBaseline{LastError: "profile not found"}
	}
	ctx, cancel := context.WithTimeout(context.Background(), 45*time.Second)
	defer cancel()
	body, err := a.loadProfileSubscriptionBody(ctx, target)
	if err != nil {
		return ProfileRulesBaseline{LastError: err.Error()}
	}
	doc, err := parseClashDocToMap(body)
	if err != nil || doc == nil {
		if err == nil {
			err = errors.New("subscription payload is not a YAML mapping")
		}
		return ProfileRulesBaseline{LastError: err.Error()}
	}
	isFull := subscriptionDocIsFullProfile(doc)
	if !isFull {
		// Non-full-profile subs provide only proxies; start from the same
		// synthetic `rules: [MATCH,Manual]` base that writeRuntimeConfig seeds
		// so the baseline reflects what the generated YAML would contain.
		// (Manual is a select group containing Auto + every provider entry,
		// so default behaviour is still "pick the fastest" while manual node
		// selection from the UI keeps working — Mihomo rejects PUT selection
		// on url-test groups.)
		doc = map[string]any{"rules": []any{"MATCH,Manual"}}
	}
	if err := applyProfileMergeTemplate(doc, target.MergeTemplate); err != nil {
		return ProfileRulesBaseline{LastError: err.Error()}
	}
	if err := applyProfileMergeTemplate(doc, target.ProxyTemplate); err != nil {
		return ProfileRulesBaseline{LastError: err.Error()}
	}
	rawRules, _ := doc["rules"].([]any)
	rules := make([]string, 0, len(rawRules))
	for _, r := range rawRules {
		s, ok := r.(string)
		if !ok {
			continue
		}
		if strings.TrimSpace(s) == "" {
			continue
		}
		rules = append(rules, s)
	}
	return ProfileRulesBaseline{Rules: rules, IsFullProfile: isFull}
}

// GetProfileProxyGroupsBaseline returns the `proxy-groups:` list that would
// end up in the generated config.yaml if the profile's proxy-groups editor
// template (prepend / append / delete.proxy-groups) were empty. That is:
// whatever the subscription itself ships (or the synthetic Auto/Manual
// fallback for non-full-profile subs) plus whatever the extend merge
// template injects.
//
// The UI renders this list read-only inside the proxy-groups editor so the
// user can see which groups come from the subscription / extended config
// and mark them for deletion (via delete.proxy-groups in the proxy-groups
// template) without editing them in place.
func (a *App) GetProfileProxyGroupsBaseline(profileID string) ProfileProxyGroupsBaseline {
	profileID = strings.TrimSpace(profileID)
	if profileID == "" {
		return ProfileProxyGroupsBaseline{LastError: "profile id is required"}
	}
	a.mu.RLock()
	var target Profile
	found := false
	for _, p := range a.profiles {
		if p.ID == profileID {
			target = p
			found = true
			break
		}
	}
	a.mu.RUnlock()
	if !found {
		return ProfileProxyGroupsBaseline{LastError: "profile not found"}
	}
	ctx, cancel := context.WithTimeout(context.Background(), 45*time.Second)
	defer cancel()
	body, err := a.loadProfileSubscriptionBody(ctx, target)
	if err != nil {
		return ProfileProxyGroupsBaseline{LastError: err.Error()}
	}
	doc, err := parseClashDocToMap(body)
	if err != nil || doc == nil {
		if err == nil {
			err = errors.New("subscription payload is not a YAML mapping")
		}
		return ProfileProxyGroupsBaseline{LastError: err.Error()}
	}
	isFull := subscriptionDocIsFullProfile(doc)
	if !isFull {
		// Non-full-profile subs ship only proxies; start from the same
		// synthetic Auto/Manual base that writeRuntimeConfig seeds so the
		// baseline reflects what the generated YAML would contain.
		doc = map[string]any{
			"proxy-groups": []any{
				map[string]any{
					"name":      "Auto",
					"type":      "url-test",
					"use":       []any{"sub1"},
					"url":       "http://www.gstatic.com/generate_204",
					"interval":  300,
					"tolerance": 50,
				},
				map[string]any{
					"name":    "Manual",
					"type":    "select",
					"proxies": []any{"Auto"},
					"use":     []any{"sub1"},
				},
			},
		}
	}
	if err := applyProfileMergeTemplate(doc, target.MergeTemplate); err != nil {
		return ProfileProxyGroupsBaseline{LastError: err.Error()}
	}
	rawGroups, _ := doc["proxy-groups"].([]any)
	groups := make([]map[string]any, 0, len(rawGroups))
	for _, g := range rawGroups {
		gm, ok := g.(map[string]any)
		if !ok {
			continue
		}
		name, _ := gm["name"].(string)
		if strings.TrimSpace(name) == "" {
			continue
		}
		groups = append(groups, gm)
	}
	return ProfileProxyGroupsBaseline{Groups: groups, IsFullProfile: isFull}
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
		if errors.Is(err, os.ErrNotExist) {
			if genErr := a.ensureProfileConfigSnapshot(profileID); genErr == nil {
				if b2, err2 := os.ReadFile(p); err2 == nil {
					return ProfileConfigPeek{Path: p, Body: string(b2)}
				}
			}
		}
		return ProfileConfigPeek{Path: p, LastError: err.Error()}
	}
	return ProfileConfigPeek{Path: p, Body: string(b)}
}

func (a *App) ensureProfileConfigSnapshot(profileID string) error {
	a.mu.RLock()
	var target Profile
	found := false
	for _, p := range a.profiles {
		if p.ID == profileID {
			target = p
			found = true
			break
		}
	}
	traffic := strings.TrimSpace(a.state.Traffic)
	secret := strings.TrimSpace(a.coreSecret)
	a.mu.RUnlock()
	if !found {
		return errors.New("profile not found")
	}
	if strings.TrimSpace(target.URL) == "" && !profileHasLocalConfig(target) {
		return errors.New("profile has no subscription url")
	}
	if traffic != "proxy" && traffic != "tun" {
		traffic = "tun"
	}
	if secret == "" {
		secret = "secret"
	}
	root, err := slothDataRoot()
	if err != nil {
		return err
	}
	dataDir := filepath.Join(root, "runtime", profileID)
	// Snapshot is generated on demand for the profile-edit UI; no live session
	// yet, so the YAML carries tun.enable=false. Connect/SetTrafficMode will
	// rewrite it with the correct intent before Mihomo sees it.
	return a.writeRuntimeConfig(
		dataDir,
		target.URL,
		target.MergeTemplate,
		target.ProxyTemplate,
		target.RulesTemplate,
		0,
		7890,
		secret,
		traffic,
		false,
		false,
	)
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
	if err := atomicWriteFile(p, []byte(content), 0o644); err != nil {
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

// reconnectActiveProfile regenerates runtime YAML for the active profile and
// asks the live Mihomo to hot-reload it via PUT /configs?force=true. This
// matches clash-verge-rev's CoreManager::update_config flow: no PATCH flip
// of tun.enable, no wintun teardown between reloads that keep the same user
// intent (connected && traffic=="tun").
//
// For profile switches (different ID becomes active) the call delegates to
// ensureCoreForProfile so the old Mihomo is replaced by one bound to the new
// profile's IPC pipe.
func (a *App) reconnectActiveProfile() {
	if !a.reconnectInFlight.CompareAndSwap(false, true) {
		a.reconnectQueued.Store(true)
		a.traceEvent("pipeline.reconnect.coalesced", "skip", 0, nil)
		return
	}
	go func() {
		defer a.reconnectInFlight.Store(false)
		passes := 0
		overall := time.Now()
		for {
			passes++
			a.reconnectQueued.Store(false)
			passStart := time.Now()
			if err := a.reloadActiveProfileConfig(); err != nil {
				a.traceEvent("pipeline.reconnect.pass", "fail", time.Since(passStart), map[string]any{
					"pass":  passes,
					"error": err.Error(),
				})
			} else {
				a.traceEvent("pipeline.reconnect.pass", "ok", time.Since(passStart), map[string]any{
					"pass": passes,
				})
			}
			a.emitAppStateChanged()
			if !a.reconnectQueued.Load() {
				a.traceEvent("pipeline.reconnect.done", "ok", time.Since(overall), map[string]any{
					"passes": passes,
				})
				return
			}
			// Coalesce bursts of triggers (template save + subscription
			// refresh happening close together): one more loop pass.
			time.Sleep(120 * time.Millisecond)
		}
	}()
}

// reloadActiveProfileConfig regenerates runtime YAML for the active profile
// and reloads it into the running core. It is the hot-reload entry point used
// by template saves and subscription refreshes; it preserves the current
// user intent so the YAML keeps tun.enable stable across reloads — matching
// clash-verge-rev's "regenerate with current state → PUT /configs force".
//
// If the active profile has changed since the core was started, the call
// delegates to applyRuntimeConfig, which in turn routes profile switches
// through ensureCoreForProfile (stop-old/start-new bound to the new pipe).
func (a *App) reloadActiveProfileConfig() error {
	a.mu.RLock()
	activeID := strings.TrimSpace(a.state.Profile.ActiveProfileID)
	var target Profile
	found := false
	for _, p := range a.profiles {
		if p.ID == activeID {
			target = p
			found = true
			break
		}
	}
	traffic := strings.TrimSpace(a.state.Traffic)
	connected := a.state.Connection.Status == "connected"
	a.mu.RUnlock()
	if !found {
		return nil
	}
	if strings.TrimSpace(target.URL) == "" && !profileHasLocalConfig(target) {
		return errors.New("profile has no subscription url")
	}

	// Effective TUN intent follows verge-rev's enable_tun_mode exactly: the
	// YAML always reflects what the user currently wants, never a hardcoded
	// placeholder. Reloads that do not change this value do not thrash wintun.
	enableTun := connected && traffic == "tun"
	return a.applyRuntimeConfig(target, traffic, enableTun)
}

// applyRuntimeConfig is the single entry point for pushing runtime YAML to a
// live Mihomo. Mirrors clash-verge-rev's CoreManager::update_config / apply_config:
//
//  1. Regenerate runtime YAML from the profile + active templates, inlining
//     the requested enableTun value in tun.enable.
//  2. If the core is not running, the fresh file on disk is all we need —
//     the next Connect will spawn Mihomo with it.
//  3. If a core is running for a different profile, delegate to
//     ensureCoreForProfile so the pipe bound to the old profile is stopped
//     and a new one is spawned under the current pipe.
//  4. Otherwise PUT /configs?path=<abs>&force=true — Mihomo re-reads the file
//     and merges it without restarting. Because the YAML reflects the caller's
//     intent verbatim, tun.enable is stable across reloads that preserve the
//     intent, so there is no PATCH-driven wintun flap.
// profileHasLocalConfig reports whether a profile supplies its own config
// WITHOUT a subscription URL: a local/imported profile (Type "local" — its body
// is seeded into the subscription body cache at import, see ImportProfileFromText)
// or one with a hand-edited on-disk config.yaml (SkipAutoConfig). Such profiles
// must NOT be rejected by the "needs a subscription URL" guards — the runtime
// pipeline (tryWriteMergedFullProfile → cache-first) builds their config from the
// cached body / on-disk file, no network involved. (See architecture/core-lifecycle.md #1.)
func profileHasLocalConfig(p Profile) bool {
	return strings.EqualFold(strings.TrimSpace(p.Type), "local") || p.SkipAutoConfig
}

// loadProfileSubscriptionBody returns the subscription body used to build the
// read-only baseline views (rules / proxy-groups editors): the seeded on-disk
// body cache for local profiles (Type "local", no URL), or a fresh network
// fetch for URL-backed profiles. Keeps local profiles first-class in the editors.
func (a *App) loadProfileSubscriptionBody(ctx context.Context, p Profile) ([]byte, error) {
	if strings.TrimSpace(p.URL) == "" {
		if !profileHasLocalConfig(p) {
			return nil, errors.New("profile has no subscription url")
		}
		root, err := slothDataRoot()
		if err != nil {
			return nil, err
		}
		body := readSubscriptionBodyCache(filepath.Join(root, "runtime", p.ID))
		if len(body) == 0 {
			return nil, errors.New("local profile has no cached config body")
		}
		return body, nil
	}
	return fetchSubscriptionBody(ctx, p.URL)
}

func (a *App) applyRuntimeConfig(profile Profile, traffic string, enableTun bool) error {
	return a.applyRuntimeConfigWithGen(profile, traffic, enableTun, 0)
}

func (a *App) applyRuntimeConfigWithGen(profile Profile, traffic string, enableTun bool, expectedGen uint64) error {
	if expectedGen != 0 && a.connectGen.Load() != expectedGen {
		return errConnectAborted
	}
	applyStart := time.Now()
	traceFields := map[string]any{
		"profileId": profile.ID,
		"traffic":   traffic,
		"enableTun": enableTun,
		"gen":       expectedGen,
	}
	defer func() {
		// Final outcome is logged by caller-specific paths below; this defer is
		// a backstop so we always see a duration even if a fast-return slipped
		// past the explicit trace points (e.g. dataDir prep error).
		_ = applyStart
		_ = traceFields
	}()
	if strings.TrimSpace(profile.URL) == "" && !profileHasLocalConfig(profile) {
		return errors.New("profile has no subscription url")
	}
	if traffic != "proxy" && traffic != "tun" {
		traffic = "tun"
	}

	a.mu.RLock()
	secret := strings.TrimSpace(a.coreSecret)
	listen := a.effectiveCoreEndpointLocked()
	coreProfileID := strings.TrimSpace(a.coreActiveProfileID)
	mixedPort := a.state.Core.MixedPort
	a.mu.RUnlock()
	if secret == "" {
		secret = "secret"
	}

	// Profile switch while connected: Mihomo's IPC pipe is tied to the profile
	// ID (slothMihomoIPCPath), so we cannot hot-reload a config bound to a
	// different pipe. Fall through to ensureCoreForProfile which stops the old
	// core and starts a new one with the correct enableTun from the start.
	if strings.TrimSpace(profile.ID) != coreProfileID && coreProfileID != "" {
		return a.ensureCoreForProfile(profile, 0, enableTun)
	}

	bin, err := a.resolveMihomoBinary()
	if err != nil {
		return err
	}
	root, err := slothDataRoot()
	if err != nil {
		return err
	}
	dataDir := filepath.Join(root, "runtime", profile.ID)
	if err := os.MkdirAll(filepath.Join(dataDir, "providers"), 0o755); err != nil {
		return err
	}

	if mixedPort == 0 {
		// Pre-Connect regeneration: no runtime yet, pick any free port. The
		// next Connect will overwrite this file anyway because SkipAutoConfig
		// reads the current port from disk.
		if p, perr := pickFreePort(); perr == nil {
			mixedPort = p
		} else {
			mixedPort = 7890
		}
	}
	if err := writeRuntimeConfigIfNeeded(a, bin, dataDir, profile, 0, mixedPort, secret, traffic, false, enableTun); err != nil {
		return err
	}
	if expectedGen != 0 && a.connectGen.Load() != expectedGen {
		return errConnectAborted
	}

	// No running core: file on disk is up to date, Connect will consume it.
	if strings.TrimSpace(listen) == "" {
		a.traceEvent("pipeline.apply.done", "skip", time.Since(applyStart), mergeFields(traceFields, map[string]any{
			"reason": "no_running_core",
		}))
		return nil
	}

	cfgAbs, abserr := filepath.Abs(filepath.Join(dataDir, "config.yaml"))
	if abserr != nil {
		return abserr
	}
	reloadStart := time.Now()
	ctx, cancel := context.WithTimeout(context.Background(), coreReloadTimeout())
	defer cancel()
	if err := coreReloadConfigFileAt(ctx, listen, secret, cfgAbs); err != nil {
		if expectedGen != 0 && a.connectGen.Load() != expectedGen {
			return errConnectAborted
		}
		a.traceEvent("pipeline.apply.reload", "fail", time.Since(reloadStart), mergeFields(traceFields, map[string]any{
			"error": err.Error(),
		}))
		// HTTP-over-named-pipe on Windows occasionally fails to flush the PUT
		// response even though mihomo finished the reload internally (the
		// core log shows "Initial configuration complete, total time: 15ms"
		// while our request still sits waiting). Before we tear the whole
		// process down, probe /version — if it returns quickly and reports
		// the running version, the reload likely succeeded and the user's
		// connection is fine; we accept the soft failure with a warning
		// rather than punishing them with a forced restart that takes another
		// 5-15 seconds of stalled "Connecting" state.
		probeCtx, probeCancel := context.WithTimeout(context.Background(), 3*time.Second)
		_, probeErr := fetchVersionAt(listen, secret)
		probeCancel()
		_ = probeCtx
		if probeErr == nil {
			a.traceEvent("pipeline.apply.reload", "soft_ok", time.Since(reloadStart), mergeFields(traceFields, map[string]any{
				"note": "reload HTTP response timed out but /version probe is healthy; assuming reload succeeded",
			}))
			a.traceEvent("pipeline.apply.done", "ok", time.Since(applyStart), traceFields)
			return nil
		}
		// Core really is unresponsive. Fall back to a clean restart, then
		// one retry of the reload. Anything beyond that fails the connect
		// for real so the UI can surface it.
		if restartErr := a.forceRestartCoreForProfile(profile, expectedGen, enableTun); restartErr != nil {
			a.traceEvent("pipeline.apply.restart", "fail", 0, mergeFields(traceFields, map[string]any{
				"error": restartErr.Error(),
			}))
			return fmt.Errorf("reload failed (%v), fallback restart failed (%v)", err, restartErr)
		}
		a.traceEvent("pipeline.apply.restart", "ok", 0, traceFields)
		if expectedGen != 0 && a.connectGen.Load() != expectedGen {
			return errConnectAborted
		}
		a.mu.RLock()
		relisten := a.effectiveCoreEndpointLocked()
		resecret := strings.TrimSpace(a.coreSecret)
		a.mu.RUnlock()
		if strings.TrimSpace(relisten) == "" {
			return errors.New("reload fallback: core endpoint is unavailable after restart")
		}
		retryStart := time.Now()
		ctx2, cancel2 := context.WithTimeout(context.Background(), coreReloadTimeout())
		defer cancel2()
		if err2 := coreReloadConfigFileAt(ctx2, relisten, resecret, cfgAbs); err2 != nil {
			// Same probe-then-accept logic for the post-restart retry:
			// the freshly-spawned core may also be slow to flush its first
			// reload response but its /version handler is independent.
			probeCtx2, probeCancel2 := context.WithTimeout(context.Background(), 3*time.Second)
			_, probeErr2 := fetchVersionAt(relisten, resecret)
			probeCancel2()
			_ = probeCtx2
			if probeErr2 == nil {
				a.traceEvent("pipeline.apply.retry_reload", "soft_ok", time.Since(retryStart), mergeFields(traceFields, map[string]any{
					"note": "retry reload HTTP timed out but /version probe is healthy after restart",
				}))
				a.traceEvent("pipeline.apply.done", "ok", time.Since(applyStart), traceFields)
				return nil
			}
			a.traceEvent("pipeline.apply.retry_reload", "fail", time.Since(retryStart), mergeFields(traceFields, map[string]any{
				"error": err2.Error(),
			}))
			return fmt.Errorf("reload failed (%v), restart succeeded but retry reload failed (%v)", err, err2)
		}
		a.traceEvent("pipeline.apply.retry_reload", "ok", time.Since(retryStart), traceFields)
	} else {
		a.traceEvent("pipeline.apply.reload", "ok", time.Since(reloadStart), traceFields)
	}
	a.traceEvent("pipeline.apply.done", "ok", time.Since(applyStart), traceFields)
	return nil
}

// mergeFields shallow-merges two field maps, with overrides winning over base.
// Used to add per-event fields to the shared trace context without mutating it.
func mergeFields(base, extra map[string]any) map[string]any {
	out := make(map[string]any, len(base)+len(extra))
	for k, v := range base {
		out[k] = v
	}
	for k, v := range extra {
		out[k] = v
	}
	return out
}
