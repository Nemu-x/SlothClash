package main

import (
	"net/netip"
	"strings"
)

// prefixesFromAny parses a YAML list (or single string) of CIDRs into prefixes,
// skipping anything unparsable — a malformed entry in a subscription must never
// make config generation fail.
func prefixesFromAny(v any) []netip.Prefix {
	var raw []string
	switch t := v.(type) {
	case []any:
		for _, e := range t {
			if s, ok := e.(string); ok {
				raw = append(raw, s)
			}
		}
	case []string:
		raw = append(raw, t...)
	case string:
		raw = append(raw, t)
	}
	out := make([]netip.Prefix, 0, len(raw))
	for _, s := range raw {
		p, err := netip.ParsePrefix(strings.TrimSpace(s))
		if err != nil {
			continue
		}
		out = append(out, p.Masked())
	}
	return out
}

// prefixCovers reports whether outer fully contains inner (same address family).
func prefixCovers(outer, inner netip.Prefix) bool {
	if outer.Addr().Is4() != inner.Addr().Is4() {
		return false
	}
	return outer.Bits() <= inner.Bits() && outer.Contains(inner.Addr())
}

// dropUnroutableFakeIPRange6 removes `dns.fake-ip-range6` when the TUN we are
// about to bring up would not route that pool.
//
// Measured on Windows 2026-08-31 with a real subscription: its tun block ships
// `route-exclude-address: [..., fc00::/7, ...]` (the usual list — verge honours
// the same one). mihomo's auto-route then installs the IPv6 default split MINUS
// the excluded ranges, so our injected ULA pool `fdfe:dcba:9876::1/64` — which
// lives inside fc00::/7 — gets no route into the tunnel at all. Windows sends
// those connections out of the physical interface instead, where nothing answers:
// `curl -6 https://fonts.googleapis.com` hung for 21 s and failed while the IPv4
// path took 0.27 s, and the core log showed the packets never reached mihomo.
// In a browser that is a Happy-Eyeballs stall on every dual-stack site — the
// "everything feels slower than it used to" symptom.
//
// A black-holed pool is strictly worse than no pool: with `fake-ip-range6` absent
// mihomo answers AAAA with NOERROR/NODATA (verified against the shipped core), so
// apps go straight to the working IPv4 fake address. The IPv6 leak this pool was
// added to close (architecture/ipv6.md) stays closed either way — top-level
// `ipv6: true` keeps the TUN's v6 routes installed, so real IPv6 destinations are
// still captured by the tunnel.
func dropUnroutableFakeIPRange6(m map[string]any) {
	tun, ok := m["tun"].(map[string]any)
	if !ok {
		return
	}
	if enabled, ok := tun["enable"].(bool); !ok || !enabled {
		return
	}
	// With auto-route off mihomo installs no routes and ignores the exclusion
	// list; whoever turned it off owns the routing table.
	if auto, ok := tun["auto-route"].(bool); ok && !auto {
		return
	}
	dns, ok := m["dns"].(map[string]any)
	if !ok {
		return
	}
	raw, _ := dns["fake-ip-range6"].(string)
	pool, err := netip.ParsePrefix(strings.TrimSpace(raw))
	if err != nil || pool.Addr().Is4() {
		return
	}
	pool = pool.Masked()
	// An explicit tun inet6-address covering the pool makes it on-link on the
	// tunnel itself, so it stays reachable whatever the exclusion list says.
	for _, onLink := range prefixesFromAny(tun["inet6-address"]) {
		if prefixCovers(onLink, pool) {
			return
		}
	}
	for _, ex := range prefixesFromAny(tun["route-exclude-address"]) {
		if !prefixCovers(ex, pool) {
			continue
		}
		delete(dns, "fake-ip-range6")
		debugLog("config", "IPV6-2", "fakeip_v6_route_guard.go:dropUnroutableFakeIPRange6",
			"dropped fake-ip-range6: excluded from tun routing",
			map[string]any{"pool": strings.TrimSpace(raw), "excludedBy": ex.String()})
		return
	}
}
