package main

import (
	"strings"
	"sync"
)

// Corp-VPN coexistence config overlay (corp-vpn-coexistence, task 4).
//
// When the user runs their corporate VPN (OpenConnect sidecar, managed by the
// privileged service) alongside mihomo, the two must not fight over the same
// traffic. OpenConnect installs host routes for the corp subnets it was pushed;
// mihomo's TUN otherwise swallows ALL traffic (auto-route). To let corp subnets
// fall through to OpenConnect we exclude them from the TUN, and to make corp
// hostnames resolve to their REAL internal addresses (not mihomo fake-ips) we
// point their DNS at the corp resolvers and keep them out of the fake-ip pool.
//
// Everything here is additive and a strict no-op when no corp split is active,
// so with the feature off the generated config is byte-identical to before
// (the config-parity guard stays green).

// corpVpnSplit is the split learned from the corp gateway — the subset of the
// service's CorpVpnStatusData the config overlay needs.
type corpVpnSplit struct {
	Routes     []string // corp subnets (CIDR) — excluded from mihomo TUN → routed to OpenConnect
	DNSServers []string // corp resolvers
	DNSDomains []string // corp search domains resolved via the corp resolvers
	Tundev     string   // corp tunnel interface (e.g. "utun4") for the DNS interface-bind
}

// active reports whether there is anything to overlay. A full-tunnel corp
// connection pushes no split routes; there is nothing coexistence can do there,
// so it is treated as inactive for overlay purposes (the UI warns separately).
func (s corpVpnSplit) active() bool {
	return len(s.Routes) > 0
}

var (
	corpVpnStateMu sync.RWMutex
	corpVpnState   corpVpnSplit
)

// setCorpVpnSplit records the split the running corp sidecar reported (called by
// the lifecycle orchestration on connect; cleared on disconnect). The next
// config reload picks it up via currentCorpVpnSplit.
func setCorpVpnSplit(s corpVpnSplit) {
	corpVpnStateMu.Lock()
	corpVpnState = s
	corpVpnStateMu.Unlock()
}

// currentCorpVpnSplit returns the active corp split (empty when the sidecar is
// down), read by the config overlay on every reload.
func currentCorpVpnSplit() corpVpnSplit {
	corpVpnStateMu.RLock()
	defer corpVpnStateMu.RUnlock()
	return corpVpnState
}

// applyCorpVpnOverlay injects corp-VPN coexistence into a generated runtime
// config map. No-op unless the split has routes.
//
//  1. tun.route-exclude-address += corp subnets — the kernel routes those to the
//     OpenConnect utun instead of mihomo's TUN.
//  2. dns.nameserver-policy: corp domains → corp resolvers (split-DNS), so corp
//     names resolve inside the corp network.
//  3. dns.fake-ip-filter += corp domains — CRITICAL under enhanced-mode:fake-ip.
//     Without this, corp names get a synthetic 198.18.x.x that never matches the
//     excluded corp subnets, so the split silently fails; keeping them out of
//     the pool yields their real internal IPs, which the route-exclude then
//     steers to OpenConnect.
//
// Additive and idempotent: existing entries are preserved and de-duplicated, and
// user/subscription DNS policy for a domain is never overwritten.
func applyCorpVpnOverlay(m map[string]any, split corpVpnSplit) {
	if !split.active() {
		return
	}

	// (1) route-exclude-address on the tun block.
	tun, ok := m["tun"].(map[string]any)
	if !ok || tun == nil {
		tun = map[string]any{}
	}
	tun["route-exclude-address"] = mergeStringListInto(tun["route-exclude-address"], split.Routes)
	m["tun"] = tun

	// DNS bits only make sense when there is a dns block to extend. It is always
	// present under TUN (ensureDefaultDNSForTun runs first), but be defensive.
	dns, ok := m["dns"].(map[string]any)
	if !ok || dns == nil {
		dns = map[string]any{}
	}

	// (2) split-DNS via nameserver-policy: match both the bare domain and its
	// subdomains ("+.corp" ⇒ *.corp). Never clobber a policy the subscription
	// already set for that exact key.
	if len(split.DNSDomains) > 0 && len(split.DNSServers) > 0 {
		policy, ok := dns["nameserver-policy"].(map[string]any)
		if !ok || policy == nil {
			policy = map[string]any{}
		}
		// Bind each corp resolver to the corp tunnel interface (mihomo's
		// `<resolver>#<iface>` syntax) so the DNS dial egresses the corp utun
		// instead of being bound to the physical NIC by auto-detect-interface.
		// Validated live: `route get <resolver>` → the corp utun. Without the
		// bind, corp-domain resolution silently fails when both VPNs are up.
		boundServers := make([]string, 0, len(split.DNSServers))
		for _, s := range split.DNSServers {
			s = strings.TrimSpace(s)
			if s == "" {
				continue
			}
			if split.Tundev != "" && !strings.Contains(s, "#") {
				s += "#" + split.Tundev
			}
			boundServers = append(boundServers, s)
		}
		servers := toAnyList(boundServers)
		for _, d := range split.DNSDomains {
			d = strings.TrimSpace(strings.TrimPrefix(d, "."))
			if d == "" {
				continue
			}
			for _, key := range []string{d, "+." + d} {
				if _, exists := policy[key]; !exists {
					policy[key] = servers
				}
			}
		}
		dns["nameserver-policy"] = policy
	}

	// (3) keep corp domains out of the fake-ip pool so they resolve to real IPs.
	if len(split.DNSDomains) > 0 {
		patterns := make([]string, 0, len(split.DNSDomains))
		for _, d := range split.DNSDomains {
			d = strings.TrimSpace(strings.TrimPrefix(d, "."))
			if d != "" {
				patterns = append(patterns, "+."+d)
			}
		}
		dns["fake-ip-filter"] = mergeStringListInto(dns["fake-ip-filter"], patterns)
	}

	m["dns"] = dns
}

// mergeStringListInto appends add to an existing YAML list value (which may be
// []any, []string, or absent), de-duplicating while preserving order, and
// returns a []any suitable for writing back into the config map.
func mergeStringListInto(existing any, add []string) []any {
	seen := make(map[string]struct{})
	out := make([]any, 0)
	push := func(s string) {
		s = strings.TrimSpace(s)
		if s == "" {
			return
		}
		if _, dup := seen[s]; dup {
			return
		}
		seen[s] = struct{}{}
		out = append(out, s)
	}
	switch v := existing.(type) {
	case []any:
		for _, e := range v {
			if s, ok := e.(string); ok {
				push(s)
			}
		}
	case []string:
		for _, s := range v {
			push(s)
		}
	}
	for _, s := range add {
		push(s)
	}
	return out
}

func toAnyList(ss []string) []any {
	out := make([]any, 0, len(ss))
	for _, s := range ss {
		out = append(out, s)
	}
	return out
}
