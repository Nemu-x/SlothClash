package main

import (
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"net/netip"
	"net/url"
	"strings"

	"SlothClashDesktop/internal/desync"

	"golang.org/x/net/idna"
	"golang.org/x/net/publicsuffix"
)

// desyncCustomLimit caps the custom-domain entries: each one is a hidden group
// with the whole custom candidate set, and they are calibrated one at a time.
const desyncCustomLimit = 32

// DesyncCustomEntry is one user domain routed through Desync (the domain and
// its subdomains). ID names the entry's hidden group and never changes.
type DesyncCustomEntry struct {
	ID      string `json:"id"`
	Domain  string `json:"domain"`
	Enabled bool   `json:"enabled"`
}

// desyncCustomError is a validation failure; Code is stable for the UI.
type desyncCustomError struct{ Code string }

func (e *desyncCustomError) Error() string { return "custom domain: " + e.Code }

func customDomainError(code string) error { return &desyncCustomError{Code: code} }

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
// see (bad ID, invalid or re-normalized domain, duplicates) and keeps at most
// desyncCustomLimit. Prefs are ours, but a hand-edited file must not be able
// to put arbitrary text into generated rules.
func sanitizeDesyncCustom(entries []DesyncCustomEntry) []DesyncCustomEntry {
	if len(entries) == 0 {
		return nil
	}
	var out []DesyncCustomEntry
	ids := map[string]bool{}
	domains := map[string]bool{}
	for _, e := range entries {
		domain, err := normalizeDesyncCustomDomain(e.Domain)
		if err != nil || domain != e.Domain || !validDesyncCustomID(e.ID) || ids[e.ID] || domains[domain] || len(out) >= desyncCustomLimit {
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
