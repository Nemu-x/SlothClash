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
	if err := finalizeRuntimeConfigPipeline(cfg, tmp, 7890, 9090, "secret", "tun", true); err != nil {
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
	ok, err := tryWriteMergedFullProfile(
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
	)
	if err != nil {
		t.Fatalf("tryWriteMergedFullProfile returned error: %v", err)
	}
	if !ok {
		t.Fatalf("expected full-profile path to be used")
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
	ok, err := tryWriteMergedFullProfile(
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
	)
	if err != nil {
		t.Fatalf("tryWriteMergedFullProfile returned error: %v", err)
	}
	if !ok {
		t.Fatalf("expected full-profile path to be used")
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

