package main

import (
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"net/netip"
	"net/url"
	"runtime"
	"strings"

	"SlothClashDesktop/internal/desync"

	"golang.org/x/net/idna"
	"golang.org/x/net/publicsuffix"
)

// desyncCustomLimit caps the custom-domain entries: each one is a hidden group
// with the whole custom candidate set, and they are calibrated one at a time.
const desyncCustomLimit = 32

// desyncFakeTTLSupported gates the fake-TTL strategies of the custom set: the
// same platform gate as Discord TCP (validateDesyncSettings). A variable so
// the goldens are generated with one candidate set on every OS.
var desyncFakeTTLSupported = runtime.GOOS == "windows"

// DesyncCustomEntry is one user domain routed through Desync (the domain and
// its subdomains), or one catalog preset (Preset set, Domain empty: its
// domains are read from desync.Presets when the config is generated, so a
// catalog update reaches stored entries). ID names the entry's hidden group
// and never changes.
type DesyncCustomEntry struct {
	ID      string `json:"id"`
	Domain  string `json:"domain"`
	Preset  string `json:"preset,omitempty"`
	Enabled bool   `json:"enabled"`
}

// key is the entry's cache key (desync.CustomEntryKey).
func (e DesyncCustomEntry) key() string { return desync.CustomEntryKey(e.Domain, e.Preset) }

// DesyncPreset is one catalog preset as the UI lists it.
type DesyncPreset struct {
	ID      string   `json:"id"`
	Name    string   `json:"name"`
	Domains []string `json:"domains"`
}

// desyncCustomError is a validation failure; Code is stable for the UI.
// Detail, when set, names what the failure refers to (a preset ID), never a
// user domain.
type desyncCustomError struct{ Code, Detail string }

func (e *desyncCustomError) Error() string {
	if e.Detail != "" {
		return "custom domain: " + e.Code + ": " + e.Detail
	}
	return "custom domain: " + e.Code
}

func customDomainError(code string) error { return &desyncCustomError{Code: code} }

// presetOverlap returns the preset ID among entries whose catalog domains
// overlap domain, or "". Every catalog domain counts, not only the ones the
// preset owns at the moment: ownership changes as presets are switched.
func presetOverlap(entries []DesyncCustomEntry, domain string) string {
	for _, e := range entries {
		if e.Preset == "" {
			continue
		}
		p, _ := desync.PresetByID(e.Preset)
		for _, d := range p.Domains {
			if desync.DomainsOverlap(d, domain) {
				return e.Preset
			}
		}
	}
	return ""
}

// normalizeDesyncCustomDomain turns what a user typed (a domain, a URL, a
// host:port) into the lowercase ASCII (punycode) domain a DOMAIN-SUFFIX rule
// and an https probe use, or rejects it. It checks the domain itself, not the
// entry list (duplicates and the cap are the caller's).
func normalizeDesyncCustomDomain(input string) (string, error) {
	host := strings.TrimSpace(input)
	if host == "" {
		return "", customDomainError("empty")
	}
	if strings.Contains(host, "://") {
		u, err := url.Parse(host)
		if err != nil || u.Host == "" {
			return "", customDomainError("invalid")
		}
		host = u.Host
	} else if i := strings.IndexAny(host, "/?#"); i >= 0 {
		host = host[:i]
	}
	if i := strings.LastIndex(host, "@"); i >= 0 {
		host = host[i+1:]
	}
	if strings.HasPrefix(host, "[") {
		return "", customDomainError("ip")
	}
	if strings.Count(host, ":") > 1 {
		if _, err := netip.ParseAddr(host); err == nil {
			return "", customDomainError("ip")
		}
		return "", customDomainError("invalid")
	}
	if h, port, ok := strings.Cut(host, ":"); ok {
		if port == "" || strings.Trim(port, "0123456789") != "" {
			return "", customDomainError("invalid")
		}
		host = h
	}
	if strings.ContainsAny(host, "*") || strings.HasPrefix(host, "+.") {
		return "", customDomainError("wildcard")
	}
	host = strings.TrimPrefix(strings.TrimSuffix(host, "."), ".")
	if _, err := netip.ParseAddr(host); err == nil {
		return "", customDomainError("ip")
	}
	ascii, err := idna.Lookup.ToASCII(host)
	if err != nil || ascii == "" || len(ascii) > 253 {
		return "", customDomainError("invalid")
	}
	labels := strings.Split(ascii, ".")
	if len(labels) < 2 {
		return "", customDomainError("public_suffix")
	}
	for _, label := range labels {
		if label == "" || len(label) > 63 || strings.HasPrefix(label, "-") || strings.HasSuffix(label, "-") || strings.Trim(label, "abcdefghijklmnopqrstuvwxyz0123456789-") != "" {
			return "", customDomainError("invalid")
		}
	}
	if strings.Trim(labels[len(labels)-1], "0123456789") == "" {
		return "", customDomainError("ip") // e.g. 1.2.3 — not a name
	}
	if suffix, _ := publicsuffix.PublicSuffix(ascii); suffix == ascii {
		return "", customDomainError("public_suffix")
	}
	if desync.BuiltinDomainFor(ascii) != "" {
		return "", customDomainError("builtin")
	}
	return ascii, nil
}

