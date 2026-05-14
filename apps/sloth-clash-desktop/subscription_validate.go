package main

import (
	"fmt"
	"os"
	"strings"
)

func validateDNSInvariants(m map[string]any) error {
	// Self-heal before validating so template/profile edge cases do not break connect.
	ensureDefaultDNSForTun(m)
	dns, ok := m["dns"].(map[string]any)
	if !ok || dns == nil {
		return nil
	}
	respectRules := false
	switch v := dns["respect-rules"].(type) {
	case bool:
		respectRules = v
	case string:
		s := strings.ToLower(strings.TrimSpace(v))
		respectRules = s == "true" || s == "1" || s == "yes" || s == "on"
	}
	if respectRules {
		switch v := dns["proxy-server-nameserver"].(type) {
		case []any:
			if len(v) > 0 {
				return nil
			}
		case []string:
			if len(v) > 0 {
				return nil
			}
		case string:
			if strings.TrimSpace(v) != "" {
				return nil
			}
		}
		// Final hard fallback: force defaults and accept.
		dns["proxy-server-nameserver"] = []any{"1.1.1.1", "8.8.8.8"}
		m["dns"] = dns
	}
	return nil
}

// validateProxyGroupRefs verifies every proxy-group reference (use / proxies)
// resolves to a known provider / proxy / group / builtin policy. Names are
// compared *verbatim* — same rule as clash-verge-rev's cleanup_proxy_groups —
// so trailing/leading whitespace baked into a subscription is respected on
// both sides of the comparison. Only the membership check uses TrimSpace to
// skip empty / whitespace-only tokens.
func validateProxyGroupRefs(m map[string]any) error {
	providerSet := map[string]bool{}
	if providers, ok := m["proxy-providers"].(map[string]any); ok {
		for k := range providers {
			if strings.TrimSpace(k) != "" {
				providerSet[k] = true
			}
		}
	}
	allowed := map[string]bool{
		"DIRECT":      true,
		"REJECT":      true,
		"REJECT-DROP": true,
		"PASS":        true,
	}
	if proxies, ok := m["proxies"].([]any); ok {
		for _, it := range proxies {
			switch v := it.(type) {
			case map[string]any:
				if n, _ := v["name"].(string); strings.TrimSpace(n) != "" {
					allowed[n] = true
				}
			case string:
				if strings.TrimSpace(v) != "" {
					allowed[v] = true
				}
			}
		}
	}
	if groups, ok := m["proxy-groups"].([]any); ok {
		for _, g := range groups {
			if gm, ok := g.(map[string]any); ok {
				if n, _ := gm["name"].(string); strings.TrimSpace(n) != "" {
					allowed[n] = true
				}
			}
		}
		for idx, g := range groups {
			gm, ok := g.(map[string]any)
			if !ok {
				continue
			}
			name, _ := gm["name"].(string)
			if useArr, ok := gm["use"].([]any); ok {
				for _, u := range useArr {
					if s, ok := u.(string); ok && strings.TrimSpace(s) != "" && !providerSet[s] {
						return fmt.Errorf("proxy-groups[%d] %q references unknown provider %q", idx, name, s)
					}
				}
			}
			if pArr, ok := gm["proxies"].([]any); ok {
				for _, p := range pArr {
					if s, ok := p.(string); ok && strings.TrimSpace(s) != "" && !allowed[s] {
						return fmt.Errorf("proxy-groups[%d] %q references unknown proxy/group %q", idx, name, s)
					}
				}
			}
		}
	}
	return nil
}

func validateFinalConfigSemantics(m map[string]any) error {
	if err := validateProxyGroupRefs(m); err != nil {
		return err
	}
	if err := validateRulePoliciesExist(m); err != nil {
		return err
	}
	if err := validateDNSInvariants(m); err != nil {
		return err
	}
	return nil
}

