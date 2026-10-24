package main

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"reflect"
	"runtime"
	"strings"
	"testing"

	"gopkg.in/yaml.v3"
)

func TestDesyncSettingsValidation(t *testing.T) {
	valid := DesyncSettings{Enabled: true, YouTube: true, Discord: true, DiscordTTL: 4}
	if _, err := validateDesyncSettings(valid, "windows", true); err != nil {
		t.Fatal(err)
	}
	if _, err := validateDesyncSettings(valid, "linux", true); err == nil {
		t.Fatal("accepted unsupported fake platform")
	}
	if _, err := validateDesyncSettings(valid, "windows", false); err == nil {
		t.Fatal("accepted unsupported core")
	}
	if _, err := validateDesyncSettings(DesyncSettings{Enabled: true, DiscordTTL: 4}, "windows", true); err == nil {
		t.Fatal("accepted empty enabled selection")
	}
	valid.DiscordTTL = 999
	if _, err := validateDesyncSettings(valid, "windows", true); err == nil {
		t.Fatal("accepted invalid TTL")
	}
	valid.Enabled = false
	if got, err := validateDesyncSettings(valid, "linux", false); err != nil || got.Enabled || got.DiscordTTL != 4 {
		t.Fatalf("disable must remain available: %+v %v", got, err)
	}
}

func TestDesyncOverlayPreservesProfileAndRemovesOnlyGeneratedEntries(t *testing.T) {
	userProxy := map[string]any{"name": "subscription-node", "type": "socks5", "server": "example.test", "port": 1080}
	doc := map[string]any{"proxies": []any{userProxy}, "rules": []any{"DOMAIN-SUFFIX,internal.test,DIRECT", "MATCH,subscription-node"}, "mode": "rule"}
	settings := DesyncSettings{Enabled: true, YouTube: true, Discord: true, DiscordTTL: 4}
	applyDesyncOverlay(doc, settings)
	if len(doc["proxies"].([]any)) != 3 {
		t.Fatalf("unexpected proxies: %v", doc["proxies"])
	}
	if !reflect.DeepEqual(doc["proxies"].([]any)[0], userProxy) {
		t.Fatal("subscription proxy changed")
	}
	rules := doc["rules"].([]any)
	if rules[len(rules)-1] != "MATCH,subscription-node" {
		t.Fatal("subscription fallback changed")
	}
	if !strings.Contains(rules[0].(string), "NETWORK,udp") {
		t.Fatal("missing targeted QUIC rule")
	}
	for _, raw := range rules[:len(rules)-2] {
		rule := raw.(string)
		if !strings.Contains(rule, "NETWORK,tcp") && !strings.Contains(rule, "DST-PORT,443") {
			t.Fatalf("unscoped route: %s", rule)
		}
	}
	first, _ := json.Marshal(doc)
	applyDesyncOverlay(doc, settings)
	second, _ := json.Marshal(doc)
	if string(first) != string(second) {
		t.Fatal("overlay is not idempotent")
	}
	applyDesyncOverlay(doc, DesyncSettings{})
	if !reflect.DeepEqual(doc["proxies"], []any{userProxy}) || !reflect.DeepEqual(doc["rules"], []any{"DOMAIN-SUFFIX,internal.test,DIRECT", "MATCH,subscription-node"}) {
		t.Fatalf("disable failed to restore profile routes: %v", doc)
	}
}

func TestDesyncOverlayDisabledDoesNotCreateKeys(t *testing.T) {
	doc := map[string]any{"mode": "rule"}
	applyDesyncOverlay(doc, DesyncSettings{})
	if len(doc) != 1 {
		t.Fatalf("disabled overlay created entries: %v", doc)
	}
}

func TestDesyncOverlaySurvivesUserScript(t *testing.T) {
	prefsMu.Lock()
	previous := prefsCurrent
	prefsCurrent.Desync = DesyncSettings{Enabled: true, YouTube: true, Discord: true, DiscordTTL: 4}
	prefsMu.Unlock()
	defer func() { prefsMu.Lock(); prefsCurrent = previous; prefsMu.Unlock() }()
	doc := representativeFullProfile()
	var result scriptResult
	script := `function main(config) {
        config.proxies = config.proxies.filter(function(p) { return p.name.indexOf('SLOTH-DESYNC-') !== 0 })
        config.rules = ['MATCH,DIRECT']
        return config
    }`
	if err := finalizeRuntimeConfigPipelineWithScript(doc, t.TempDir(), 7890, 9090, "local", "proxy", true, false, script, &result); err != nil {
		t.Fatal(err)
	}
	if !result.Applied {
		t.Fatalf("script did not run: %s", result.Err)
	}
	managed := 0
	for _, raw := range doc["proxies"].([]any) {
		if entry, ok := raw.(map[string]any); ok && entry[desyncManagedKey] == true {
			managed++
		}
	}
	if managed != 2 {
		t.Fatal("enabled routes were not restored through the complete script/finalization pipeline")
	}
	if doc["rules"].([]any)[len(doc["rules"].([]any))-1] != "MATCH,DIRECT" {
		t.Fatal("profile fallback changed")
	}
}

func TestDesyncDiagnosticUsesExplicitNamedOutbound(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/proxies/"+desyncDiscordRoute+"/delay" || r.URL.Query().Get("url") != "https://discord.com/api/v10/gateway" || r.Header.Get("Authorization") != "Bearer local-secret" {
			t.Errorf("unexpected probe: %s %v", r.URL, r.Header)
		}
		_, _ = w.Write([]byte(`{"delay":12}`))
	}))
	defer server.Close()
	delay, err := probeDesyncOutbound(context.Background(), strings.TrimPrefix(server.URL, "http://"), "local-secret", desyncDiscordRoute, "https://discord.com/api/v10/gateway")
	if err != nil || delay != 12 {
		t.Fatalf("probe: %d %v", delay, err)
	}
}

func TestDesyncRuntimeConfigPreflight(t *testing.T) {
	bin := os.Getenv("SLOTH_MIHOMO_BIN")
	if bin == "" {
		t.Skip("set SLOTH_MIHOMO_BIN for patched-core preflight")
	}
	doc := map[string]any{"mode": "rule", "rules": []any{"MATCH,DIRECT"}, "proxies": []any{}}
	settings := DesyncSettings{Enabled: true, YouTube: true, Discord: runtime.GOOS == "windows", DiscordTTL: 4}
	applyDesyncOverlay(doc, settings)
	data, err := yaml.Marshal(doc)
	if err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "config.yaml"), data, 0600); err != nil {
		t.Fatal(err)
	}
	if err := runConfigPreflight(bin, dir); err != nil {
		t.Fatal(err)
	}
}
