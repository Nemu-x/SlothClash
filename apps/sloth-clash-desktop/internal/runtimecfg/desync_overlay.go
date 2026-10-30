package runtimecfg

import (
	"sort"
	"strings"

	"SlothClashDesktop/internal/desync"
)

// The profile body is never edited. Only generated runtime entries are
// replaced; user scripts cannot silently remove an explicitly enabled route.
func applyDesyncOverlay(m map[string]any, s DesyncSettings, cache []desync.CacheEntry, networkKey func() string) {
	applyAutomaticDesyncOverlay(m, s, cache, networkKey)
}

func desyncSlice(value any) []any { values, _ := value.([]any); return values }

func applyAutomaticDesyncOverlay(m map[string]any, s DesyncSettings, cache []desync.CacheEntry, networkKey func() string) {
	// Also removes r1-r4 generated entries when migrating or disabling.
	managedRules := map[string]bool{}
	managedRules["AND,((NETWORK,udp),(PROCESS-NAME,Discord.exe)),"+desync.DiscordUDPRoute] = true // r3-r5 migration
	for _, r := range append(desync.Rules(desync.YouTubeDomains, desync.YouTubeRoute), desync.Rules(desync.DiscordDomains, desync.DiscordRoute)...) {
		managedRules[r] = true
		if strings.Contains(r, "DST-PORT,443") {
			managedRules[r[:strings.LastIndex(r, ",")]+",REJECT"] = true // r1-r8 migration
		}
	}
	for _, r := range desync.DiscordUDPRules() {
		managedRules[r] = true
	}
	removed := false
	for _, key := range []string{"proxies", "proxy-groups"} {
		entries := []any{}
		for _, v := range desyncSlice(m[key]) {
			if p, ok := v.(map[string]any); ok && p[desync.ManagedKey] == true {
				removed = true
				continue
			}
			entries = append(entries, v)
		}
		if _, ok := m[key]; ok {
			if key == "proxy-groups" && len(entries) == 0 {
				delete(m, key)
			} else {
				m[key] = entries
			}
		}
	}
	var rules []any
	for _, r := range desyncSlice(m["rules"]) {
		if text, ok := r.(string); ok && (managedRules[text] || isCustomDesyncRule(text)) {
			removed = true
			continue
		}
		rules = append(rules, r)
	}
	if !s.Enabled {
		if removed {
			m["rules"] = rules
		}
		return
	}
	proxies := desyncSlice(m["proxies"])
	groups := desyncSlice(m["proxy-groups"])
	// Read before anything is added: only the profile's own rules and groups
	// name its main group.
	vpnTarget := desyncVPNTarget(rules, groups, proxies)
	builtin := []any{}
	caps := s.Caps
	if caps.OS == "" {
		caps = desync.Platform
	}
	network := ""
	if networkKey != nil {
		network = networkKey()
	}
	for _, service := range []string{"voice", "youtube", "discord"} {
		route := ""
		var domains []string
		switch service {
		case "voice":
			if !s.DiscordUDP {
				continue
			}
			route = desync.DiscordUDPRoute
		case "youtube":
			if !s.YouTube {
				continue
			}
			route = desync.YouTubeRoute
			domains = desync.YouTubeDomains
		case "discord":
			if !s.Discord {
				continue
			}
			route = desync.DiscordRoute
			domains = desync.DiscordDomains
		}
		names := []string{}
		for _, c := range desync.Ordered(service, caps, desync.CachedSelection(cache, network, service)) {
			proxies = append(proxies, c.Proxy(route))
			names = append(names, c.Name(route))
		}
		// PASS last: no strategy left (or none this OS can run) falls
		// through to the profile's rules.
		names = append(names, "PASS")
		groups = append(groups, map[string]any{"name": route, "type": "select", "hidden": true, "proxies": names, "default-selected": names[0], desync.ManagedKey: true})
		if service == "voice" {
			for _, r := range desync.DiscordUDPRules() {
				builtin = append(builtin, r)
			}
		} else {
			for _, r := range desync.Rules(domains, route) {
				builtin = append(builtin, r)
			}
		}
	}
	// The user's rules and the popular services: one hidden group per entry,
	// named by its stable ID.
	for _, entry := range s.Custom {
		route := desync.CustomRoute(entry.ID)
		group := map[string]any{"name": route, "type": "select", "hidden": true, desync.ManagedKey: true}
		switch {
		case entry.Preset == "" && entry.Target == DesyncTargetVPN:
			// The profile's main group; PASS (the profile's own rules)
			// when it has none.
			member := vpnTarget
			if member == "" {
				member = "PASS"
			}
			group["proxies"] = []string{member}
			group["default-selected"] = member
		case entry.Preset == "" && entry.Target == DesyncTargetDirect:
			group["proxies"] = []string{"DIRECT"}
			group["default-selected"] = "DIRECT"
		case entry.Preset == "" && entry.Kind == DesyncKindProcess:
			// An application has no address to probe, so calibration
			// cannot test strategies for it: the catalog's first (cheapest)
			// TCP strategy, then PASS (also all there is should this OS
			// run none).
			names := []string{}
			if candidates := desync.Candidates("custom", caps); len(candidates) > 0 {
				proxies = append(proxies, candidates[0].Proxy(route))
				names = append(names, candidates[0].Name(route))
			}
			names = append(names, "PASS")
			group["proxies"] = names
			group["default-selected"] = names[0]
		default:
			// Calibrated: the custom candidates, DIRECT (the site is not
			// blocked) and PASS (nothing works: fall through to the
			// profile's rules).
			cached := desync.CachedSelection(cache, network, desync.CustomService(desync.CustomEntryKey(entry.Domain, entry.Preset)))
			names := []string{}
			for _, c := range desync.Ordered("custom", caps, cached) {
				proxies = append(proxies, c.Proxy(route))
				names = append(names, c.Name(route))
			}
			selected := names[0]
			if cached == desync.DirectID {
				selected = "DIRECT"
			}
			names = append(names, "DIRECT", "PASS")
			group["proxies"] = names
			group["default-selected"] = selected
		}
		groups = append(groups, group)
	}
	// Domain rules (the user's and the popular services') first, so a user's
	// rule for a built-in service's domain overrides the service; then the
	// built-in services; then the application rules, which a rule for the
	// destination beats.
	generated := customDesyncDomainRules(s.Custom)
	generated = append(generated, builtin...)
	generated = append(generated, customDesyncProcessRules(s.Custom)...)
	m["proxies"] = proxies
	m["proxy-groups"] = groups
	m["rules"] = append(generated, rules...)
}

