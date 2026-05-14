package main

import (
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"gopkg.in/yaml.v3"
)

func TestSmokeFinalizeRuntimeConfigPipelineDNSRepair(t *testing.T) {
	t.Parallel()
	cfg := map[string]any{
		"dns": map[string]any{
			"respect-rules": true,
		},
		"proxy-groups": []any{
			map[string]any{"name": "Main", "type": "select", "proxies": []any{"DIRECT"}},
		},
	}
	tmp := t.TempDir()
	if err := finalizeRuntimeConfigPipeline(cfg, tmp, 7890, 9090, "secret", "tun", true, true); err != nil {
		t.Fatalf("finalizeRuntimeConfigPipeline error: %v", err)
	}
	dns, ok := cfg["dns"].(map[string]any)
	if !ok {
		t.Fatalf("dns block missing")
	}
	raw := dns["proxy-server-nameserver"]
	switch v := raw.(type) {
	case []any:
		if len(v) == 0 {
			t.Fatalf("proxy-server-nameserver was not repaired")
		}
	default:
		t.Fatalf("proxy-server-nameserver has unexpected type: %T", raw)
	}
}

func TestSmokeTryWriteMergedFullProfileFromURL(t *testing.T) {
	t.Parallel()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`
dns:
  respect-rules: true
proxy-groups:
  - name: MainGroup
    type: select
    proxies: [DIRECT]
rules:
  - MATCH,MainGroup
`))
	}))
	defer srv.Close()

	tmp := t.TempDir()
	outcome, err := tryWriteMergedFullProfile(
		tmp,
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
		t.Fatalf("tryWriteMergedFullProfile returned error: %v", err)
	}
	if outcome != pipelineOK {
		t.Fatalf("expected full-profile path to be used, got outcome=%s", outcome)
	}
	cfgPath := filepath.Join(tmp, "config.yaml")
	b, err := os.ReadFile(cfgPath)
	if err != nil {
		t.Fatalf("cannot read generated config.yaml: %v", err)
	}
	var out map[string]any
	if err := yaml.Unmarshal(b, &out); err != nil {
		t.Fatalf("generated yaml is invalid: %v", err)
	}
	dns, _ := out["dns"].(map[string]any)
	if dns == nil {
		t.Fatalf("generated config has no dns block")
	}
	sec, _ := out["secret"].(string)
	if strings.TrimSpace(sec) == "" {
		t.Fatalf("generated config has empty secret")
	}
}

func TestSmokeTryWriteMergedFullProfileDecodesUnicodeEscapes(t *testing.T) {
	t.Parallel()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`
proxies:
  - "\U0001F996 Dinosaur (AK_am_ls) [VLESS - tcp]"
proxy-groups:
  - name: MainGroup
    type: select
    proxies: [DIRECT]
rules:
  - MATCH,MainGroup
`))
	}))
	defer srv.Close()

	tmp := t.TempDir()
	outcome, err := tryWriteMergedFullProfile(
		tmp,
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
		t.Fatalf("tryWriteMergedFullProfile returned error: %v", err)
	}
	if outcome != pipelineOK {
		t.Fatalf("expected full-profile path to be used, got outcome=%s", outcome)
	}
	b, err := os.ReadFile(filepath.Join(tmp, "config.yaml"))
	if err != nil {
		t.Fatalf("cannot read generated config.yaml: %v", err)
	}
	text := string(b)
	if strings.Contains(text, `\U0001F996`) {
		t.Fatalf("expected unicode escapes to be decoded in final config, got: %s", text)
	}
	if !strings.Contains(text, "🦖 Dinosaur") {
		t.Fatalf("expected emoji in final config, got: %s", text)
	}
}

// TestCacheFirstSubscriptionReadIsInstantOnSubsequentConnects verifies the
// cache-first policy added to tryWriteMergedFullProfile: once we have a
// last-known-good body on disk, a subsequent call must succeed IMMEDIATELY
// without waiting on the origin. This is the fix for the "connect is
// sometimes instant, sometimes slow" regression — previously every Connect
// did a blocking fetch with up to 50 s timeout on the critical path.
//
// We simulate the origin being unreachable by pointing subURL at a closed
// httptest server. If the call still returns ok, it means the body was
// served from the cache rather than the network.
func TestCacheFirstSubscriptionReadIsInstantOnSubsequentConnects(t *testing.T) {
	t.Parallel()

	body := []byte(`
proxy-groups:
  - name: MainGroup
    type: select
    proxies: [DIRECT]
rules:
  - MATCH,MainGroup
`)

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		t.Errorf("cache-first path must not hit origin — got HTTP request")
		_, _ = w.Write(body)
	}))
	// Close immediately: any real network attempt would fail instantly. We
	// keep the URL for the call so the function has a non-empty subURL.
	closedURL := srv.URL
	srv.Close()

	tmp := t.TempDir()
	writeSubscriptionBodyCache(tmp, body)

	outcome, err := tryWriteMergedFullProfile(
		tmp,
		closedURL,
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
		t.Fatalf("tryWriteMergedFullProfile (cache-first) returned error: %v", err)
	}
	if outcome != pipelineOK {
		t.Fatalf("expected cache-first path to succeed without the origin, got outcome=%s", outcome)
	}
	if _, err := os.Stat(filepath.Join(tmp, "config.yaml")); err != nil {
		t.Fatalf("config.yaml not written from cache: %v", err)
	}
}

// TestFirstConnectFallsBackToNetworkWhenNoCache documents the other half of
// the policy: when there is no cached body on disk, tryWriteMergedFullProfile
// must still do a blocking fetch so Connect still works on the first-ever
// run. This also protects against accidentally inverting the branches.
func TestFirstConnectFallsBackToNetworkWhenNoCache(t *testing.T) {
	t.Parallel()
	body := []byte(`
proxy-groups:
  - name: MainGroup
    type: select
    proxies: [DIRECT]
rules:
  - MATCH,MainGroup
`)
	hit := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		hit++
		_, _ = w.Write(body)
	}))
	defer srv.Close()

	tmp := t.TempDir()
	outcome, err := tryWriteMergedFullProfile(
		tmp,
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
		t.Fatalf("tryWriteMergedFullProfile (no cache) returned error: %v", err)
	}
	if outcome != pipelineOK {
		t.Fatalf("expected fresh fetch to succeed, got outcome=%s", outcome)
	}
	if hit != 1 {
		t.Fatalf("expected exactly one origin hit on first-ever connect, got %d", hit)
	}
	if _, err := os.Stat(subscriptionBodyCachePath(tmp)); err != nil {
		t.Fatalf("expected cache to be written after first fetch: %v", err)
	}
}

