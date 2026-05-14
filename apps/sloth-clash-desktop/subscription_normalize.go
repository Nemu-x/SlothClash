package main

import (
	"fmt"
	"strings"
)

// ensureGlobalProxyGroup prepends a GLOBAL selector when missing so PATCH mode global +
// PUT /proxies/GLOBAL works (many published profiles omit an explicit GLOBAL group).
func ensureGlobalProxyGroup(m map[string]any) {
	raw, ok := m["proxy-groups"]
	if !ok || raw == nil {
		return
	}
	arr, ok := raw.([]any)
	if !ok || len(arr) == 0 {
		return
	}
	for _, g := range arr {
		gm, ok := g.(map[string]any)
		if !ok {
			continue
		}
		name, _ := gm["name"].(string)
		if strings.EqualFold(strings.TrimSpace(name), "GLOBAL") {
			return
		}
	}

	seen := map[string]bool{"DIRECT": true, "REJECT": true}
	outNames := []string{"DIRECT", "REJECT"}
	for _, g := range arr {
		gm, ok := g.(map[string]any)
		if !ok {
			continue
		}
		n, _ := gm["name"].(string)
		// Preserve the group name verbatim (trailing/leading whitespace included)
		// so GLOBAL's reference matches the group's declared name byte-for-byte.
		trimmed := strings.TrimSpace(n)
		if trimmed == "" || strings.EqualFold(trimmed, "GLOBAL") {
			continue
		}
		if !seen[n] {
			seen[n] = true
			outNames = append(outNames, n)
		}
	}

	global := map[string]any{
		"name":    "GLOBAL",
		"type":    "select",
		"proxies": outNames,
	}
	m["proxy-groups"] = append([]any{global}, arr...)
}

func validateRulePoliciesExist(m map[string]any) error {
	known := map[string]bool{
		"DIRECT":      true,
		"REJECT":      true,
		"REJECT-DROP": true,
		"PASS":        true,
		"GLOBAL":      true,
	}
	if groups, ok := m["proxy-groups"].([]any); ok {
		for _, g := range groups {
			gm, ok := g.(map[string]any)
			if !ok {
				continue
			}
			name, _ := gm["name"].(string)
			name = strings.TrimSpace(name)
			if name != "" {
				known[name] = true
			}
		}
	}
	rules, ok := m["rules"].([]any)
	if !ok {
		return nil
	}
	for idx, r := range rules {
		line, ok := r.(string)
		if !ok {
			continue
		}
		policy := extractRulePolicyToken(line)
		if policy == "" {
			continue
		}
		if !known[policy] {
			return fmt.Errorf(
				"rules[%d] references unknown policy %q in rule %q",
				idx,
				policy,
				strings.TrimSpace(line),
			)
		}
	}
	return nil
}

func splitRuleCSV(rule string) []string {
	s := strings.TrimSpace(rule)
	if s == "" {
		return nil
	}
	out := make([]string, 0, 8)
	var b strings.Builder
	depth := 0
	for _, ch := range s {
		switch ch {
		case '(':
			depth++
			b.WriteRune(ch)
		case ')':
			if depth > 0 {
				depth--
			}
			b.WriteRune(ch)
		case ',':
			if depth == 0 {
				out = append(out, strings.TrimSpace(b.String()))
				b.Reset()
				continue
			}
			b.WriteRune(ch)
		default:
			b.WriteRune(ch)
		}
	}
	if b.Len() > 0 {
		out = append(out, strings.TrimSpace(b.String()))
	}
	return out
}

func isRuleOptionToken(token string) bool {
	t := strings.ToLower(strings.TrimSpace(token))
	return t == "no-resolve" || strings.HasPrefix(t, "src=") || strings.HasPrefix(t, "dst=")
}

func extractRulePolicyToken(rule string) string {
	parts := splitRuleCSV(rule)
	if len(parts) < 2 {
		return ""
	}
	// Skip rule type + payload; last non-option token is expected outbound policy.
	for i := len(parts) - 1; i >= 2; i-- {
		token := strings.TrimSpace(parts[i])
		if token == "" || isRuleOptionToken(token) {
			continue
		}
		return token
	}
	// MATCH,DIRECT-like rules have no payload section.
	if len(parts) == 2 {
		head := strings.ToUpper(strings.TrimSpace(parts[0]))
		if head == "MATCH" || head == "FINAL" {
			return strings.TrimSpace(parts[1])
		}
	}
	return ""
}