// desyncBuiltinTargets are the core's own outbounds: never a profile's main
// group.
var desyncBuiltinTargets = map[string]bool{"DIRECT": true, "REJECT": true, "REJECT-DROP": true, "PASS": true, "COMPATIBLE": true, "GLOBAL": true}

// desyncVPNTarget is the group a user rule with the VPN target uses: the
// target of the profile's MATCH rule when that is one of its proxy groups or
// proxies, else the first visible proxy group with a member other than the
// core's own outbounds (a profile whose catch-all is DIRECT keeps its proxy
// selector there). "" when the profile has neither (a Desync-only profile).
func desyncVPNTarget(rules, groups, proxies []any) string {
	names := map[string]bool{}
	for _, list := range [][]any{groups, proxies} {
		for _, raw := range list {
			if entry, ok := raw.(map[string]any); ok {
				if name, ok := entry["name"].(string); ok && name != "" {
					names[name] = true
				}
			}
		}
	}
	for _, raw := range rules {
		text, ok := raw.(string)
		if !ok {
			continue
		}
		fields := strings.Split(text, ",")
		if len(fields) < 2 || !strings.EqualFold(strings.TrimSpace(fields[0]), "MATCH") {
			continue
		}
		if target := strings.TrimSpace(fields[1]); names[target] && !desyncBuiltinTargets[strings.ToUpper(target)] {
			return target
		}
		break // the first MATCH ends the rules
	}
	for _, raw := range groups {
		g, ok := raw.(map[string]any)
		if !ok || g["hidden"] == true {
			continue
		}
		name, _ := g["name"].(string)
		if name == "" || desyncBuiltinTargets[strings.ToUpper(name)] {
			continue
		}
		members := desyncSlice(g["proxies"])
		// A group filled from providers or include-all lists no members.
		usable := len(members) == 0 && (g["use"] != nil || g["include-all"] == true || g["include-all-proxies"] == true)
		for _, member := range members {
			if text, ok := member.(string); ok && !desyncBuiltinTargets[strings.ToUpper(strings.TrimSpace(text))] {
				usable = true
				break
			}
		}
		if usable {
			return name
		}
	}
	return ""
}

