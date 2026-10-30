package main

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net/netip"
	"net/url"
	"runtime"
	"strings"
	"unicode"

	"SlothClashDesktop/internal/desync"
	"SlothClashDesktop/internal/runtimecfg"

	"golang.org/x/net/idna"
	"golang.org/x/net/publicsuffix"
)

// desyncCustomLimit caps the entries with a Desync target ("My rules" sent
// through Desync, and the popular services): each one is a hidden group with
// the whole custom candidate set, and they are calibrated one at a time.
const desyncCustomLimit = 32

// desyncRuleLimit caps the whole list, the VPN and direct rules included
// (each is one hidden group with one member and a rule or two).
const desyncRuleLimit = 128

// What a user rule matches and where it sends the traffic (DesyncCustomEntry
// Kind and Route). Entries stored before rules had a kind and a route (the
// "Sites" list) read as a suffix sent through Desync.
const (
	desyncKindSuffix  = runtimecfg.DesyncKindSuffix
	desyncKindDomain  = runtimecfg.DesyncKindDomain
	desyncKindProcess = runtimecfg.DesyncKindProcess

	desyncRouteAuto   = runtimecfg.DesyncTargetAuto
	desyncRouteVPN    = runtimecfg.DesyncTargetVPN
	desyncRouteDirect = runtimecfg.DesyncTargetDirect
)

// DesyncCustomEntry is one of the user's rules ("My rules"), or one popular
// service from the catalog (Service set, Domain, Kind and Route empty: its
// domains are read from desync.Services when the config is generated, so a
// catalog update reaches stored entries, and it always goes through Desync).
//
// A rule matches a domain and its subdomains (Kind suffix), exactly one
// domain (domain) or an application by its process name (process; the name
// is in Process, Domain is empty), and sends it through Desync (Route desync:
// a strategy found by calibration, or the catalog default for an
// application), to the profile's main proxy group (vpn) or directly
// (direct). ID names the entry's hidden group and never changes; editing a
// rule gives it a new one.
type DesyncCustomEntry struct {
	ID      string `json:"id"`
	Domain  string `json:"domain"`
	Process string `json:"process,omitempty"`
	Kind    string `json:"kind,omitempty"`
	Route   string `json:"route,omitempty"`
	Service string `json:"service,omitempty"`
	Enabled bool   `json:"enabled"`
}

