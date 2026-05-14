package main

import (
	"fmt"
	"os"
	"path/filepath"
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
	mergeBundledGeoIfMissing(m, dataDir)
	return nil
}

func mergeBundledGeoIfMissing(m map[string]any, dataDir string) {
	if _, has := m["geoip"]; has {
		return
	}
	if _, has := m["geosite"]; has {
		return
	}
	if _, has := m["geox-url"]; has {
		return
	}
	geoDir := filepath.Join(dataDir, "geo")
	geoIP := filepath.Join(geoDir, "geoip.dat")
	if _, err := os.Stat(geoIP); err != nil {
		return
	}
	m["geoip"] = filepath.ToSlash(geoIP)
	m["geo-auto-update"] = false
	if gs := filepath.Join(geoDir, "geosite.dat"); fileExists(gs) {
		m["geosite"] = filepath.ToSlash(gs)
	}
}

func fileExists(p string) bool {
	_, err := os.Stat(p)
	return err == nil
}