// desyncUserRule reports a user rule (not a preset) with the given kind.
func desyncUserRule(entry DesyncCustomRoute, kind string) bool {
	return entry.Preset == "" && entry.Kind == kind
}

// desyncEntryRules is the rules sending what one matcher (DOMAIN,
// DOMAIN-SUFFIX or PROCESS-NAME and its value) matches to entry's group: TCP
// plus UDP/443 for a Desync target (desync.MatchRules: the candidates reject
// QUIC so the application falls back to TCP, where the transform applies),
// every network for the VPN and direct targets.
func desyncEntryRules(entry DesyncCustomRoute, matcher, value string) []any {
	route := desync.CustomRoute(entry.ID)
	if entry.Preset == "" && (entry.Target == DesyncTargetVPN || entry.Target == DesyncTargetDirect) {
		return []any{matcher + "," + value + "," + route}
	}
	var out []any
	for _, rule := range desync.MatchRules(matcher, value, route) {
		out = append(out, rule)
	}
	return out
}

// customDesyncDomainRules routes the domain entries: the user's domain and
// suffix rules and the popular services' domains. A more specific domain
// (sub.example.org) goes before a broader one (example.org) so both work; at
// equal depth an exact-domain rule goes before a suffix rule and a user's rule
// before a popular service's domain, so the user's choice wins; then the
// user's entry order, and the catalog order within a service.
func customDesyncDomainRules(entries []DesyncCustomRoute) []any {
	type routed struct {
		entry  DesyncCustomRoute
		domain string
		rank   int
	}
	var ordered []routed
	for _, entry := range entries {
		if desyncUserRule(entry, DesyncKindProcess) {
			continue
		}
		rank := 1
		switch {
		case entry.Preset != "":
			rank = 2
		case entry.Kind == DesyncKindDomain:
			rank = 0
		}
		for _, domain := range customDesyncDomains(entry, entries) {
			ordered = append(ordered, routed{entry, domain, rank})
		}
	}
	sort.SliceStable(ordered, func(i, j int) bool {
		di, dj := strings.Count(ordered[i].domain, "."), strings.Count(ordered[j].domain, ".")
		if di != dj {
			return di > dj
		}
		return ordered[i].rank < ordered[j].rank
	})
	var out []any
	for _, r := range ordered {
		matcher := "DOMAIN-SUFFIX"
		if desyncUserRule(r.entry, DesyncKindDomain) {
			matcher = "DOMAIN"
		}
		out = append(out, desyncEntryRules(r.entry, matcher, r.domain)...)
	}
	return out
}

// customDesyncProcessRules routes the application rules, in the user's order.
func customDesyncProcessRules(entries []DesyncCustomRoute) []any {
	var out []any
	for _, entry := range entries {
		if desyncUserRule(entry, DesyncKindProcess) {
			out = append(out, desyncEntryRules(entry, "PROCESS-NAME", entry.Process)...)
		}
	}
	return out
}

// customDesyncDomains is the domains entry routes among the enabled entries:
// its own domain, or the preset's catalog domains minus those an enabled
// preset earlier in the catalog also lists (desync.ServiceOwnedDomains), so a
// shared domain is in exactly one group.
func customDesyncDomains(entry DesyncCustomRoute, entries []DesyncCustomRoute) []string {
	if entry.Preset == "" {
		return []string{entry.Domain}
	}
	var enabled []string
	for _, e := range entries {
		if e.Preset != "" {
			enabled = append(enabled, e.Preset)
		}
	}
	return desync.ServiceOwnedDomains(entry.Preset, enabled)
}

// isCustomDesyncRule reports a rule targeting a custom-domain group. The
// prefix is ours, so any such rule is a generated one from an earlier pass
// (or a removed entry) and is regenerated from the current settings.
func isCustomDesyncRule(rule string) bool {
	i := strings.LastIndex(rule, ",")
	return i >= 0 && strings.HasPrefix(rule[i+1:], desync.CustomRoutePrefix)
}
