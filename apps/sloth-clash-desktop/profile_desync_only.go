package main

import (
	"errors"
	"reflect"
	"strings"
	"time"
)

// A Desync-only profile is for users without a VPN subscription: it has no
// URL and no nodes, and its generated config is the Desync routes (YouTube,
// Discord, Discord voice, custom domains) with everything else MATCH,DIRECT
// (runtimecfg's DesyncOnly input). The user creates one on demand from the
// Add profile dialog (CreateDesyncOnlyProfile); it is never created
// implicitly.
//
// What it is not allowed to carry is enforced at every writer: no
// subscription URL or age key, no extend / proxy-groups / rules templates, no
// script, no hand-edited config.yaml, no auto-update. There is nothing for any
// of them to act on, and each would make the generated config something other
// than "Desync + direct". A profiles.json edited by hand is sanitized on load
// (sanitizeDesyncOnlyProfile).

const profileKindDesyncOnly = "desync-only"

// errDesyncOnlyProfileField is returned by every setter of a field a
// Desync-only profile does not have. Clearing such a field (empty value) is
// always accepted, so generic "save everything" UI paths keep working.
var errDesyncOnlyProfileField = errors.New("a Desync-only profile has no subscription, templates or script")

// profileIsDesyncOnly reports whether p is a Desync-only profile.
func profileIsDesyncOnly(p Profile) bool {
	return p.Kind == profileKindDesyncOnly
}

// sanitizeDesyncOnlyProfile drops everything a Desync-only profile must not
// carry. A no-op for every other profile. Reports whether p changed.
func sanitizeDesyncOnlyProfile(p *Profile) bool {
	if !profileIsDesyncOnly(*p) {
		return false
	}
	before := *p
	p.Type = "local"
	p.URL = ""
	p.AgeSecretKey = ""
	p.SubscriptionInfo = ""
	p.SubscriptionSupportURL = ""
	p.SubscriptionAnnouncement = ""
	p.MergeTemplate = ""
	p.ProxyTemplate = ""
	p.RulesTemplate = ""
	p.ScriptOverride = ""
	p.ScriptError = ""
	p.ScriptErrorLine = 0
	p.ScriptErrorColumn = 0
	p.ScriptConsole = nil
	p.ScriptConsoleCut = false
	p.ScriptLastDurationMS = 0
	p.SkipAutoConfig = false
	p.AutoUpdateEnabled = false
	p.AutoUpdateIntervalMinutes = 0
	p.LastUpdateAttempt = 0
	p.LastUpdateError = ""
	p.LastUpdateErrorTransient = false
	p.LastGoodGroup = ""
	return !reflect.DeepEqual(before, *p)
}

// rejectDesyncOnlyValue is the setter guard: a non-empty value for a field a
// Desync-only profile does not have is an error.
func rejectDesyncOnlyValue(p Profile, value string) error {
	if profileIsDesyncOnly(p) && strings.TrimSpace(value) != "" {
		return errDesyncOnlyProfileField
	}
	return nil
}

// CreateDesyncOnlyProfile adds a Desync-only profile (no VPN). An empty name
// gets the default one. Like an import, it becomes the active profile only
// when there is none yet; activating it is the user's next step (the UI
// offers to turn Desync on at that point if it is off).
func (a *App) CreateDesyncOnlyProfile(name string) (AppState, error) {
	finalName := strings.TrimSpace(name)
	if finalName == "" {
		finalName = "Desync only"
	}
	a.mu.Lock()
	defer a.mu.Unlock()
	p := Profile{
		ID:          a.newProfileIDLocked(time.Now()),
		Name:        finalName,
		Type:        "local",
		Kind:        profileKindDesyncOnly,
		LastUpdated: time.Now().Unix(),
	}
	a.profiles = append(a.profiles, p)
	a.state.Profile.Profiles = a.profiles
	if a.state.Profile.ActiveProfileID == "" {
		a.state.Profile.ActiveProfileID = p.ID
		a.enforceDesyncOnlyModeLocked()
	}
	a.state.UpdatedAt = time.Now().Unix()
	if err := a.persistProfilesLocked(); err != nil {
		return a.state, err
	}
	return a.state, nil
}

// newProfileIDLocked mints "profile-<timestamp>" like the import paths, moving
// one second forward while the ID is taken (two profiles created within the
// same second would otherwise share a runtime dir). Caller holds a.mu.
func (a *App) newProfileIDLocked(now time.Time) string {
	for {
		id := "profile-" + now.Format("20060102150405")
		taken := false
		for _, p := range a.profiles {
			if p.ID == id {
				taken = true
				break
			}
		}
		if !taken {
			return id
		}
		now = now.Add(time.Second)
	}
}

// activeProfileIsDesyncOnlyLocked reports whether the active profile is a
// Desync-only one. Caller holds a.mu (read or write).
func (a *App) activeProfileIsDesyncOnlyLocked() bool {
	idx := a.activeProfileIndexLocked()
	return idx >= 0 && profileIsDesyncOnly(a.profiles[idx])
}

// activeProfileIsDesyncOnly is activeProfileIsDesyncOnlyLocked under a.mu.
func (a *App) activeProfileIsDesyncOnly() bool {
	a.mu.RLock()
	defer a.mu.RUnlock()
	return a.activeProfileIsDesyncOnlyLocked()
}

// enforceDesyncOnlyModeLocked pins the routing mode to Rule while a
// Desync-only profile is active: its whole config is rules (Desync routes,
// then MATCH,DIRECT), Global has no node to send traffic to and Direct would
// switch Desync off. Reports whether the mode changed. Caller holds a.mu
// (write).
//
// LastNonDirectMode is left alone: it is the user's own Rule/Global choice,
// not something this profile chose, and restoreModeAfterDesyncOnlyLocked
// brings it back when a profile with servers becomes active again.
func (a *App) enforceDesyncOnlyModeLocked() bool {
	if !a.activeProfileIsDesyncOnlyLocked() || a.state.Mode.Current == "rule" {
		return false
	}
	a.state.Mode.Current = "rule"
	if strings.EqualFold(strings.TrimSpace(a.state.Proxy.ActiveGroup), "GLOBAL") {
		a.state.Proxy.ActiveGroup = ""
	}
	return true
}

// restoreModeAfterDesyncOnlyLocked runs after the active profile changed;
// wasDesyncOnly tells whether the previous one was a Desync-only profile.
// Leaving it for a profile with servers ends the forced Rule mode: the mode
// returns to the user's last own non-direct choice (LastNonDirectMode), so a
// Global choice made before visiting the Desync-only profile survives the
// round trip instead of silently becoming Rule. A Direct mode from before the
// visit is not brought back (LastNonDirectMode never holds Direct); Rule or
// Global, whichever the user picked last, is the least surprising state for a
// profile with servers. Reports whether the mode changed. Caller holds a.mu
// (write).
func (a *App) restoreModeAfterDesyncOnlyLocked(wasDesyncOnly bool) bool {
	if !wasDesyncOnly || a.activeProfileIsDesyncOnlyLocked() || a.state.Mode.Current != "rule" || a.state.Mode.LastNonDirectMode != "global" {
		return false
	}
	a.state.Mode.Current = "global"
	return true
}

// errDesyncOnlyMode is SetMode's answer to Global / Direct while a
// Desync-only profile is active.
var errDesyncOnlyMode = errors.New("a Desync-only profile works in Rule mode only")
