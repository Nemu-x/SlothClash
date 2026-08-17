package main

import (
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestCoreProtocolSurfacePassesThroughPipeline guards the class of bug a core
// bump can introduce silently: a proxy type or option the pinned core supports
// but our pipeline mangles or drops on the way to config.yaml.
//
// The pipeline deliberately has NO proxy-type allowlist — proxies are passed
// through verbatim and only `hysteria2`/`hysteria`/`tuic` are touched at all
// (normalizeProxySNI). This test locks that in with the newest protocol surface
// we ship, including options that arrived with the core bump:
//   - zerotier: an outbound with NO server/port at all, only `network`
//   - wireguard: nested `ip-stack` + `amnezia-wg-option` (AmneziaWG 3.x)
//   - hysteria2: `handshake-timeout` alongside the servername→sni normalization
//   - anytls: `client-metadata`
//
// When a sidecar binary is available it also asserts the shipped core actually
// accepts the generated config, which is what makes this a compatibility test
// rather than a shape test.
func TestCoreProtocolSurfacePassesThroughPipeline(t *testing.T) {
	t.Parallel()
	subscription := `
mixed-port: 7890
mode: rule
proxies:
  - name: zt-node
    type: zerotier
    network: 8056c2e21c000001
    udp: true
    ip-stack:
      mode: gvisor
      congestion-controller: bbr3
  - name: wg-amnezia
    type: wireguard
    server: 10.0.0.1
    port: 51820
    private-key: aG9wZWZ1bGx5LWEtdmFsaWQtYmFzZTY0LWtleS0xMjM0NTY=
    public-key: aG9wZWZ1bGx5LWEtdmFsaWQtYmFzZTY0LWtleS0xMjM0NTY=
    ip: 10.0.0.2
    ip-stack:
      mode: auto
      congestion-controller: bbr
    amnezia-wg-option:
      jc: 4
      s3: 100
      i1: "<b 0xdeadbeef>"
      disable-cookies: true
  - name: hy2-node
    type: hysteria2
    server: hy2.example.test
    port: 443
    password: secret-pass
    servername: hy2.example.test
    handshake-timeout: 12
  - name: anytls-node
    type: anytls
    server: at.example.test
    port: 8443
    password: secret-pass
    client-metadata: "sloth/1"
proxy-groups:
  - name: Main
    type: select
    proxies:
      - zt-node
      - wg-amnezia
      - hy2-node
      - anytls-node
      - DIRECT
sniffer:
  enable: true
  sniff:
    HTTP:
      ports: [80, 8080]
    QUIC:
      ports: [443]
rules:
  - MATCH,Main
`
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(subscription))
	}))
	defer srv.Close()

	dir := t.TempDir()
	outcome, err := tryWriteMergedFullProfile(
		dir, srv.URL, "", "", "", "", 9090, 7890, "secret", "tun", true, true,
	)
	if err != nil {
		t.Fatalf("tryWriteMergedFullProfile failed: %v", err)
	}
	if outcome != pipelineOK {
		t.Fatalf("expected full-profile path, got outcome=%s", outcome)
	}
	cfg := readYAMLMapForTest(t, filepath.Join(dir, "config.yaml"))

	byName := map[string]map[string]any{}
	proxies, _ := cfg["proxies"].([]any)
	for _, it := range proxies {
		if p, ok := it.(map[string]any); ok {
			name, _ := p["name"].(string)
			byName[name] = p
		}
	}
	for _, want := range []string{"zt-node", "wg-amnezia", "hy2-node", "anytls-node"} {
		if byName[want] == nil {
			t.Fatalf("proxy %q was dropped by the pipeline (kept %d of 4)", want, len(byName))
		}
	}

	// zerotier has no server/port — the pipeline must not require them, and its
	// nested ip-stack block must survive as a map.
	zt := byName["zt-node"]
	if zt["network"] != "8056c2e21c000001" {
		t.Errorf("zerotier network changed: %#v", zt["network"])
	}
	ztStack, ok := zt["ip-stack"].(map[string]any)
	if !ok {
		t.Fatalf("zerotier ip-stack mangled: %#v", zt["ip-stack"])
	}
	if ztStack["mode"] != "gvisor" || ztStack["congestion-controller"] != "bbr3" {
		t.Errorf("zerotier ip-stack values changed: %#v", ztStack)
	}

	amnezia, ok := byName["wg-amnezia"]["amnezia-wg-option"].(map[string]any)
	if !ok {
		t.Fatalf("amnezia-wg-option mangled: %#v", byName["wg-amnezia"]["amnezia-wg-option"])
	}
	if amnezia["jc"] != 4 || amnezia["i1"] != "<b 0xdeadbeef>" || amnezia["disable-cookies"] != true {
		t.Errorf("amnezia-wg-option values changed: %#v", amnezia)
	}

	// handshake-timeout must stay an integer (seconds) — a float would change
	// the field's shape for the core.
	hy := byName["hy2-node"]
	if ht, ok := hy["handshake-timeout"].(int); !ok || ht != 12 {
		t.Errorf("hysteria2 handshake-timeout changed: %#v (%T)", hy["handshake-timeout"], hy["handshake-timeout"])
	}
	// ...while the servername→sni normalization still applies to it.
	if hy["sni"] != "hy2.example.test" {
		t.Errorf("hysteria2 sni not normalized from servername: %#v", hy["sni"])
	}

	if got := byName["anytls-node"]["client-metadata"]; got != "sloth/1" {
		t.Errorf("anytls client-metadata changed: %#v", got)
	}

	// The sniffer block is passed through verbatim. Note the core's sniffer names
	// are only TLS/HTTP/QUIC — the H2C and QUICv2 support added in 1.19.30 lives
	// INSIDE those sniffers and is not configurable by name, so an invented name
	// here would be rejected by the core below.
	sniffer, _ := cfg["sniffer"].(map[string]any)
	if sniffer == nil {
		t.Fatalf("sniffer block dropped")
	}
	sniff, _ := sniffer["sniff"].(map[string]any)
	for _, want := range []string{"HTTP", "QUIC"} {
		if _, ok := sniff[want]; !ok {
			t.Errorf("sniffer %s dropped: %#v", want, sniff)
		}
	}

	bin := testCoreBinaryForPassthrough()
	if bin == "" {
		t.Skip("no core binary available (set SLOTH_MIHOMO_BIN or run `pnpm run prebuild`) — skipping the real-core acceptance check")
	}
	if err := runConfigPreflight(bin, dir); err != nil {
		t.Fatalf("the shipped core rejected our generated config: %v", err)
	}
}

// testCoreBinaryForPassthrough resolves a core binary to validate against:
// the explicit SLOTH_MIHOMO_BIN override first, otherwise the locally
// provisioned sidecar. Returns "" when neither is present.
func testCoreBinaryForPassthrough() string {
	if bin := strings.TrimSpace(os.Getenv("SLOTH_MIHOMO_BIN")); bin != "" {
		return bin
	}
	matches, _ := filepath.Glob(filepath.Join("build", "sidecar", "sloth-mihomo-*"))
	for _, m := range matches {
		// The alpha sidecar is a separate, unpinned binary — never validate against it.
		if strings.Contains(filepath.Base(m), "-alpha-") {
			continue
		}
		if abs, err := filepath.Abs(m); err == nil {
			return abs
		}
		return m
	}
	return ""
}
