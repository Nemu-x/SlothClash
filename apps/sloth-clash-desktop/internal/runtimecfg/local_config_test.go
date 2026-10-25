package runtimecfg

import (
	"os"
	"path/filepath"
	"testing"

	"gopkg.in/yaml.v3"
)

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
	WriteSubscriptionBodyCache(dataDir, []byte(body))

	// No SubURL — exactly what a local profile passes. Cache-first means no network.
	outcome, err := tryWriteMergedFullProfile(Input{
		DataDir:                dataDir,
		CtrlPort:               0,
		MixedPort:              7890,
		Secret:                 "secret",
		WithExternalController: false,
		Traffic:                "tun",
		EnableTun:              true,
		Env:                    testEnv(),
	}, nil)
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
