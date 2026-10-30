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
	generated := []any{}
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
				generated = append(generated, r)
			}
		} else {
			for _, r := range desync.Rules(domains, route) {
				generated = append(generated, r)
			}
		}
	}
	// Custom domains: one hidden group per entry, after the built-in services
	// (whose domains an entry can never repeat). Its members are the custom
	// candidates, DIRECT (the site is not blocked) and PASS (nothing works:
	// fall through to the profile's rules).
	for _, entry := range s.Custom {
		route := desync.CustomRoute(entry.ID)
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
		groups = append(groups, map[string]any{"name": route, "type": "select", "hidden": true, "proxies": names, "default-selected": selected, desync.ManagedKey: true})
	}
	generated = append(generated, customDesyncRules(s.Custom)...)
	m["proxies"] = proxies
	m["proxy-groups"] = groups
	m["rules"] = append(generated, rules...)
}

// customDesyncRules routes each entry's domains like a built-in service (TCP,
// plus UDP/443 so the candidates can reject QUIC and the browser falls back
// to TCP, where the transform applies). A more specific domain
// (sub.example.org) goes before a broader one (example.org) so both work;
// domains of equal depth keep the user's entry order, then the catalog order
// within a preset.
func customDesyncRules(entries []DesyncCustomRoute) []any {
	type routed struct{ domain, route string }
	var ordered []routed
	for _, entry := range entries {
		for _, domain := range customDesyncDomains(entry, entries) {
			ordered = append(ordered, routed{domain, desync.CustomRoute(entry.ID)})
		}
	}
	sort.SliceStable(ordered, func(i, j int) bool {
		return strings.Count(ordered[i].domain, ".") > strings.Count(ordered[j].domain, ".")
	})
	var out []any
	for _, r := range ordered {
		for _, rule := range desync.Rules([]string{r.domain}, r.route) {
			out = append(out, rule)
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
