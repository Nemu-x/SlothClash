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
	// Custom maps a custom domain to its winner (desync.CacheEntry.Custom).
	Custom  map[string]string `json:"custom,omitempty"`
	Updated int64             `json:"updated"`
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
