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
	applyDesyncOverlay(doc, settings, currentDesktopPrefs().DesyncCache)
	if len(doc["proxies"].([]any)) != 1+len(desyncCandidates("youtube"))+len(desyncCandidates("discord")) {
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
	applyDesyncOverlay(doc, settings, currentDesktopPrefs().DesyncCache)
	second, _ := json.Marshal(doc)
	if string(first) != string(second) {
		t.Fatal("overlay is not idempotent")
	}
	applyDesyncOverlay(doc, DesyncSettings{}, currentDesktopPrefs().DesyncCache)
	if !reflect.DeepEqual(doc["proxies"], []any{userProxy}) || !reflect.DeepEqual(doc["rules"], []any{"DOMAIN-SUFFIX,internal.test,DIRECT", "MATCH,subscription-node"}) {
		t.Fatalf("disable failed to restore profile routes: %v", doc)
	}
}

func TestDesyncOverlayDisabledDoesNotCreateKeys(t *testing.T) {
	doc := map[string]any{"mode": "rule"}
	applyDesyncOverlay(doc, DesyncSettings{}, currentDesktopPrefs().DesyncCache)
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
	if err := finalizeRuntimeConfigPipelineWithScript(doc, t.TempDir(), 7890, 9090, "local", "proxy", true, false, script, &result, currentRuntimeConfigPrefs()); err != nil {
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
	if managed != len(desyncCandidates("youtube"))+len(desyncCandidates("discord")) {
		t.Fatal("enabled routes were not restored through the complete script/finalization pipeline")
	}
	if doc["rules"].([]any)[len(doc["rules"].([]any))-1] != "MATCH,DIRECT" {
		t.Fatal("profile fallback changed")
	}
}

func TestDesyncDiagnosticUsesExplicitNamedOutbound(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/proxies/"+desyncDiscordRoute+"/delay" || r.URL.Query().Get("url") != "https://discord.com/api/v10/gateway" || r.Header.Get("Authorization") != "Bearer local-secret" || r.URL.Query().Get("expected") != "200-299" {
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
	settings := DesyncSettings{Enabled: true, YouTube: true, Discord: runtime.GOOS == "windows", DiscordTTL: 4, DiscordUDP: runtime.GOOS == "windows", DiscordUDPTTL: 4, DiscordUDPRepeats: 6}
	applyDesyncOverlay(doc, settings, currentDesktopPrefs().DesyncCache)
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

func TestDesyncUDPOverlayIsExplicitAndReversible(t *testing.T) {
	doc := map[string]any{"rules": []any{"PROCESS-NAME,Discord.exe,subscription-node", "MATCH,subscription-node"}, "proxies": []any{map[string]any{"name": "subscription-node", "type": "direct"}}}
	s := DesyncSettings{Enabled: true, YouTube: true, Discord: true, DiscordTTL: 4, DiscordUDP: true, DiscordUDPTTL: 4, DiscordUDPRepeats: 6}
	applyDesyncOverlay(doc, s, currentDesktopPrefs().DesyncCache)
	if doc["rules"].([]any)[0] != desyncDiscordUDPRules()[0] {
		t.Fatal("Discord UDP did not override only the intended desktop rule")
	}
	proxies := doc["proxies"].([]any)
	var udp, youtube map[string]any
	for _, raw := range proxies {
		p := raw.(map[string]any)
		switch p["name"] {
		case desyncDiscordUDPRoute + "-f4":
			udp = p
		case desyncYouTubeRoute + "-r3":
			youtube = p
		}
	}
	if udp == nil || youtube == nil {
		t.Fatal("missing isolated candidates")
	}
	if options := udp["desync"].(map[string]any)["udp"].(map[string]any); options["packets"] != 3 || options["repeats"] != 1 {
		t.Fatalf("unsafe initial UDP preset: %v", options)
	}
	if options := youtube["desync"].(map[string]any); options["tlsrec"] != "3+s" || options["dns-fallback"] != true {
		t.Fatal("proven YouTube candidate lost")
	}
	groups := doc["proxy-groups"].([]any)
	if len(groups) != 3 || groups[0].(map[string]any)["name"] != desyncDiscordUDPRoute || groups[0].(map[string]any)["hidden"] != true {
		t.Fatal("missing hidden voice selector")
	}
	s.DiscordUDP = false
	applyDesyncOverlay(doc, s, currentDesktopPrefs().DesyncCache)
	if len(doc["proxies"].([]any)) != 1+len(desyncCandidates("youtube"))+len(desyncCandidates("discord")) {
		t.Fatal("UDP disable removed or retained the wrong outbounds")
	}
	for _, rule := range doc["rules"].([]any) {
		if strings.Contains(rule.(string), desyncDiscordUDPRoute) {
			t.Fatal("UDP route survived disable")
		}
	}
	if last := doc["rules"].([]any); last[len(last)-1] != "MATCH,subscription-node" {
		t.Fatal("subscription fallback changed")
	}
}

func TestDesyncUDPMigrationAndValidation(t *testing.T) {
	prefsMu.Lock()
	previous := prefsCurrent
	prefsCurrent.Desync = DesyncSettings{Enabled: true, YouTube: true, Discord: true, DiscordTTL: 4}
	prefsMu.Unlock()
	defer func() { prefsMu.Lock(); prefsCurrent = previous; prefsMu.Unlock() }()
	migrated := currentDesyncSettings()
	if migrated.DiscordUDP || migrated.DiscordUDPTTL != 4 || migrated.DiscordUDPRepeats != 1 {
		t.Fatalf("migration enabled untested UDP or lost defaults: %+v", migrated)
	}
	prefsMu.Lock()
	prefsCurrent.Desync.DiscordUDPRepeats = 6
	prefsMu.Unlock()
	if got := currentDesyncSettings(); got.DiscordUDPRepeats != 6 {
		t.Fatalf("explicit UDP repeat setting was overwritten: %+v", got)
	}
	for _, s := range []DesyncSettings{{Enabled: true, DiscordUDP: true, DiscordUDPTTL: 256}, {Enabled: true, DiscordUDP: true, DiscordUDPRepeats: 17}} {
		if _, err := validateDesyncSettings(s, "windows", true); err == nil {
			t.Fatal("invalid UDP options accepted")
		}
		s.Enabled = false
		if got, err := validateDesyncSettings(s, "windows", false); err != nil || got.Enabled {
			t.Fatalf("UDP settings prevented disable: %+v %v", got, err)
		}
	}
	if _, err := validateDesyncSettings(DesyncSettings{Enabled: true, DiscordUDP: true}, "linux", true); err == nil {
		t.Fatal("UDP fake accepted on an unsupported platform")
	}
}

func TestDesyncDiagnosticPreservesCoreFailure(t *testing.T) {
	for _, body := range []string{
		`{"message":"TLS handshake failed: context deadline exceeded"}`,
		`TLS handshake failed: context deadline exceeded`,
		`{"message":"TLS handshake failed: https://example.test/?token=private-test-token"}`,
	} {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(http.StatusServiceUnavailable)
			_, _ = w.Write([]byte(body))
		}))
		_, err := probeDesyncOutbound(context.Background(), strings.TrimPrefix(server.URL, "http://"), "", desyncYouTubeRoute, "https://www.youtube.com/generate_204")
		server.Close()
		if err == nil || !strings.Contains(err.Error(), "HTTP 503: TLS handshake failed") || strings.Contains(err.Error(), "private-test-token") {
			t.Fatalf("lost failure detail or exposed a credential: %v", err)
		}
	}
}
