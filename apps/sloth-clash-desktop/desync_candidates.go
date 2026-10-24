package main

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"net"
	"sort"
	"strings"
	"time"
)

type desyncCandidate struct {
	ID, TLSRec, Split string
	TTL, Repeats      int
	UDP               bool
}

func (c desyncCandidate) name(route string) string { return route + "-" + c.ID }
func desyncCandidates(service string) []desyncCandidate {
	var out []desyncCandidate
	if service == "youtube" {
		for _, n := range []int{3, 1, 2, 4, 8} {
			out = append(out, desyncCandidate{ID: fmt.Sprintf("r%d", n), TLSRec: fmt.Sprintf("%d+s", n)})
		}
		return append(out, desyncCandidate{ID: "split", Split: "1+s"})
	}
	ttls := []int{4, 5, 3, 6, 2, 7, 8, 10, 12, 16, 24, 32}
	for ttl := 1; ttl <= 32; ttl++ {
		seen := false
		for _, v := range ttls {
			if v == ttl {
				seen = true
				break
			}
		}
		if !seen {
			ttls = append(ttls, ttl)
		}
	}
	for _, ttl := range ttls {
		c := desyncCandidate{ID: fmt.Sprintf("f%d", ttl), TLSRec: "1+s", TTL: ttl}
		if service == "voice" {
			c.UDP = true
			c.TLSRec = ""
			c.Repeats = 1
		}
		out = append(out, c)
	}
	if service == "voice" {
		for _, ttl := range []int{4, 5, 3, 6, 8, 10, 12} {
			out = append(out, desyncCandidate{ID: fmt.Sprintf("f%d-r2", ttl), UDP: true, TTL: ttl, Repeats: 2})
		}
	} else {
		out = append(out, desyncCandidate{ID: "record", TLSRec: "1+s"}, desyncCandidate{ID: "split", Split: "1+s"})
	}
	return out
}
func (c desyncCandidate) proxy(route string) map[string]any {
	opts := map[string]any{"dns-fallback": true}
	if c.TLSRec != "" {
		opts["tlsrec"] = c.TLSRec
	}
	if c.Split != "" {
		opts["split"] = c.Split
		opts["split-delay-ms"] = 5
	}
	if c.UDP {
		opts["udp"] = map[string]any{"ttl": c.TTL, "repeats": c.Repeats, "packets": 3}
	} else if c.TTL > 0 {
		opts["fake"] = map[string]any{"ttl": c.TTL, "prefix": -1}
	}
	return map[string]any{"name": c.name(route), "type": "direct", desyncManagedKey: true, "desync": opts}
}

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
func desyncCachedSelection(network, service string) string {
	for _, c := range currentDesktopPrefs().DesyncCache {
		if c.Network != network || time.Now().Unix()-c.Updated > 86400 {
			continue
		}
		switch service {
		case "youtube":
			return c.YouTube
		case "discord":
			return c.Discord
		case "voice":
			return c.Voice
		}
	}
	return ""
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
func orderedDesyncCandidates(service, cached string) []desyncCandidate {
	candidates := desyncCandidates(service)
	for i, c := range candidates {
		if c.ID == cached {
			return append([]desyncCandidate{c}, append(candidates[:i:i], candidates[i+1:]...)...)
		}
	}
	return candidates
}

func applyAutomaticDesyncOverlay(m map[string]any, s DesyncSettings) {
	// Also removes r1-r4 generated entries when migrating or disabling.
	managedRules := map[string]bool{}
	for _, r := range append(desyncRules(desyncYouTubeDomains, desyncYouTubeRoute), desyncRules(desyncDiscordDomains, desyncDiscordRoute)...) {
		managedRules[r] = true
	}
	for _, r := range desyncDiscordUDPRules() {
		managedRules[r] = true
	}
	removed := false
	for _, key := range []string{"proxies", "proxy-groups"} {
		entries := []any{}
		for _, v := range desyncSlice(m[key]) {
			if p, ok := v.(map[string]any); ok && p[desyncManagedKey] == true {
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
			route = desyncDiscordUDPRoute
		case "youtube":
			if !s.YouTube {
				continue
			}
			route = desyncYouTubeRoute
			domains = desyncYouTubeDomains
		case "discord":
			if !s.Discord {
				continue
			}
			route = desyncDiscordRoute
			domains = desyncDiscordDomains
		}
		names := []string{}
		for _, c := range orderedDesyncCandidates(service, desyncCachedSelection(network, service)) {
			proxies = append(proxies, c.proxy(route))
			names = append(names, c.name(route))
		}
		groups = append(groups, map[string]any{"name": route, "type": "select", "hidden": true, "proxies": names, "default-selected": names[0], desyncManagedKey: true})
		if service == "voice" {
			for _, r := range desyncDiscordUDPRules() {
				generated = append(generated, r)
			}
		} else {
			for _, r := range desyncRules(domains, route) {
				generated = append(generated, r)
			}
		}
	}
	m["proxies"] = proxies
	m["proxy-groups"] = groups
	m["rules"] = append(generated, rules...)
}
