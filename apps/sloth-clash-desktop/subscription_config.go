package main

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

	"gopkg.in/yaml.v3"
)

const tunDefaultDNSYAML = `dns:
  enable: true
  listen: 0.0.0.0:1053
  ipv6: true
  enhanced-mode: fake-ip
  fake-ip-range: 198.18.0.1/16
  use-hosts: true
  default-nameserver:
    - 1.1.1.1
    - 8.8.8.8
  nameserver:
    - https://1.1.1.1/dns-query
    - tls://8.8.8.8:853
`

func fetchSubscriptionBody(ctx context.Context, rawURL string) ([]byte, error) {
	norm, err := normalizeSubscriptionURL(rawURL)
	if err != nil {
		return nil, err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, norm, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("User-Agent", "clash.meta/mihomo; SlothClash/1.0")

	client := &http.Client{
		Timeout: 50 * time.Second,
		CheckRedirect: func(_ *http.Request, via []*http.Request) error {
			if len(via) >= 12 {
				return errors.New("too many redirects")
			}
			return nil
		},
	}

	resp, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		b, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
		return nil, fmt.Errorf("subscription HTTP %d: %s", resp.StatusCode, strings.TrimSpace(string(b)))
	}
	return io.ReadAll(io.LimitReader(resp.Body, 6<<20))
}

func parseClashDocToMap(b []byte) (map[string]any, error) {
	b = bytes.TrimSpace(b)
	if len(b) == 0 {
		return nil, errors.New("empty subscription body")
	}
	b = bytes.TrimPrefix(b, []byte{0xEF, 0xBB, 0xBF})

	var m map[string]any
	err := yaml.Unmarshal(b, &m)
	if err == nil && len(m) > 0 {
		return m, nil
	}
	if dec, derr := decodeBase64Flexible(strings.TrimSpace(string(b))); derr == nil && len(dec) > 0 {
		dec = bytes.TrimSpace(dec)
		dec = bytes.TrimPrefix(dec, []byte{0xEF, 0xBB, 0xBF})
		var m2 map[string]any
		if err2 := yaml.Unmarshal(dec, &m2); err2 == nil && len(m2) > 0 {
			return m2, nil
		}
	}
	if err != nil {
		return nil, err
	}
	return nil, errors.New("invalid clash yaml mapping")
}

// subscriptionDocIsFullProfile reports whether the downloaded document should be used as the
// main mihomo config (Verge-style full profile) instead of Sloth's minimal proxy-provider wrapper.
func subscriptionDocIsFullProfile(m map[string]any) bool {
	if m == nil {
		return false
	}
	if _, ok := m["rule-providers"]; ok {
		return true
	}
	raw, ok := m["rules"]
	if !ok || raw == nil {
		return false
	}
	arr, ok := raw.([]any)
	return ok && len(arr) > 0
}

func mergeTunFromYAMLString(m map[string]any, fragment string) {
	var wrap map[string]any
	if err := yaml.Unmarshal([]byte(fragment), &wrap); err != nil {
		return
	}
	if t, ok := wrap["tun"].(map[string]any); ok {
		m["tun"] = t
	}
}

func ensureDefaultDNSForTun(m map[string]any) {
	if _, ok := m["dns"]; ok {
		return
	}
	var wrap map[string]any
	if err := yaml.Unmarshal([]byte(tunDefaultDNSYAML), &wrap); err != nil {
		return
	}
	if d, ok := wrap["dns"].(map[string]any); ok {
		m["dns"] = d
	}
}

func overlaySlothRuntimeOnMap(m map[string]any, mixedPort, ctrlPort int, secret, traffic string, withExternalController bool) {
	m["mixed-port"] = mixedPort
	m["socks-port"] = 0
	m["port"] = 0

	if withExternalController && ctrlPort > 0 {
		m["external-controller"] = fmt.Sprintf("127.0.0.1:%d", ctrlPort)
	} else {
		delete(m, "external-controller")
	}
	m["secret"] = secret
	m["allow-lan"] = false

	if strings.TrimSpace(traffic) == "tun" {
		mergeTunFromYAMLString(m, tunBlockForTraffic("tun"))
		ensureDefaultDNSForTun(m)
	} else {
		mergeTunFromYAMLString(m, tunBlockForTraffic("proxy"))
	}
}

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
		n = strings.TrimSpace(n)
		if n == "" || strings.EqualFold(n, "GLOBAL") {
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
	optFlags := map[string]bool{
		"no-resolve": true,
	}
	for idx, r := range rules {
		line, ok := r.(string)
		if !ok {
			continue
		}
		parts := strings.Split(line, ",")
		if len(parts) < 2 {
			continue
		}
		candidate := strings.TrimSpace(parts[len(parts)-1])
		if optFlags[strings.ToLower(candidate)] && len(parts) >= 3 {
			candidate = strings.TrimSpace(parts[len(parts)-2])
		}
		if candidate == "" {
			continue
		}
		if !known[candidate] {
			return fmt.Errorf("rules[%d] [%s] error: proxy [%s] not found", idx, line, candidate)
		}
	}
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

func tryWriteMergedFullProfile(dataDir, subURL, extendTemplate, proxyTemplate, rulesTemplate string, ctrlPort, mixedPort int, secret, traffic string, withExternalController bool) (bool, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 55*time.Second)
	defer cancel()

	b, err := fetchSubscriptionBody(ctx, subURL)
	if err != nil || len(bytes.TrimSpace(b)) == 0 {
		return false, nil
	}
	doc, err := parseClashDocToMap(b)
	if err != nil || doc == nil {
		return false, nil
	}
	if !subscriptionDocIsFullProfile(doc) {
		return false, nil
	}

	if err := applyProfileMergeTemplate(doc, extendTemplate); err != nil {
		return false, err
	}
	if err := applyProfileMergeTemplate(doc, proxyTemplate); err != nil {
		return false, err
	}
	if err := applyProfileMergeTemplate(doc, rulesTemplate); err != nil {
		return false, err
	}

	overlaySlothRuntimeOnMap(doc, mixedPort, ctrlPort, secret, traffic, withExternalController)
	ensureGlobalProxyGroup(doc)
	if err := validateRulePoliciesExist(doc); err != nil {
		return false, err
	}
	mergeBundledGeoIfMissing(doc, dataDir)

	out, err := yaml.Marshal(doc)
	if err != nil {
		return false, err
	}
	cfgPath := filepath.Join(dataDir, "config.yaml")
	if err := os.WriteFile(cfgPath, out, 0o644); err != nil {
		return false, err
	}
	return true, nil
}
