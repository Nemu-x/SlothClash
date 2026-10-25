package main

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"net"
	"sort"
	"strings"
	"time"

	"SlothClashDesktop/internal/desync"
)

type DesyncNetworkCache struct {
	Network string `json:"network"`
	YouTube string `json:"youtube,omitempty"`
	Discord string `json:"discord,omitempty"`
	Voice   string `json:"voice,omitempty"`
	Updated int64  `json:"updated"`
}

func desyncNetworkKey() string {
	// Digest only; neither addresses nor interface identifiers leave this helper.
	var parts []string
	interfaces, _ := net.Interfaces()
	for _, i := range interfaces {
		name := strings.ToLower(i.Name)
		if strings.Contains(name, "sloth") || strings.Contains(name, "wintun") || strings.HasPrefix(name, "utun") {
			continue
		}
		if i.Flags&net.FlagUp == 0 || i.Flags&net.FlagLoopback != 0 {
			continue
		}
		addresses, _ := i.Addrs()
		for _, a := range addresses {
			if strings.HasPrefix(a.String(), "198.18.") || strings.HasPrefix(a.String(), "fe80:") {
				continue
			}
			parts = append(parts, fmt.Sprintf("%d:%s:%s", i.Index, i.Name, a.String()))
		}
	}
	sort.Strings(parts)
	build, _ := bundledResources.ReadFile(embeddedCoreBuildFile)
	sum := sha256.Sum256(append([]byte("desync-auto-v1\n"+strings.Join(parts, "\n")), build...))
	return hex.EncodeToString(sum[:12])
}
// desyncCacheEntries converts the persisted winner cache to the catalog's type
// (same fields; DesyncNetworkCache only adds the JSON tags).
func desyncCacheEntries(cache []DesyncNetworkCache) []desync.CacheEntry {
	if len(cache) == 0 {
		return nil
	}
	out := make([]desync.CacheEntry, len(cache))
	for i, c := range cache {
		out[i] = desync.CacheEntry(c)
	}
	return out
}

func desyncCachedSelection(network, service string) string {
	return desync.CachedSelection(desyncCacheEntries(currentDesktopPrefs().DesyncCache), network, service)
}

func saveDesyncSelection(network, service, id string) {
	prefsMu.Lock()
	defer prefsMu.Unlock()
	entries := append([]DesyncNetworkCache(nil), prefsCurrent.DesyncCache...)
	var entry DesyncNetworkCache
	filtered := []DesyncNetworkCache{}
	for _, c := range entries {
		if c.Network == network {
			entry = c
		} else {
			filtered = append(filtered, c)
		}
	}
	entry.Network = network
	entry.Updated = time.Now().Unix()
	switch service {
	case "youtube":
		entry.YouTube = id
	case "discord":
		entry.Discord = id
	case "voice":
		entry.Voice = id
	}
	filtered = append(filtered, entry)
	if len(filtered) > 6 {
		filtered = filtered[len(filtered)-6:]
	}
	prefsCurrent.DesyncCache = filtered
	savePrefsBestEffort(prefsCurrent)
}

func applyAutomaticDesyncOverlay(m map[string]any, s DesyncSettings, cache []desync.CacheEntry) {
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
		if text, ok := r.(string); ok && managedRules[text] {
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
	network := desyncNetworkKey()
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
		for _, c := range desync.Ordered(service, desync.CachedSelection(cache, network, service)) {
			proxies = append(proxies, c.Proxy(route))
			names = append(names, c.Name(route))
		}
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
	m["proxies"] = proxies
	m["proxy-groups"] = groups
	m["rules"] = append(generated, rules...)
}