// normalizeProxyGroupRefs keeps proxy-group references valid after template merges.
// It sanitizes both:
//   - `use`: only existing proxy-providers remain
//   - `proxies`: only valid proxy/group/provider/builtin names remain
//
// Matches clash-verge-rev's cleanup_proxy_groups exactly: names are compared
// and preserved *verbatim* (no whitespace trimming). This is important because
// some subscriptions intentionally include trailing/leading whitespace in a
// proxy name and reference the same whitespace-sensitive string from a group;
// trimming only the reference side (but not the proxy.name field) would break
// the link and cause Mihomo to report "'<name>' not found".
func normalizeProxyGroupRefs(m map[string]any) {
	rawProviders, ok := m["proxy-providers"]
	if !ok || rawProviders == nil {
		return
	}
	providerMap, ok := rawProviders.(map[string]any)
	if !ok || len(providerMap) == 0 {
		return
	}
	providerNames := make([]string, 0, len(providerMap))
	providerSet := make(map[string]bool, len(providerMap))
	for name := range providerMap {
		if strings.TrimSpace(name) == "" {
			continue
		}
		providerSet[name] = true
		providerNames = append(providerNames, name)
	}
	if len(providerNames) == 0 {
		return
	}

	proxySet := map[string]bool{}
	if rawProxies, ok := m["proxies"].([]any); ok {
		for _, it := range rawProxies {
			switch v := it.(type) {
			case map[string]any:
				if n, _ := v["name"].(string); strings.TrimSpace(n) != "" {
					proxySet[n] = true
				}
			case string:
				if strings.TrimSpace(v) != "" {
					proxySet[v] = true
				}
			}
		}
	}

	groups, ok := m["proxy-groups"].([]any)
	if !ok || len(groups) == 0 {
		return
	}
	groupSet := map[string]bool{}
	for _, g := range groups {
		gm, ok := g.(map[string]any)
		if !ok {
			continue
		}
		if n, _ := gm["name"].(string); strings.TrimSpace(n) != "" {
			groupSet[n] = true
		}
	}
	allowed := map[string]bool{
		"DIRECT":      true,
		"REJECT":      true,
		"REJECT-DROP": true,
		"PASS":        true,
	}
	for name := range providerSet {
		allowed[name] = true
	}
	for name := range proxySet {
		allowed[name] = true
	}
	for name := range groupSet {
		allowed[name] = true
	}

	for i := range groups {
		gm, ok := groups[i].(map[string]any)
		if !ok {
			continue
		}
		hasValidProvider := false
		useRaw, hasUse := gm["use"]
		if !hasUse || useRaw == nil {
		} else if useArr, ok := useRaw.([]any); ok {
			filtered := make([]any, 0, len(useArr))
			for _, item := range useArr {
				s, ok := item.(string)
				if !ok {
					continue
				}
				if strings.TrimSpace(s) == "" {
					continue
				}
				if providerSet[s] {
					filtered = append(filtered, s)
					hasValidProvider = true
				}
			}
			if len(filtered) == 0 {
				// Keep provider-backed group valid instead of failing startup.
				for _, p := range providerNames {
					filtered = append(filtered, p)
				}
				hasValidProvider = len(filtered) > 0
			}
			gm["use"] = filtered
		}

		if proxiesRaw, hasProxies := gm["proxies"]; hasProxies && proxiesRaw != nil {
			if proxiesArr, ok := proxiesRaw.([]any); ok {
				out := make([]any, 0, len(proxiesArr))
				for _, item := range proxiesArr {
					s, ok := item.(string)
					if !ok {
						continue
					}
					if strings.TrimSpace(s) == "" {
						continue
					}
					if allowed[s] {
						out = append(out, s)
					}
				}
				if len(out) == 0 {
					if hasValidProvider {
						out = append(out, "DIRECT")
					} else if allowed["DIRECT"] {
						out = append(out, "DIRECT")
					}
				}
				gm["proxies"] = out
			}
		}
		groups[i] = gm
	}
	m["proxy-groups"] = groups
}

func safeGroupNameForRules(groups []any) string {
	best := ""
	for _, g := range groups {
		gm, ok := g.(map[string]any)
		if !ok {
			continue
		}
		name, _ := gm["name"].(string)
		name = strings.TrimSpace(name)
		if name == "" {
			continue
		}
		up := strings.ToUpper(name)
		if up == "DIRECT" || up == "REJECT" || up == "REJECT-DROP" || up == "PASS" || up == "GLOBAL" {
			continue
		}
		typ, _ := gm["type"].(string)
		t := strings.ToLower(strings.ReplaceAll(strings.TrimSpace(typ), "-", ""))
		if t == "urltest" || t == "fallback" || t == "loadbalance" {
			return name
		}
		if best == "" {
			best = name
		}
	}
	return best
}

