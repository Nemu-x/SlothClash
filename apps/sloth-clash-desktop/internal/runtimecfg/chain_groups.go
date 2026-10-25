package runtimecfg

import "strings"

// Proxy groups of `type: chain` (core patch 0006) dial every hop through the
// one before it. The generic group cleanup (normalizeProxyGroupRefs) drops
// unknown members and falls back to DIRECT, which is right for a selector and
// wrong for a chain: a dropped hop silently shortens the chain, so traffic skips
// a relay the config asked for. Chains are therefore never rewritten hop by hop;
// a chain the core would refuse becomes a REJECT selector of the same name, so
// rules and groups that use it stay valid, it fails closed, and one bad chain
// does not fail the whole subscription.

const chainGroupType = "chain"

// chainBuiltinHops are the proxies that exist without being configured (the
// core's list in config/chain.go); only the first hop may be one of them.
var chainBuiltinHops = map[string]bool{
	"DIRECT": true, "REJECT": true, "REJECT-DROP": true, "COMPATIBLE": true, "PASS": true, "PASS-RULE": true, "GLOBAL": true,
}

// chainUnsupportedKeys are group keys a chain refuses: its hops are listed in
// `proxies`, in order.
var chainUnsupportedKeys = []string{"use", "include-all", "include-all-proxies", "include-all-providers", "filter", "exclude-filter", "exclude-type"}

func isChainGroup(gm map[string]any) bool {
	t, _ := gm["type"].(string)
	return t == chainGroupType
}

// neutralizeBrokenChainGroups replaces every chain group the core would refuse
// for a reason visible here (too few hops, an unknown, repeated or self hop, a
// built-in past the first hop, an unsupported key, or a hop that is itself a
// neutralised chain) with `select [REJECT]`. Loops and dialer-proxy conflicts
// are left to the core, whose error names them.
func neutralizeBrokenChainGroups(m map[string]any) {
	groups, ok := m["proxy-groups"].([]any)
	if !ok || len(groups) == 0 {
		return
	}
	known := map[string]bool{}
	if proxies, ok := m["proxies"].([]any); ok {
		for _, it := range proxies {
			if pm, ok := it.(map[string]any); ok {
				if n, _ := pm["name"].(string); n != "" {
					known[n] = true
				}
			}
		}
	}
	for _, g := range groups {
		if gm, ok := g.(map[string]any); ok {
			if n, _ := gm["name"].(string); n != "" {
				known[n] = true
			}
		}
	}

	neutralized := map[string]string{} // name -> reason
	// A neutralised chain used as a later hop of another chain breaks that one
	// too (its only member is REJECT), so repeat until nothing changes.
	for changed := true; changed; {
		changed = false
		for i, g := range groups {
			gm, ok := g.(map[string]any)
			if !ok || !isChainGroup(gm) {
				continue
			}
			name, _ := gm["name"].(string)
			reason := chainProblem(gm, name, known, neutralized)
			if reason == "" {
				continue
			}
			groups[i] = map[string]any{"name": name, "type": "select", "proxies": []any{"REJECT"}}
			neutralized[name] = reason
			changed = true
		}
	}
	if len(neutralized) == 0 {
		return
	}
	m["proxy-groups"] = groups
	debugLog("config", "CHAIN-1", "chain_groups.go:neutralizeBrokenChainGroups",
		"chain groups the core would refuse now route to REJECT",
		map[string]any{"chains": neutralized})
}

func chainProblem(gm map[string]any, name string, known map[string]bool, neutralized map[string]string) string {
	for _, k := range chainUnsupportedKeys {
		if _, ok := gm[k]; ok {
			return "`" + k + "` is not supported in a chain"
		}
	}
	raw, _ := gm["proxies"].([]any)
	hops := make([]string, 0, len(raw))
	for _, it := range raw {
		s, ok := it.(string)
		if !ok || strings.TrimSpace(s) == "" {
			return "a hop is not a name"
		}
		hops = append(hops, s)
	}
	if len(hops) < 2 {
		return "needs at least two hops"
	}
	seen := map[string]bool{}
	for i, hop := range hops {
		switch {
		case hop == name:
			return "the chain is one of its own hops"
		case seen[hop]:
			return "hop " + hop + " appears twice"
		case chainBuiltinHops[hop]:
			if i > 0 {
				return "built-in " + hop + " can only be the first hop"
			}
		case !known[hop]:
			return "hop " + hop + " not found"
		case i > 0 && neutralized[hop] != "":
			return "hop " + hop + " is a broken chain"
		}
		seen[hop] = true
	}
	return ""
}