func validDesyncCustomID(id string) bool {
	return len(id) >= 1 && len(id) <= 16 && strings.Trim(id, "abcdefghijklmnopqrstuvwxyz0123456789") == ""
}

// sanitizeDesyncCustom drops stored entries the config generation must not
// see (bad ID, invalid or re-normalized domain, unknown or repeated preset, a
// preset entry with a domain, duplicates, a domain overlapping a preset
// entry's) and keeps at most desyncCustomLimit. Prefs are ours, but a
// hand-edited file must not be able to put arbitrary text into generated
// rules. A preset entry wins over a plain one overlapping it wherever either
// is in the list (the API never stores both).
func sanitizeDesyncCustom(entries []DesyncCustomEntry) []DesyncCustomEntry {
	if len(entries) == 0 {
		return nil
	}
	ids := map[string]bool{}
	presets := map[string]bool{}
	var kept []DesyncCustomEntry // valid preset entries, for the overlap check
	for _, e := range entries {
		if _, known := desync.PresetByID(e.Preset); e.Preset != "" && known && e.Domain == "" && validDesyncCustomID(e.ID) && !ids[e.ID] && !presets[e.Preset] {
			ids[e.ID] = true
			presets[e.Preset] = true
			kept = append(kept, e)
		}
	}
	var out []DesyncCustomEntry
	domains := map[string]bool{}
	for _, e := range entries {
		if e.Preset != "" {
			if presets[e.Preset] && ids[e.ID] && len(out) < desyncCustomLimit {
				delete(presets, e.Preset) // the first stored copy only
				out = append(out, e)
			}
			continue
		}
		domain, err := normalizeDesyncCustomDomain(e.Domain)
		if err != nil || domain != e.Domain || !validDesyncCustomID(e.ID) || ids[e.ID] || domains[domain] || presetOverlap(kept, domain) != "" || len(out) >= desyncCustomLimit {
			continue
		}
		ids[e.ID] = true
		domains[domain] = true
		out = append(out, e)
	}
	return out
}

func newDesyncCustomID(existing []DesyncCustomEntry) (string, error) {
	for range 8 {
		var b [4]byte
		if _, err := rand.Read(b[:]); err != nil {
			return "", err
		}
		id := hex.EncodeToString(b[:])
		taken := false
		for _, e := range existing {
			taken = taken || e.ID == id
		}
		if !taken {
			return id, nil
		}
	}
	return "", fmt.Errorf("could not allocate a custom domain id")
}

// desyncHasSelection reports whether s routes anything through Desync.
func desyncHasSelection(s DesyncSettings) bool {
	if s.YouTube || s.Discord || s.DiscordUDP {
		return true
	}
	for _, e := range s.Custom {
		if e.Enabled {
			return true
		}
	}
	return false
}

// updateDesyncCustom applies change to the stored custom entries. Turning the
// last selected route off also turns Desync off (an enabled Desync with
// nothing selected is not a valid state). Changing the entries regenerates
// the config but does not restart the running calibration: the controller
// picks the new entry up and searches only for it.
func (a *App) updateDesyncCustom(action string, change func([]DesyncCustomEntry) ([]DesyncCustomEntry, error)) (DesyncStatus, error) {
	prefsMu.Lock()
	next := effectiveDesyncSettings(prefsCurrent.Desync)
	entries, err := change(append([]DesyncCustomEntry(nil), next.Custom...))
	if err != nil {
		prefsMu.Unlock()
		return a.GetDesyncStatus(), err
	}
	next.Custom = entries
	disabled := false
	if next.Enabled && !desyncHasSelection(next) {
		next.Enabled = false
		disabled = true
	}
	prefsCurrent.Desync = next
	savePrefsBestEffort(prefsCurrent)
	prefsMu.Unlock()
	if disabled {
		a.desyncCtl.invalidate()
	}
	enabled := 0
	for _, e := range entries {
		if e.Enabled {
			enabled++
		}
	}
	a.appendRuntimeDiag("desync.custom", fmt.Sprintf("action=%s entries=%d enabled=%d desync_enabled=%t", action, len(entries), enabled, next.Enabled))
	a.triggerRuntimeReloadForPrefs()
	return a.GetDesyncStatus(), nil
}

