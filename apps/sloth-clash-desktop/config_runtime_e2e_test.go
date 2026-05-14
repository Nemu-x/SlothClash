package main

import (
	"embed"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"gopkg.in/yaml.v3"
)

func readYAMLMapForTest(t *testing.T, p string) map[string]any {
	t.Helper()
	b, err := os.ReadFile(p)
	if err != nil {
		t.Fatalf("read config failed: %v", err)
	}
	var out map[string]any
	if err := yaml.Unmarshal(b, &out); err != nil {
		t.Fatalf("yaml unmarshal failed: %v", err)
	}
	return out
}

func TestE2EWriteRuntimeConfigTunAndProxyModes(t *testing.T) {
	t.Parallel()
	var b embed.FS
	a := NewApp(b)

	// Verge-rev alignment: the YAML reflects the enableTun argument verbatim.
	// Writing a tun profile with enableTun=true must produce tun.enable=true in
	// the emitted file so PUT /configs?force=true can bring wintun up without
	// a follow-up PATCH.
	tunDir := t.TempDir()
	if err := a.writeRuntimeConfig(
		tunDir,
		"https://example.com/sub",
		"",
		"",
		"",
		9090,
		7890,
		"secret",
		"tun",
		true,
		true,
	); err != nil {
		t.Fatalf("writeRuntimeConfig(tun, enableTun=true) failed: %v", err)
	}
	tunCfg := readYAMLMapForTest(t, filepath.Join(tunDir, "config.yaml"))
	tunBlock, ok := tunCfg["tun"].(map[string]any)
	if !ok {
		t.Fatalf("tun block missing")
	}
	enable, _ := tunBlock["enable"].(bool)
	if !enable {
		t.Fatalf("tun.enable expected true when enableTun=true (verge-rev parity)")
	}
	if stack, _ := tunBlock["stack"].(string); strings.TrimSpace(stack) == "" {
		t.Fatalf("tun.stack hardening must be present, got: %#v", tunBlock)
	}
	if _, has := tunBlock["dns-hijack"]; !has {
		t.Fatalf("tun.dns-hijack hardening must be present, got: %#v", tunBlock)
	}
	dns, ok := tunCfg["dns"].(map[string]any)
	if !ok {
		t.Fatalf("dns block missing when enableTun=true (fake-ip overlay required)")
	}
	psn, ok := dns["proxy-server-nameserver"].([]any)
	if !ok || len(psn) == 0 {
		t.Fatalf("dns.proxy-server-nameserver must be non-empty when TUN is enabled")
	}

	// Same profile but simulating "disconnected + traffic=tun": YAML must
	// have tun.enable=false so Mihomo boots idle, without thrashing wintun.
	idleDir := t.TempDir()
	if err := a.writeRuntimeConfig(
		idleDir,
		"https://example.com/sub",
		"",
		"",
		"",
		9090,
		7890,
		"secret",
		"tun",
		true,
		false,
	); err != nil {
		t.Fatalf("writeRuntimeConfig(tun, enableTun=false) failed: %v", err)
	}
	idleCfg := readYAMLMapForTest(t, filepath.Join(idleDir, "config.yaml"))
	idleTun, _ := idleCfg["tun"].(map[string]any)
	if enable, _ := idleTun["enable"].(bool); enable {
		t.Fatalf("tun.enable must be false when enableTun=false even for tun profile")
	}
	if stack, _ := idleTun["stack"].(string); strings.TrimSpace(stack) == "" {
		t.Fatalf("hardening must stay in idle YAML so Connect brings wintun up without a second reload")
	}

	proxyDir := t.TempDir()
	if err := a.writeRuntimeConfig(
		proxyDir,
		"https://example.com/sub",
		"",
		"",
		"",
		9090,
		7890,
		"secret",
		"proxy",
		true,
		false,
	); err != nil {
		t.Fatalf("writeRuntimeConfig(proxy) failed: %v", err)
	}
	proxyCfg := readYAMLMapForTest(t, filepath.Join(proxyDir, "config.yaml"))
	proxyTun, ok := proxyCfg["tun"].(map[string]any)
	if !ok {
		t.Fatalf("tun block missing in proxy mode")
	}
	proxyEnable, _ := proxyTun["enable"].(bool)
	if proxyEnable {
		t.Fatalf("tun.enable expected false for proxy mode")
	}
}

func TestE2EWriteRuntimeConfigAppliesRulesAndProxyTemplates(t *testing.T) {
	t.Parallel()
	var b embed.FS
	a := NewApp(b)

	proxyTemplate := `
append:
  proxy-groups:
    - name: Custom
      type: select
      proxies:
        - DIRECT
`
	rulesTemplate := `
append:
  rules:
    - DOMAIN,example.com,Custom
`
	dir := t.TempDir()
	if err := a.writeRuntimeConfig(
		dir,
		"https://example.com/sub",
		"",
		proxyTemplate,
		rulesTemplate,
		9090,
		7890,
		"secret",
		"tun",
		true,
		true,
	); err != nil {
		t.Fatalf("writeRuntimeConfig with templates failed: %v", err)
	}
	cfg := readYAMLMapForTest(t, filepath.Join(dir, "config.yaml"))

	foundCustomGroup := false
	if groups, ok := cfg["proxy-groups"].([]any); ok {
		for _, g := range groups {
			gm, ok := g.(map[string]any)
			if !ok {
				continue
			}
			name, _ := gm["name"].(string)
			if strings.TrimSpace(name) == "Custom" {
				foundCustomGroup = true
				break
			}
		}
	}
	if !foundCustomGroup {
		t.Fatalf("expected Custom group in generated config")
	}

	foundCustomRule := false
	if rules, ok := cfg["rules"].([]any); ok {
		for _, r := range rules {
			s, _ := r.(string)
			if strings.Contains(s, "DOMAIN,example.com,Custom") {
				foundCustomRule = true
				break
			}
		}
	}
	if !foundCustomRule {
		t.Fatalf("expected DOMAIN,example.com,Custom rule in generated config")
	}
}