func cleanupUnusedProxyProviders(m map[string]any) {
	providers, ok := m["proxy-providers"].(map[string]any)
	if !ok || len(providers) == 0 {
		return
	}
	used := map[string]bool{}
	if groups, ok := m["proxy-groups"].([]any); ok {
		for _, g := range groups {
			gm, ok := g.(map[string]any)
			if !ok {
				continue
			}
			if arr, ok := gm["use"].([]any); ok {
				for _, it := range arr {
					if s, ok := it.(string); ok && strings.TrimSpace(s) != "" {
						used[strings.TrimSpace(s)] = true
					}
				}
			}
		}
	}
	for k := range providers {
		if !used[k] {
			delete(providers, k)
		}
	}
	m["proxy-providers"] = providers
}

// finalizeRuntimeConfigPipeline applies the same staged normalization pipeline used for every
// generated/edited config before persistence and preflight:
//  1. rules ordering
//  2. proxy-group reference cleanup
//  3. fallback-group pruning
//  4. runtime overlay (ports/secret/tun)
//  5. semantic validation
//  6. geodata fallback injection
func finalizeRuntimeConfigPipeline(
	m map[string]any,
	dataDir string,
	mixedPort, ctrlPort int,
	secret, traffic string,
	withExternalController bool,
	enableTun bool,
) error {
	if fixed, ok := normalizeEscapedUnicodeStrings(m).(map[string]any); ok {
		for k := range m {
			delete(m, k)
		}
		for k, v := range fixed {
			m[k] = v
		}
	}
	normalizeRulesMatchLast(m)
	normalizeProxyGroupRefs(m)
	pruneFallbackAutoManualIfCustom(m)
	cleanupUnusedProxyProviders(m)
	overlaySlothRuntimeOnMap(m, mixedPort, ctrlPort, secret, traffic, withExternalController, enableTun)
	if err := validateFinalConfigSemantics(m); err != nil {
		return err
	}
	overlayBundledGeoData(m, dataDir)
	return nil
}

// overlayBundledGeoData applies the minimal config nudges that let Mihomo
// find our shipped geo data without going to the network. It deliberately
// stays light-touch, mirroring clash-verge-rev's approach:
//
//   - `ensureGeoInDataDir` (called before the pipeline runs) drops
//     `geoip.dat` / `geosite.dat` / `Country.mmdb` into the profile's
//     workdir root. Mihomo's default lookup path is exactly that, so we do
//     NOT need to rewrite the `geoip:` / `geosite:` config fields — mihomo
//     finds them on its own.
//   - We set `geodata-mode: true` only if the user has not explicitly set
//     it themselves (V2Ray-style .dat files are what we ship; mmdb mode
//     would fail). User overrides are respected so power users running on
//     MaxMind .mmdb keep their setup.
//   - We disable `geo-auto-update` because we do not have a known-good
//     mirror we can trust globally — the default geox-url (cdn.jsdelivr
//     or mirror.ghproxy) is the very thing that DNS-fails for users behind
//     restrictive networks and stalls preflight. Auto-update can be turned
//     back on by the user in the merge template if they have a good mirror.
//   - We intentionally do NOT delete `geox-url` from the config. Mihomo
//     uses it only as a fallback when the local files are missing, or for
//     the on-demand "Update GeoData" action. With local files present and
//     auto-update off, geox-url is harmless to keep, and dropping it would
//     prevent the user from using a manual refresh if they want fresher
//     data than what the app ships.
//
// The behaviour: if local geo files exist (the common case after the first
// successful Connect or after `ensureGeoInDataDir` ran), mihomo uses them
// and never touches the network. If they are somehow missing, mihomo falls
// back to its own download path with whatever geox-url is in the config —
// which may also fail, but we are no longer in a worse position than the
// user's plain mihomo would be.
func overlayBundledGeoData(m map[string]any, dataDir string) {
	_ = dataDir // kept for symmetry; we no longer build paths from it
	if _, has := m["geodata-mode"]; !has {
		m["geodata-mode"] = true
	}
	if _, has := m["geo-auto-update"]; !has {
		m["geo-auto-update"] = false
	}
}

func fileExists(p string) bool {
	_, err := os.Stat(p)
	return err == nil
}
