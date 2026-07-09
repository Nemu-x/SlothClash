package main

import (
	"os"
	"path/filepath"
	"testing"

	"gopkg.in/yaml.v3"
)

func TestProfileHasLocalConfig(t *testing.T) {
	cases := []struct {
		name string
		p    Profile
		want bool
	}{
		{"local type", Profile{Type: "local"}, true},
		{"local type cased", Profile{Type: "Local"}, true},
		{"skip auto config", Profile{SkipAutoConfig: true}, true},
		{"url profile", Profile{URL: "https://example.com/sub"}, false},
		{"empty profile", Profile{}, false},
	}
	for _, c := range cases {
		if got := profileHasLocalConfig(c.p); got != c.want {
			t.Errorf("%s: profileHasLocalConfig = %v, want %v", c.name, got, c.want)
		}
	}
}

// The core of the local-config bug fix: a local profile (body cache seeded at
// import, NO subscription URL) must produce a valid runtime config.yaml through
// the normal cache-first pipeline. If this passes, relaxing the URL guards is
// sufficient — Connect will build a working config without a URL.
func TestLocalProfileBuildsRuntimeConfigFromSeededCacheWithoutURL(t *testing.T) {
	dataDir := t.TempDir()
	// A local profile seeds its body cache at import (ImportProfileFromText).
	// Use a Clash-YAML body (shawn020308's case: local yaml file, no URL).
	body := "" +
		"proxies:\n" +
		"  - {name: n1, type: ss, server: s.example.com, port: 443, cipher: aes-128-gcm, password: pw}\n" +
		"proxy-groups:\n" +
		"  - {name: PROXY, type: select, proxies: [n1, DIRECT]}\n" +
		"rules:\n" +
		"  - MATCH,PROXY\n"
	writeSubscriptionBodyCache(dataDir, []byte(body))

	// subURL = "" — exactly what a local profile passes. Cache-first means no network.
	outcome, err := tryWriteMergedFullProfile(dataDir, "", "", "", "", "", 0, 7890, "secret", "tun", false, true)
	if err != nil {
		t.Fatalf("pipeline error: %v", err)
	}
	if outcome != pipelineOK {
		t.Fatalf("outcome = %v, want pipelineOK", outcome)
	}
	b, err := os.ReadFile(filepath.Join(dataDir, "config.yaml"))
	if err != nil {
		t.Fatalf("config.yaml not written: %v", err)
	}
	var m map[string]any
	if err := yaml.Unmarshal(b, &m); err != nil {
		t.Fatalf("generated config.yaml is invalid yaml: %v", err)
	}
	if px, ok := m["proxies"].([]any); !ok || len(px) == 0 {
		t.Fatalf("expected proxies in generated config, got: %v", m["proxies"])
	}
}
