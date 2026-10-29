package main

import (
	"crypto/rand"
	"encoding/json"
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

// DesyncCustomEntry is one user site routed through Desync (the domain and
// its subdomains), or one popular service from the catalog (Service set,
// Domain empty: its domains are read from desync.Services when the config is
// generated, so a catalog update reaches stored entries). ID names the
// entry's hidden group and never changes.
type DesyncCustomEntry struct {
	ID      string `json:"id"`
	Domain  string `json:"domain"`
	Service string `json:"service,omitempty"`
	Enabled bool   `json:"enabled"`
}

// UnmarshalJSON also reads "preset", the key popular services were stored
// under before the rename. "service" wins when both are present.
func (e *DesyncCustomEntry) UnmarshalJSON(b []byte) error {
	type plain DesyncCustomEntry
	var raw struct {
		plain
		Preset string `json:"preset"`
	}
	if err := json.Unmarshal(b, &raw); err != nil {
		return err
	}
	*e = DesyncCustomEntry(raw.plain)
	if e.Service == "" {
		e.Service = raw.Preset
	}
	return nil
}

// key is the entry's cache key (desync.CustomEntryKey).
func (e DesyncCustomEntry) key() string { return desync.CustomEntryKey(e.Domain, e.Service) }

// DesyncService is one popular service of the catalog as the UI lists it.
type DesyncService struct {
	ID      string   `json:"id"`
	Name    string   `json:"name"`
	Domains []string `json:"domains"`
}

// desyncCustomError is a validation failure; Code is stable for the UI.
// Detail, when set, names what the failure refers to (a service ID), never a
// user domain.
type desyncCustomError struct{ Code, Detail string }

func (e *desyncCustomError) Error() string {
	if e.Detail != "" {
		return "custom domain: " + e.Code + ": " + e.Detail
	}
	return "custom domain: " + e.Code
}

func customDomainError(code string) error { return &desyncCustomError{Code: code} }

// serviceOverlap returns the service ID among entries whose catalog domains
// overlap domain, or "". Every catalog domain counts, not only the ones the
// service owns at the moment: ownership changes as services are switched.
func serviceOverlap(entries []DesyncCustomEntry, domain string) string {
	for _, e := range entries {
		if e.Service == "" {
			continue
		}
		p, _ := desync.ServiceByID(e.Service)
		for _, d := range p.Domains {
			if desync.DomainsOverlap(d, domain) {
				return e.Service
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
// see (bad ID, invalid or re-normalized domain, unknown or repeated service, a
// service entry with a domain, duplicates, a domain overlapping a service
// entry's) and keeps at most desyncCustomLimit. Prefs are ours, but a
// hand-edited file must not be able to put arbitrary text into generated
// rules. A service entry wins over a plain one overlapping it wherever either
// is in the list (the API never stores both).
func sanitizeDesyncCustom(entries []DesyncCustomEntry) []DesyncCustomEntry {
	if len(entries) == 0 {
		return nil
	}
	ids := map[string]bool{}
	services := map[string]bool{}
	var kept []DesyncCustomEntry // valid service entries, for the overlap check
	for _, e := range entries {
		if _, known := desync.ServiceByID(e.Service); e.Service != "" && known && e.Domain == "" && validDesyncCustomID(e.ID) && !ids[e.ID] && !services[e.Service] {
			ids[e.ID] = true
			services[e.Service] = true
			kept = append(kept, e)
		}
	}
	var out []DesyncCustomEntry
	domains := map[string]bool{}
	for _, e := range entries {
		if e.Service != "" {
			if services[e.Service] && ids[e.ID] && len(out) < desyncCustomLimit {
				delete(services, e.Service) // the first stored copy only
				out = append(out, e)
			}
			continue
		}
		domain, err := normalizeDesyncCustomDomain(e.Domain)
		if err != nil || domain != e.Domain || !validDesyncCustomID(e.ID) || ids[e.ID] || domains[domain] || serviceOverlap(kept, domain) != "" || len(out) >= desyncCustomLimit {
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
	disabled := false
	next, err := a.updateActiveDesync(func(next DesyncSettings, desyncOnly bool) (DesyncSettings, error) {
		entries, err := change(next.Custom)
		if err != nil {
			return next, err
		}
		next.Custom = entries
		if next.Enabled && !desyncOnly && !desyncHasSelection(next) {
			next.Enabled = false
			disabled = true
		}
		return next, nil
	})
	if err != nil {
		return a.GetDesyncStatus(), err
	}
	entries := next.Custom
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
		return addDesyncCustomDomain(entries, domain)
	})
}

// addDesyncCustomDomain appends the (normalized) domain to entries, enabled.
func addDesyncCustomDomain(entries []DesyncCustomEntry, domain string) ([]DesyncCustomEntry, error) {
	for _, e := range entries {
		if e.Domain == domain {
			return nil, customDomainError("duplicate")
		}
	}
	// A service in the list (on or off) already covers its domains; a
	// second group for the same names would split their traffic.
	if service := serviceOverlap(entries, domain); service != "" {
		return nil, &desyncCustomError{Code: "service_overlap", Detail: service}
	}
	if len(entries) >= desyncCustomLimit {
		return nil, customDomainError("limit")
	}
	id, err := newDesyncCustomID(entries)
	if err != nil {
		return nil, err
	}
	return append(entries, DesyncCustomEntry{ID: id, Domain: domain, Enabled: true}), nil
}

// ListDesyncServices returns the popular-service catalog (ID, name, every
// domain).
func (a *App) ListDesyncServices() []DesyncService {
	out := make([]DesyncService, 0, len(desync.Services))
	for _, p := range desync.Services {
		out = append(out, DesyncService{ID: p.ID, Name: p.Name, Domains: append([]string(nil), p.Domains...)})
	}
	return out
}

// AddDesyncService adds the popular service id as one entry, enabled. Plain
// entries the service covers (a domain equal to, or under, one of its
// domains, e.g. a "facebook.com" typed before the catalog existed) are
// replaced by it: the service routes the same names and more, and keeping both would
// put one name in two groups. RemoveDesyncCustomDomain and
// SetDesyncCustomDomainEnabled take the service entry's ID like any other.
func (a *App) AddDesyncService(id string) (DesyncStatus, error) {
	if _, ok := desync.ServiceByID(id); !ok {
		return a.GetDesyncStatus(), customDomainError("unknown_service")
	}
	return a.updateDesyncCustom("add_service", func(entries []DesyncCustomEntry) ([]DesyncCustomEntry, error) {
		return addDesyncService(entries, id)
	})
}

// addDesyncService appends the popular service id to entries, enabled,
// dropping the plain entries it covers.
func addDesyncService(entries []DesyncCustomEntry, id string) ([]DesyncCustomEntry, error) {
	service, ok := desync.ServiceByID(id)
	if !ok {
		return nil, customDomainError("unknown_service")
	}
	kept := entries[:0:0]
	for _, e := range entries {
		if e.Service == service.ID {
			return nil, customDomainError("duplicate")
		}
		if e.Service == "" && serviceOverlap([]DesyncCustomEntry{{Service: service.ID}}, e.Domain) != "" {
			continue // replaced by the service
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
	return append(kept, DesyncCustomEntry{ID: entryID, Service: service.ID, Enabled: true}), nil
}

// The onboarding builds a new Desync-only profile's selection before the
// profile exists: nothing is saved until CreateDesyncOnlyProfile, so Cancel
// leaves every profile as it was. These methods apply the same rules as the
// saved list to a draft the UI holds (removing and switching an entry need
// no rules and are done there).

// NewDesyncDraft is the selection the onboarding starts from: a new
// profile's defaults.
func (a *App) NewDesyncDraft() DesyncSettings {
	return defaultDesyncSettings()
}

// DraftDesyncAddCustomDomain is AddDesyncCustomDomain on a draft.
func (a *App) DraftDesyncAddCustomDomain(draft DesyncSettings, input string) (DesyncSettings, error) {
	domain, err := normalizeDesyncCustomDomain(input)
	if err != nil {
		return draft, err
	}
	entries, err := addDesyncCustomDomain(sanitizeDesyncCustom(draft.Custom), domain)
	if err != nil {
		return draft, err
	}
	draft.Custom = entries
	return draft, nil
}

// DraftDesyncAddService is AddDesyncService on a draft.
func (a *App) DraftDesyncAddService(draft DesyncSettings, id string) (DesyncSettings, error) {
	entries, err := addDesyncService(sanitizeDesyncCustom(draft.Custom), id)
	if err != nil {
		return draft, err
	}
	draft.Custom = entries
	return draft, nil
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