func rewriteMatchRuleTarget(m map[string]any, from, to string) {
	rules, ok := m["rules"].([]any)
	if !ok || len(rules) == 0 {
		return
	}
	from = strings.TrimSpace(from)
	to = strings.TrimSpace(to)
	if from == "" || to == "" || strings.EqualFold(from, to) {
		return
	}
	for i, r := range rules {
		line, ok := r.(string)
		if !ok {
			continue
		}
		parts := strings.Split(line, ",")
		if len(parts) < 2 {
			continue
		}
		if !strings.EqualFold(strings.TrimSpace(parts[0]), "MATCH") {
			continue
		}
		policyIdx := len(parts) - 1
		last := strings.TrimSpace(parts[policyIdx])
		if strings.EqualFold(last, "no-resolve") && len(parts) >= 3 {
			policyIdx = len(parts) - 2
			last = strings.TrimSpace(parts[policyIdx])
		}
		if !strings.EqualFold(last, from) {
			continue
		}
		parts[policyIdx] = to
		rules[i] = strings.Join(parts, ",")
	}
	m["rules"] = rules
}

// pruneFallbackAutoManualIfCustom removes built-in fallback groups once profile/template
// already defines real groups. This keeps output closer to Verge behavior and avoids stale
// fallback routing references leaking into final config.
func pruneFallbackAutoManualIfCustom(m map[string]any) {
	rawGroups, ok := m["proxy-groups"].([]any)
	if !ok || len(rawGroups) == 0 {
		return
	}
	isDefaultAuto := func(gm map[string]any) bool {
		name, _ := gm["name"].(string)
		typ, _ := gm["type"].(string)
		return strings.EqualFold(strings.TrimSpace(name), "Auto") &&
			strings.EqualFold(strings.TrimSpace(typ), "url-test")
	}
	isDefaultManual := func(gm map[string]any) bool {
		name, _ := gm["name"].(string)
		typ, _ := gm["type"].(string)
		return strings.EqualFold(strings.TrimSpace(name), "Manual") &&
			strings.EqualFold(strings.TrimSpace(typ), "select")
	}

	hasCustom := false
	autoIdx := -1
	manualIdx := -1
	for i, g := range rawGroups {
		gm, ok := g.(map[string]any)
		if !ok {
			continue
		}
		switch {
		case isDefaultAuto(gm):
			autoIdx = i
		case isDefaultManual(gm):
			manualIdx = i
		default:
			hasCustom = true
		}
	}
	if !hasCustom {
		return
	}
	if autoIdx < 0 && manualIdx < 0 {
		return
	}

	filtered := make([]any, 0, len(rawGroups))
	for i, g := range rawGroups {
		if i == autoIdx || i == manualIdx {
			continue
		}
		filtered = append(filtered, g)
	}
	if len(filtered) == 0 {
		return
	}
	repl := safeGroupNameForRules(filtered)
	if repl != "" {
		// Both synthetic group names (Auto, Manual) used by the bare
		// fallback must be rewritten to a real custom group when we prune
		// them — otherwise the terminal MATCH rule dangles on a policy the
		// validator can no longer resolve. writeRuntimeConfig now emits
		// MATCH,Manual (select group) so manual node selection works in
		// Rule mode; keep rewriting MATCH,Auto too for back-compat with
		// configs / merge templates still using the old target.
		if autoIdx >= 0 {
			rewriteMatchRuleTarget(m, "Auto", repl)
		}
		if manualIdx >= 0 {
			rewriteMatchRuleTarget(m, "Manual", repl)
		}
	}
	m["proxy-groups"] = filtered
}

// normalizeRulesMatchLast ensures terminal MATCH rules are placed last.
// If MATCH appears earlier, appended user rules become unreachable.
func normalizeRulesMatchLast(m map[string]any) {
	rules, ok := m["rules"].([]any)
	if !ok || len(rules) == 0 {
		return
	}
	nonMatch := make([]any, 0, len(rules))
	match := make([]any, 0, 2)
	for _, it := range rules {
		s, ok := it.(string)
		if !ok {
			nonMatch = append(nonMatch, it)
			continue
		}
		parts := strings.Split(s, ",")
		head := ""
		if len(parts) > 0 {
			head = strings.ToUpper(strings.TrimSpace(parts[0]))
		}
		if head == "MATCH" {
			match = append(match, it)
			continue
		}
		nonMatch = append(nonMatch, it)
	}
	if len(match) == 0 {
		return
	}
	out := append(nonMatch, match...)
	m["rules"] = out
}