func TestIntegrationPreflightWithRealCoreBinary(t *testing.T) {
	bin := strings.TrimSpace(os.Getenv("SLOTH_MIHOMO_BIN"))
	if bin == "" {
		t.Skip("set SLOTH_MIHOMO_BIN to run real-core preflight integration")
	}
	dir := t.TempDir()
	validCfg := `
mixed-port: 7890
mode: rule
proxy-groups:
  - name: Main
    type: select
    proxies: [DIRECT]
rules:
  - MATCH,DIRECT
`
	if err := os.WriteFile(filepath.Join(dir, "config.yaml"), []byte(validCfg), 0o644); err != nil {
		t.Fatalf("write valid config failed: %v", err)
	}
	if err := runConfigPreflight(bin, dir); err != nil {
		t.Fatalf("preflight should pass with valid config: %v", err)
	}

	badDir := t.TempDir()
	if err := os.WriteFile(filepath.Join(badDir, "config.yaml"), []byte("dns:\n  -"), 0o644); err != nil {
		t.Fatalf("write invalid config failed: %v", err)
	}
	if err := runConfigPreflight(bin, badDir); err == nil {
		t.Fatalf("preflight should fail with malformed yaml")
	}
}

func TestE2ERuntimeTunProfileKeepsSnifferAndUDPSemantics(t *testing.T) {
	t.Parallel()
	subscription := `
mixed-port: 7890
mode: rule
dns:
  respect-rules: true
  nameserver:
    - 1.1.1.1
proxies:
  - name: udp-node
    type: vless
    server: edge.example.test
    port: 443
    uuid: 00000000-0000-0000-0000-000000000000
    udp: true
proxy-groups:
  - name: Main
    type: select
    proxies:
      - udp-node
      - DIRECT
sniffer:
  enable: true
  sniff:
    QUIC:
      ports: [443, 8443]
rules:
  - MATCH,Main
`
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(subscription))
	}))
	defer srv.Close()

	dir := t.TempDir()
	outcome, err := tryWriteMergedFullProfile(
		dir,
		srv.URL,
		"",
		"",
		"",
		9090,
		7890,
		"secret",
		"tun",
		true,
		true,
	)
	if err != nil {
		t.Fatalf("tryWriteMergedFullProfile(tun profile) failed: %v", err)
	}
	if outcome != pipelineOK {
		t.Fatalf("expected full-profile path for tun profile e2e, got outcome=%s", outcome)
	}
	cfg := readYAMLMapForTest(t, filepath.Join(dir, "config.yaml"))

	dns, _ := cfg["dns"].(map[string]any)
	if dns == nil {
		t.Fatalf("dns block missing")
	}
	if psn, _ := dns["proxy-server-nameserver"].([]any); len(psn) == 0 {
		t.Fatalf("dns.proxy-server-nameserver should be auto-healed in tun mode")
	}
	sniffer, _ := cfg["sniffer"].(map[string]any)
	if sniffer == nil {
		t.Fatalf("sniffer block should be preserved")
	}
	proxies, _ := cfg["proxies"].([]any)
	if len(proxies) == 0 {
		t.Fatalf("proxies list missing")
	}
	first, _ := proxies[0].(map[string]any)
	udp, _ := first["udp"].(bool)
	if !udp {
		t.Fatalf("expected udp=true preserved on proxy")
	}
}

func TestE2ERuntimeFinalizerRepairsBrokenGroupRefs(t *testing.T) {
	t.Parallel()
	subscription := `
mode: rule
proxy-providers:
  provider-a:
    type: file
    path: ./providers/provider-a.yaml
  provider-z:
    type: file
    path: ./providers/provider-z.yaml
proxy-groups:
  - name: Main
    type: select
    use: [provider-a]
    proxies: [MissingNode, MissingGroup]
rules:
  - MATCH,Main
`
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(subscription))
	}))
	defer srv.Close()

	dir := t.TempDir()
	outcome, err := tryWriteMergedFullProfile(
		dir,
		srv.URL,
		"",
		"",
		"",
		9090,
		7890,
		"secret",
		"tun",
		true,
		true,
	)
	if err != nil {
		t.Fatalf("tryWriteMergedFullProfile with broken refs should self-heal, got: %v", err)
	}
	if outcome != pipelineOK {
		t.Fatalf("expected full-profile path for broken ref repair e2e, got outcome=%s", outcome)
	}
	cfg := readYAMLMapForTest(t, filepath.Join(dir, "config.yaml"))

	providers, _ := cfg["proxy-providers"].(map[string]any)
	if len(providers) != 1 || providers["provider-a"] == nil {
		t.Fatalf("unused provider should be removed and used provider kept, got: %#v", providers)
	}
	groups, _ := cfg["proxy-groups"].([]any)
	if len(groups) == 0 {
		t.Fatalf("proxy-groups missing")
	}
	mainGroup, _ := groups[0].(map[string]any)
	proxies, _ := mainGroup["proxies"].([]any)
	if len(proxies) == 0 || strings.TrimSpace(toString(proxies[0])) != "DIRECT" {
		t.Fatalf("group proxies should fallback to DIRECT after invalid ref pruning, got: %#v", proxies)
	}
}

