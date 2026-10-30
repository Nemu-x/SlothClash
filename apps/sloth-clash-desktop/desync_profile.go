package main

import (
	"encoding/json"
	"reflect"
	"runtime"
)

// Desync settings belong to a profile (Profile.Desync): turning Desync off,
// or choosing what it covers, on one profile leaves the others alone. The
// active profile's settings drive the runtime config and the calibration.
//
// prefsCurrent.Desync is their in-memory copy for the readers that work
// without an App (config generation, the calibration controller, the cache
// pruning): syncActiveDesyncLocked refreshes it whenever the active profile
// or its settings change. It is never written to prefs.json.
//
// Before settings moved into profiles they were one global object, prefs.json
// "desync". A profiles.json without the desyncPerProfile marker gets that
// object copied into every profile that has no settings of its own, once
// (migrateLegacyDesyncLocked); after the copy is saved the object is dropped
// from prefs.json.

// defaultDesyncSettings is what a profile that never had its Desync settings
// changed uses: YouTube, plus Discord where its method works. Desync itself
// is off (a Desync-only profile is always on, see profileDesyncSettings).
func defaultDesyncSettings() DesyncSettings {
	return DesyncSettings{YouTube: true, Discord: runtime.GOOS == "windows"}
}

// profileDesyncSettings is p's effective Desync settings: the stored ones
// (the defaults when none were stored) with a sanitized custom list. A
// Desync-only profile has nothing but Desync routes, so for it Desync is
// always on, whatever was stored.
func profileDesyncSettings(p Profile) DesyncSettings {
	s := defaultDesyncSettings()
	if p.Desync != nil {
		s = *p.Desync
	}
	s = effectiveDesyncSettings(s)
	if profileIsDesyncOnly(p) {
		s.Enabled = true
		s.Custom = withoutVPNRules(s.Custom)
	}
	return s
}

// desyncOtherProfileKeys is the cache keys (DesyncCustomEntry.key) of the
// calibrated entries of every profile but the active one. The cache keeps their
// winners, so switching back to a profile does not search again. Guarded by
// prefsMu, refreshed with prefsCurrent.Desync.
var desyncOtherProfileKeys map[string]bool

// syncActiveDesyncLocked copies the active profile's effective Desync
// settings into prefsCurrent.Desync (the defaults, off, without an active
// profile). Caller holds a.mu (read or write); takes prefsMu.
func (a *App) syncActiveDesyncLocked() {
	active := defaultDesyncSettings()
	others := map[string]bool{}
	idx := a.activeProfileIndexLocked()
	for i, p := range a.profiles {
		if i == idx {
			active = profileDesyncSettings(p)
			continue
		}
		if p.Desync != nil {
			for _, e := range sanitizeDesyncCustom(p.Desync.Custom) {
				if e.calibrated() {
					others[e.key()] = true
				}
			}
		}
	}
	prefsMu.Lock()
	prefsCurrent.Desync = active
	desyncOtherProfileKeys = others
	prefsMu.Unlock()
}

// legacyDesyncSettings is prefs.json "desync" as builds before per-profile
// settings wrote it. The TTL and repeat fields never reached the config; they
// are read only to tell a saved object from an untouched one.
type legacyDesyncSettings struct {
	Enabled           bool                `json:"enabled"`
	YouTube           bool                `json:"youtube"`
	Discord           bool                `json:"discord"`
	DiscordTTL        int                 `json:"discordTTL"`
	DiscordUDP        bool                `json:"discordUDP"`
	DiscordUDPTTL     int                 `json:"discordUDPTTL"`
	DiscordUDPRepeats int                 `json:"discordUDPRepeats"`
	Custom            []DesyncCustomEntry `json:"custom,omitempty"`
}

// prefsLegacyDesync is prefs.json "desync" as loaded, nil when absent or
// already moved into the profiles. It is written back with every prefs save
// until the move is on disk. Guarded by prefsMu.
var prefsLegacyDesync json.RawMessage

// legacyDesyncSelection is what the legacy object meant, and whether it
// meant anything other than the defaults. Those builds treated an object that
// was never saved (every field zero; the custom list has its own writers)
// as the defaults, so an untouched one with no custom entries changes
// nothing.
func legacyDesyncSelection(raw json.RawMessage) (DesyncSettings, bool) {
	var l legacyDesyncSettings
	if len(raw) == 0 || json.Unmarshal(raw, &l) != nil {
		return DesyncSettings{}, false
	}
	custom := sanitizeDesyncCustom(l.Custom)
	l.Custom = nil
	if reflect.DeepEqual(l, legacyDesyncSettings{}) {
		if len(custom) == 0 {
			return DesyncSettings{}, false
		}
		s := defaultDesyncSettings()
		s.Custom = custom
		return s, true
	}
	return DesyncSettings{Enabled: l.Enabled, YouTube: l.YouTube, Discord: l.Discord, DiscordUDP: l.DiscordUDP, Custom: custom}, true
}

// migrateLegacyDesyncLocked copies the legacy global settings into every
// profile without settings of its own and marks the move done. Reports
// whether a profile changed (profiles.json must then be saved). Running it
// again changes nothing: only profiles without settings are filled, and the
// marker stops it on the next start. Caller holds a.mu (write).
func (a *App) migrateLegacyDesyncLocked() bool {
	a.desyncPerProfile = true
	prefsMu.RLock()
	raw := prefsLegacyDesync
	prefsMu.RUnlock()
	legacy, ok := legacyDesyncSelection(raw)
	if !ok {
		return false
	}
	changed := false
	for i := range a.profiles {
		if a.profiles[i].Desync != nil {
			continue
		}
		s := legacy
		s.Custom = append([]DesyncCustomEntry(nil), legacy.Custom...)
		a.profiles[i].Desync = &s
		changed = true
	}
	return changed
}

// dropLegacyDesyncPrefs removes the legacy object from prefs.json once the
// profiles carrying its copy are saved.
func dropLegacyDesyncPrefs() {
	prefsMu.Lock()
	defer prefsMu.Unlock()
	if prefsLegacyDesync == nil {
		return
	}
	prefsLegacyDesync = nil
	savePrefsBestEffort(prefsCurrent)
}