// DesyncRuleInput is a rule as the UI submits it: Kind suffix, domain or
// process; Value what the user typed; Route desync, vpn or direct.
type DesyncRuleInput struct {
	Kind  string `json:"kind"`
	Value string `json:"value"`
	Route string `json:"route"`
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

// kind is the entry's kind, suffix for an entry stored before kinds.
func (e DesyncCustomEntry) kind() string {
	if e.Kind == "" {
		return desyncKindSuffix
	}
	return e.Kind
}

// route is the entry's route, desync for an entry stored before routes and
// for a popular service.
func (e DesyncCustomEntry) route() string {
	if e.Route == "" || e.Service != "" {
		return desyncRouteAuto
	}
	return e.Route
}

// throughDesync reports an entry with a Desync group of candidates (counted
// by desyncCustomLimit).
func (e DesyncCustomEntry) throughDesync() bool { return e.route() == desyncRouteAuto }

// calibrated reports an entry the calibration searches a strategy for: a
// popular service, or a domain or suffix rule through Desync. An application
// has no address to probe.
func (e DesyncCustomEntry) calibrated() bool {
	return e.throughDesync() && (e.Service != "" || e.kind() != desyncKindProcess)
}

// key is the entry's cache key (desync.CustomEntryKey).
func (e DesyncCustomEntry) key() string { return desync.CustomEntryKey(e.Domain, e.Service) }

// value is what a rule matches: its domain or process name.
func (e DesyncCustomEntry) value() string {
	if e.kind() == desyncKindProcess {
		return e.Process
	}
	return e.Domain
}

// sameRule reports two rules matching the same thing (a process name in any
// case: the core compares them case-insensitively).
func (e DesyncCustomEntry) sameRule(o DesyncCustomEntry) bool {
	return e.Service == "" && o.Service == "" && e.kind() == o.kind() && strings.EqualFold(e.value(), o.value())
}

// DesyncService is one popular service of the catalog as the UI lists it.
type DesyncService struct {
	ID      string   `json:"id"`
	Name    string   `json:"name"`
	Domains []string `json:"domains"`
}

// desyncError is a Desync settings failure the UI translates: "desync:
// <code>" or "desync: <code>: <detail>". Code is stable for the UI. Detail,
// when set, names what the failure refers to (a service ID), never a user
// domain.
//
// Codes: the rule list's (empty, invalid, ip, wildcard, public_suffix,
// builtin, duplicate, limit, rule_limit, not_found, service_overlap,
// unknown_service, invalid_process, invalid_kind, invalid_route, no_vpn) and
// the settings' (unsupported_core, unsupported_udp_core, no_selection,
// discord_platform, no_profile).
type desyncError struct{ Code, Detail string }

func (e *desyncError) Error() string {
	if e.Detail != "" {
		return "desync: " + e.Code + ": " + e.Detail
	}
	return "desync: " + e.Code
}

func desyncErr(code string) error { return &desyncError{Code: code} }

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
// host:port) into the lowercase ASCII (punycode) domain a rule sent through
// Desync and an https probe use, or rejects it: on top of
// normalizeDesyncHost, a public suffix is no site to probe and a built-in
// service's domain has its own switch. It checks the domain itself, not the
// entry list (duplicates and the cap are the caller's).
func normalizeDesyncCustomDomain(input string) (string, error) {
	ascii, err := normalizeDesyncHost(input)
	if err != nil {
		return "", err
	}
	if !strings.Contains(ascii, ".") {
		return "", desyncErr("public_suffix")
	}
	if suffix, _ := publicsuffix.PublicSuffix(ascii); suffix == ascii {
		return "", desyncErr("public_suffix")
	}
	if desync.BuiltinDomainFor(ascii) != "" {
		return "", desyncErr("builtin")
	}
	return ascii, nil
}

// normalizeDesyncHost turns what a user typed (a domain, a URL, a host:port)
// into a lowercase ASCII (punycode) domain name, one label or more, or
// rejects it.
func normalizeDesyncHost(input string) (string, error) {
	host := strings.TrimSpace(input)
	if host == "" {
		return "", desyncErr("empty")
	}
	if strings.Contains(host, "://") {
		u, err := url.Parse(host)
		if err != nil || u.Host == "" {
			return "", desyncErr("invalid")
		}
		host = u.Host
	} else if i := strings.IndexAny(host, "/?#"); i >= 0 {
		host = host[:i]
	}
	if i := strings.LastIndex(host, "@"); i >= 0 {
		host = host[i+1:]
	}
	if strings.HasPrefix(host, "[") {
		return "", desyncErr("ip")
	}
	if strings.Count(host, ":") > 1 {
		if _, err := netip.ParseAddr(host); err == nil {
			return "", desyncErr("ip")
		}
		return "", desyncErr("invalid")
	}
	if h, port, ok := strings.Cut(host, ":"); ok {
		if port == "" || strings.Trim(port, "0123456789") != "" {
			return "", desyncErr("invalid")
		}
		host = h
	}
	if strings.ContainsAny(host, "*") || strings.HasPrefix(host, "+.") {
		return "", desyncErr("wildcard")
	}
	host = strings.TrimPrefix(strings.TrimSuffix(host, "."), ".")
	if _, err := netip.ParseAddr(host); err == nil {
		return "", desyncErr("ip")
	}
	ascii, err := idna.Lookup.ToASCII(host)
	if err != nil || ascii == "" || len(ascii) > 253 {
		return "", desyncErr("invalid")
	}
	labels := strings.Split(ascii, ".")
	for _, label := range labels {
		if label == "" || len(label) > 63 || strings.HasPrefix(label, "-") || strings.HasSuffix(label, "-") || strings.Trim(label, "abcdefghijklmnopqrstuvwxyz0123456789-") != "" {
			return "", desyncErr("invalid")
		}
	}
	if strings.Trim(labels[len(labels)-1], "0123456789") == "" {
		return "", desyncErr("ip") // e.g. 1.2.3 — not a name
	}
	return ascii, nil
}

// desyncProcessNameMax bounds a process name (a file name: 255 on every
// desktop OS).
const desyncProcessNameMax = 255

// normalizeDesyncProcess turns what a user typed into the process name a
// PROCESS-NAME rule matches: an executable's file name such as Telegram.exe
// (Windows) or telegram (Linux, macOS). A pasted path is reduced to its file
// name and surrounding quotes are dropped. Wildcards (the core's
// PROCESS-NAME-WILDCARD is not offered) and the characters a rule cannot
// carry (comma, parentheses, control characters) are rejected. The case is
// kept: the core compares names case-insensitively.
func normalizeDesyncProcess(input string) (string, error) {
	name := strings.TrimSpace(input)
	name = strings.TrimSpace(strings.Trim(name, `"'`))
	if i := strings.LastIndexAny(name, `/\`); i >= 0 {
		name = strings.TrimSpace(name[i+1:])
	}
	if name == "" {
		return "", desyncErr("empty")
	}
	if strings.ContainsAny(name, "*?") {
		return "", desyncErr("wildcard")
	}
	if name == "." || name == ".." || len(name) > desyncProcessNameMax || strings.ContainsAny(name, `,()"<>|:`) {
		return "", desyncErr("invalid_process")
	}
	for _, r := range name {
		if unicode.IsControl(r) || r == unicode.ReplacementChar {
			return "", desyncErr("invalid_process")
		}
	}
	return name, nil
}

// desyncGOOS is the OS process names are completed for (a variable for
// tests).
var desyncGOOS = runtime.GOOS

// desyncProcessForOS completes a process name typed without an extension
// with ".exe" on Windows, where the core matches the executable's file name
// (telegram → telegram.exe). Elsewhere names have no extension.
func desyncProcessForOS(name, goos string) string {
	if goos == "windows" && !strings.Contains(name, ".") {
		return name + ".exe"
	}
	return name
}

// normalizeDesyncRule validates a rule (its kind, value and route) and
// returns it as an entry without ID or state. A rule sent through Desync
// follows the site rules (no public suffix, no built-in service domain); a
// VPN or direct suffix rule may name a whole zone (ru, co.uk), a domain rule
// may not. It checks the rule itself, not the list.
func normalizeDesyncRule(in DesyncRuleInput) (DesyncCustomEntry, error) {
	switch in.Route {
	case desyncRouteAuto, desyncRouteVPN, desyncRouteDirect:
	default:
		return DesyncCustomEntry{}, desyncErr("invalid_route")
	}
	entry := DesyncCustomEntry{Kind: in.Kind, Route: in.Route}
	switch in.Kind {
	case desyncKindProcess:
		name, err := normalizeDesyncProcess(in.Value)
		if err != nil {
			return DesyncCustomEntry{}, err
		}
		entry.Process = name
		return entry, nil
	case desyncKindSuffix, desyncKindDomain:
	default:
		return DesyncCustomEntry{}, desyncErr("invalid_kind")
	}
	if in.Route == desyncRouteAuto {
		domain, err := normalizeDesyncCustomDomain(in.Value)
		if err != nil {
			return DesyncCustomEntry{}, err
		}
		entry.Domain = domain
		return entry, nil
	}
	domain, err := normalizeDesyncHost(in.Value)
	if err != nil {
		return DesyncCustomEntry{}, err
	}
	if in.Kind == desyncKindDomain {
		if suffix, _ := publicsuffix.PublicSuffix(domain); suffix == domain {
			return DesyncCustomEntry{}, desyncErr("public_suffix")
		}
	}
	entry.Domain = domain
	return entry, nil
}

func validDesyncCustomID(id string) bool {
	return len(id) >= 1 && len(id) <= 16 && strings.Trim(id, "abcdefghijklmnopqrstuvwxyz0123456789") == ""
}

// sanitizeDesyncCustom drops stored entries the config generation must not
// see (bad ID, unknown kind or route, an invalid or re-normalized value,
// unknown or repeated service, a service entry with a rule's fields,
// duplicates, a Desync domain rule overlapping a service entry) and keeps at
// most desyncCustomLimit entries through Desync and desyncRuleLimit in all.
// Prefs are ours, but a hand-edited file must not be able to put arbitrary
// text into generated rules. A service entry wins over a rule overlapping it
// wherever either is in the list (the API never stores both).
//
// Rules come out with an explicit kind and route: an entry stored before
// rules had them (a "Sites" domain) becomes a suffix rule through Desync,
// which is what it always routed.
func sanitizeDesyncCustom(entries []DesyncCustomEntry) []DesyncCustomEntry {
	if len(entries) == 0 {
		return nil
	}
	ids := map[string]bool{}
	services := map[string]bool{}
	var kept []DesyncCustomEntry // valid service entries, for the overlap check
	for _, e := range entries {
		if _, known := desync.ServiceByID(e.Service); e.Service != "" && known && e.Domain == "" && e.Process == "" && e.Kind == "" && e.Route == "" && validDesyncCustomID(e.ID) && !ids[e.ID] && !services[e.Service] {
			ids[e.ID] = true
			services[e.Service] = true
			kept = append(kept, e)
		}
	}
	var out []DesyncCustomEntry
	through := 0
	add := func(e DesyncCustomEntry) {
		if e.throughDesync() {
			through++
		}
		out = append(out, e)
	}
	full := func(e DesyncCustomEntry) bool {
		return len(out) >= desyncRuleLimit || (e.throughDesync() && through >= desyncCustomLimit)
	}
	for _, e := range entries {
		if e.Service != "" {
			if services[e.Service] && ids[e.ID] && !full(e) {
				delete(services, e.Service) // the first stored copy only
				add(e)
			}
			continue
		}
		e.Kind, e.Route = e.kind(), e.route()
		rule, err := normalizeDesyncRule(DesyncRuleInput{Kind: e.Kind, Value: e.value(), Route: e.Route})
		if err != nil || rule.Domain != e.Domain || rule.Process != e.Process || !validDesyncCustomID(e.ID) || ids[e.ID] || full(e) {
			continue
		}
		if e.Kind != desyncKindProcess && e.Route == desyncRouteAuto && serviceOverlap(kept, e.Domain) != "" {
			continue
		}
		duplicate := false
		for _, o := range out {
			duplicate = duplicate || o.sameRule(e)
		}
		if duplicate {
			continue
		}
		ids[e.ID] = true
		add(e)
	}
	return out
}

// withoutVPNRules drops the rules sent to the VPN: a Desync-only profile has
// none (the API refuses them with no_vpn).
func withoutVPNRules(entries []DesyncCustomEntry) []DesyncCustomEntry {
	var out []DesyncCustomEntry
	for _, e := range entries {
		if e.Service != "" || e.route() != desyncRouteVPN {
			out = append(out, e)
		}
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

// desyncHasSelection reports whether s routes anything: a service, or a
// switched-on rule or popular service (a VPN or direct rule counts: it is
// the user's routing, applied while Desync is on).
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

// updateDesyncCustom applies change to the stored entries (desyncOnly: the
// active profile is a Desync-only one). Turning the last selected route off
// also turns Desync off (an enabled Desync with nothing selected is not a
// valid state). Changing the entries regenerates the config but does not
// restart the running calibration: the controller picks the new entry up and
// searches only for it.
func (a *App) updateDesyncCustom(action string, change func(entries []DesyncCustomEntry, desyncOnly bool) ([]DesyncCustomEntry, error)) (DesyncStatus, error) {
	disabled := false
	next, err := a.updateActiveDesync(func(next DesyncSettings, desyncOnly bool) (DesyncSettings, error) {
		entries, err := change(next.Custom, desyncOnly)
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

// AddDesyncCustomDomain adds a site: a suffix rule through Desync (the
// domain normalized; see normalizeDesyncCustomDomain), enabled.
func (a *App) AddDesyncCustomDomain(input string) (DesyncStatus, error) {
	return a.AddDesyncRule(DesyncRuleInput{Kind: desyncKindSuffix, Value: input, Route: desyncRouteAuto})
}

// AddDesyncRule adds a rule to the active profile, enabled. A process name
// typed without an extension gets ".exe" on Windows. The VPN route is
// refused on a Desync-only profile (no_vpn).
func (a *App) AddDesyncRule(input DesyncRuleInput) (DesyncStatus, error) {
	rule, err := desyncRuleFromInput(input)
	if err != nil {
		return a.GetDesyncStatus(), err
	}
	return a.updateDesyncCustom("add", func(entries []DesyncCustomEntry, desyncOnly bool) ([]DesyncCustomEntry, error) {
		return addDesyncRule(entries, rule, desyncOnly)
	})
}

// UpdateDesyncRule replaces the rule with the given ID by input, in place and
// keeping its on/off state. A changed rule gets a new ID: its hidden group,
// calibration state and cached strategy belonged to what it matched before.
func (a *App) UpdateDesyncRule(id string, input DesyncRuleInput) (DesyncStatus, error) {
	rule, err := desyncRuleFromInput(input)
	if err != nil {
		return a.GetDesyncStatus(), err
	}
	return a.updateDesyncCustom("edit", func(entries []DesyncCustomEntry, desyncOnly bool) ([]DesyncCustomEntry, error) {
		return updateDesyncRule(entries, id, rule, desyncOnly)
	})
}

// desyncRuleFromInput is normalizeDesyncRule for this OS.
func desyncRuleFromInput(input DesyncRuleInput) (DesyncCustomEntry, error) {
	rule, err := normalizeDesyncRule(input)
	if err != nil {
		return rule, err
	}
	if rule.Kind == desyncKindProcess {
		rule.Process = desyncProcessForOS(rule.Process, desyncGOOS)
	}
	return rule, nil
}

// checkDesyncRule checks rule (normalized) against the entries it joins
// (others: the list without the entry it replaces).
func checkDesyncRule(others []DesyncCustomEntry, rule DesyncCustomEntry, desyncOnly bool) error {
	if desyncOnly && rule.Route == desyncRouteVPN {
		return desyncErr("no_vpn")
	}
	through := 0
	for _, e := range others {
		if e.sameRule(rule) {
			return desyncErr("duplicate")
		}
		if e.throughDesync() {
			through++
		}
	}
	// A service in the list (on or off) already sends its domains through
	// Desync; a second Desync group for the same names would split their
	// traffic. A VPN or direct rule is the user overriding it and goes first.
	if rule.Route == desyncRouteAuto && rule.Kind != desyncKindProcess {
		if service := serviceOverlap(others, rule.Domain); service != "" {
			return &desyncError{Code: "service_overlap", Detail: service}
		}
	}
	if rule.throughDesync() && through >= desyncCustomLimit {
		return desyncErr("limit")
	}
	if len(others) >= desyncRuleLimit {
		return desyncErr("rule_limit")
	}
	return nil
}

// addDesyncRule appends the (normalized) rule to entries, enabled.
func addDesyncRule(entries []DesyncCustomEntry, rule DesyncCustomEntry, desyncOnly bool) ([]DesyncCustomEntry, error) {
	if err := checkDesyncRule(entries, rule, desyncOnly); err != nil {
		return nil, err
	}
	id, err := newDesyncCustomID(entries)
	if err != nil {
		return nil, err
	}
	rule.ID, rule.Enabled = id, true
	return append(entries, rule), nil
}

// updateDesyncRule replaces the rule id by the (normalized) rule. A popular
// service is not a rule (not_found).
func updateDesyncRule(entries []DesyncCustomEntry, id string, rule DesyncCustomEntry, desyncOnly bool) ([]DesyncCustomEntry, error) {
	at := -1
	for i, e := range entries {
		if e.ID == id && e.Service == "" {
			at = i
		}
	}
	if at < 0 {
		return nil, desyncErr("not_found")
	}
	old := entries[at]
	if old.kind() == rule.Kind && old.route() == rule.Route && old.Domain == rule.Domain && old.Process == rule.Process {
		return entries, nil
	}
	others := append(append([]DesyncCustomEntry(nil), entries[:at]...), entries[at+1:]...)
	if err := checkDesyncRule(others, rule, desyncOnly); err != nil {
		return nil, err
	}
	newID, err := newDesyncCustomID(entries)
	if err != nil {
		return nil, err
	}
	rule.ID, rule.Enabled = newID, old.Enabled
	out := append([]DesyncCustomEntry(nil), entries...)
	out[at] = rule
	return out, nil
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

// AddDesyncService adds the popular service id as one entry, enabled. Domain
// and suffix rules through Desync that the service covers (a domain equal to,
// or under, one of its domains, e.g. a "facebook.com" typed before the
// catalog existed) are replaced by it: the service routes the same names and
// more, and keeping both would put one name in two Desync groups. VPN and
// direct rules stay: they override the service. RemoveDesyncCustomDomain and
// SetDesyncCustomDomainEnabled take the service entry's ID like any other.
func (a *App) AddDesyncService(id string) (DesyncStatus, error) {
	if _, ok := desync.ServiceByID(id); !ok {
		return a.GetDesyncStatus(), desyncErr("unknown_service")
	}
	return a.updateDesyncCustom("add_service", func(entries []DesyncCustomEntry, _ bool) ([]DesyncCustomEntry, error) {
		return addDesyncService(entries, id)
	})
}

// addDesyncService appends the popular service id to entries, enabled,
// dropping the Desync domain rules it covers.
func addDesyncService(entries []DesyncCustomEntry, id string) ([]DesyncCustomEntry, error) {
	service, ok := desync.ServiceByID(id)
	if !ok {
		return nil, desyncErr("unknown_service")
	}
	kept := entries[:0:0]
	through := 0
	for _, e := range entries {
		if e.Service == service.ID {
			return nil, desyncErr("duplicate")
		}
		if e.Service == "" && e.route() == desyncRouteAuto && e.kind() != desyncKindProcess && serviceOverlap([]DesyncCustomEntry{{Service: service.ID}}, e.Domain) != "" {
			continue // replaced by the service
		}
		if e.throughDesync() {
			through++
		}
		kept = append(kept, e)
	}
	if through >= desyncCustomLimit {
		return nil, desyncErr("limit")
	}
	if len(kept) >= desyncRuleLimit {
		return nil, desyncErr("rule_limit")
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
// no rules and are done there). The draft is a Desync-only profile's, so the
// VPN route is refused.

// NewDesyncDraft is the selection the onboarding starts from: a new
// profile's defaults.
func (a *App) NewDesyncDraft() DesyncSettings {
	return defaultDesyncSettings()
}

// DraftDesyncAddCustomDomain is AddDesyncCustomDomain on a draft.
func (a *App) DraftDesyncAddCustomDomain(draft DesyncSettings, input string) (DesyncSettings, error) {
	return a.DraftDesyncAddRule(draft, DesyncRuleInput{Kind: desyncKindSuffix, Value: input, Route: desyncRouteAuto})
}

// DraftDesyncAddRule is AddDesyncRule on a draft.
func (a *App) DraftDesyncAddRule(draft DesyncSettings, input DesyncRuleInput) (DesyncSettings, error) {
	rule, err := desyncRuleFromInput(input)
	if err != nil {
		return draft, err
	}
	entries, err := addDesyncRule(sanitizeDesyncCustom(draft.Custom), rule, true)
	if err != nil {
		return draft, err
	}
	draft.Custom = entries
	return draft, nil
}

// DraftDesyncUpdateRule is UpdateDesyncRule on a draft.
func (a *App) DraftDesyncUpdateRule(draft DesyncSettings, id string, input DesyncRuleInput) (DesyncSettings, error) {
	rule, err := desyncRuleFromInput(input)
	if err != nil {
		return draft, err
	}
	entries, err := updateDesyncRule(sanitizeDesyncCustom(draft.Custom), id, rule, true)
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

// RemoveDesyncCustomDomain removes the entry (a rule or a popular service)
// with the given ID.
func (a *App) RemoveDesyncCustomDomain(id string) (DesyncStatus, error) {
	return a.updateDesyncCustom("remove", func(entries []DesyncCustomEntry, _ bool) ([]DesyncCustomEntry, error) {
		for i, e := range entries {
			if e.ID == id {
				return append(entries[:i], entries[i+1:]...), nil
			}
		}
		return nil, desyncErr("not_found")
	})
}

// SetDesyncCustomDomainEnabled switches one entry on or off.
func (a *App) SetDesyncCustomDomainEnabled(id string, enabled bool) (DesyncStatus, error) {
	return a.updateDesyncCustom("toggle", func(entries []DesyncCustomEntry, _ bool) ([]DesyncCustomEntry, error) {
		for i := range entries {
			if entries[i].ID == id {
				entries[i].Enabled = enabled
				return entries, nil
			}
		}
		return nil, desyncErr("not_found")
	})
}