// AddDesyncCustomDomain adds a custom domain (normalized; see
// normalizeDesyncCustomDomain), enabled.
func (a *App) AddDesyncCustomDomain(input string) (DesyncStatus, error) {
	domain, err := normalizeDesyncCustomDomain(input)
	if err != nil {
		return a.GetDesyncStatus(), err
	}
	return a.updateDesyncCustom("add", func(entries []DesyncCustomEntry) ([]DesyncCustomEntry, error) {
		for _, e := range entries {
			if e.Domain == domain {
				return nil, customDomainError("duplicate")
			}
		}
		// A preset in the list (on or off) already covers its domains; a
		// second group for the same names would split their traffic.
		if preset := presetOverlap(entries, domain); preset != "" {
			return nil, &desyncCustomError{Code: "preset_overlap", Detail: preset}
		}
		if len(entries) >= desyncCustomLimit {
			return nil, customDomainError("limit")
		}
		id, err := newDesyncCustomID(entries)
		if err != nil {
			return nil, err
		}
		return append(entries, DesyncCustomEntry{ID: id, Domain: domain, Enabled: true}), nil
	})
}

// ListDesyncPresets returns the preset catalog (ID, name, every domain).
func (a *App) ListDesyncPresets() []DesyncPreset {
	out := make([]DesyncPreset, 0, len(desync.Presets))
	for _, p := range desync.Presets {
		out = append(out, DesyncPreset{ID: p.ID, Name: p.Name, Domains: append([]string(nil), p.Domains...)})
	}
	return out
}

// AddDesyncPreset adds the catalog preset id as one entry, enabled. Plain
// entries the preset covers (a domain equal to, or under, one of its
// domains, e.g. a "facebook.com" typed before presets existed) are replaced
// by it: the preset routes the same names and more, and keeping both would
// put one name in two groups. RemoveDesyncCustomDomain and
// SetDesyncCustomDomainEnabled take the preset entry's ID like any other.
func (a *App) AddDesyncPreset(id string) (DesyncStatus, error) {
	preset, ok := desync.PresetByID(id)
	if !ok {
		return a.GetDesyncStatus(), customDomainError("unknown_preset")
	}
	return a.updateDesyncCustom("add_preset", func(entries []DesyncCustomEntry) ([]DesyncCustomEntry, error) {
		kept := entries[:0:0]
		for _, e := range entries {
			if e.Preset == preset.ID {
				return nil, customDomainError("duplicate")
			}
			if e.Preset == "" && presetOverlap([]DesyncCustomEntry{{Preset: preset.ID}}, e.Domain) != "" {
				continue // replaced by the preset
			}
			kept = append(kept, e)
		}
		if len(kept) >= desyncCustomLimit {
			return nil, customDomainError("limit")
		}
		entryID, err := newDesyncCustomID(entries)
		if err != nil {
			return nil, err
		}
		return append(kept, DesyncCustomEntry{ID: entryID, Preset: preset.ID, Enabled: true}), nil
	})
}

// RemoveDesyncCustomDomain removes the entry with the given ID.
func (a *App) RemoveDesyncCustomDomain(id string) (DesyncStatus, error) {
	return a.updateDesyncCustom("remove", func(entries []DesyncCustomEntry) ([]DesyncCustomEntry, error) {
		for i, e := range entries {
			if e.ID == id {
				return append(entries[:i], entries[i+1:]...), nil
			}
		}
		return nil, customDomainError("not_found")
	})
}

// SetDesyncCustomDomainEnabled switches one entry on or off.
func (a *App) SetDesyncCustomDomainEnabled(id string, enabled bool) (DesyncStatus, error) {
	return a.updateDesyncCustom("toggle", func(entries []DesyncCustomEntry) ([]DesyncCustomEntry, error) {
		for i := range entries {
			if entries[i].ID == id {
				entries[i].Enabled = enabled
				return entries, nil
			}
		}
		return nil, customDomainError("not_found")
	})
}
