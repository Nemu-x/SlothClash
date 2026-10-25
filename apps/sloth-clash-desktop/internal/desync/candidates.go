package desync

import (
	"fmt"
	"time"
)

// Candidate is one desync strategy a route can use; Name(route) is the name
// of the managed proxy implementing it.
type Candidate struct {
	ID, TLSRec, Split string
	TTL, Repeats      int
	UDP               bool
}

func (c Candidate) Name(route string) string { return route + "-" + c.ID }

// Candidates returns the strategies tried for service ("youtube", "discord"
// or "voice"), in probe order.
func Candidates(service string) []Candidate {
	var out []Candidate
	if service == "youtube" {
		for _, n := range []int{3, 1, 2, 4, 8} {
			out = append(out, Candidate{ID: fmt.Sprintf("r%d", n), TLSRec: fmt.Sprintf("%d+s", n)})
		}
		return append(out, Candidate{ID: "split", Split: "1+s"})
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
		c := Candidate{ID: fmt.Sprintf("f%d", ttl), TLSRec: "1+s", TTL: ttl}
		if service == "voice" {
			c.UDP = true
			c.TLSRec = ""
			c.Repeats = 1
		}
		out = append(out, c)
	}
	if service == "voice" {
		for _, ttl := range []int{4, 5, 3, 6, 8, 10, 12} {
			out = append(out, Candidate{ID: fmt.Sprintf("f%d-r2", ttl), UDP: true, TTL: ttl, Repeats: 2})
		}
	} else {
		out = append(out, Candidate{ID: "record", TLSRec: "1+s"}, Candidate{ID: "split", Split: "1+s"})
	}
	return out
}

// Proxy is the managed direct proxy implementing c for route.
func (c Candidate) Proxy(route string) map[string]any {
	opts := map[string]any{"dns-fallback": true}
	if !c.UDP {
		opts["reject-quic"] = true
	}
	if c.TLSRec != "" {
		opts["tlsrec"] = c.TLSRec
	}
	if c.Split != "" {
		opts["split"] = c.Split
		opts["split-delay-ms"] = 5
	}
	if c.UDP {
		opts["udp"] = map[string]any{"ttl": c.TTL, "repeats": c.Repeats, "packets": 3, "stable-binding": true}
	} else if c.TTL > 0 {
		opts["fake"] = map[string]any{"ttl": c.TTL, "prefix": -1}
	}
	return map[string]any{"name": c.Name(route), "type": "direct", ManagedKey: true, "desync": opts}
}

// CachedSelection returns the candidate ID cached as the winner for service on
// network, or "" when there is none or it is older than a day.
func CachedSelection(cache []CacheEntry, network, service string) string {
	for _, c := range cache {
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

// Ordered is Candidates(service) with the cached winner, if any, moved first.
func Ordered(service, cached string) []Candidate {
	candidates := Candidates(service)
	for i, c := range candidates {
		if c.ID == cached {
			return append([]Candidate{c}, append(candidates[:i:i], candidates[i+1:]...)...)
		}
	}
	return candidates
}

// CacheEntry is one network's remembered winners (the desktop prefs persist
// them as DesyncNetworkCache, which has the same fields).
type CacheEntry struct {
	Network string
	YouTube string
	Discord string
	Voice   string
	Updated int64
}
