package main

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"SlothClashDesktop/internal/desync"
	"SlothClashDesktop/internal/runtimecfg"
	"SlothClashDesktop/internal/runtimecfg/runtimecfgtest"
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

func TestDesyncOverlaySurvivesUserScript(t *testing.T) {
	prefsMu.Lock()
	previous := prefsCurrent
	prefsCurrent.Desync = DesyncSettings{Enabled: true, YouTube: true, Discord: true, DiscordTTL: 4}
	prefsMu.Unlock()
	defer func() { prefsMu.Lock(); prefsCurrent = previous; prefsMu.Unlock() }()
	doc := runtimecfgtest.RepresentativeFullProfile()
	var result runtimecfg.ScriptResult
	script := `function main(config) {
        config.proxies = config.proxies.filter(function(p) { return p.name.indexOf('SLOTH-DESYNC-') !== 0 })
        config.rules = ['MATCH,DIRECT']
        return config
    }`
	if err := runtimecfg.Finalize(doc, runtimecfg.Input{
		DataDir:                t.TempDir(),
		Script:                 script,
		CtrlPort:               9090,
		MixedPort:              7890,
		Secret:                 "local",
		WithExternalController: true,
		Traffic:                "proxy",
		EnableTun:              false,
		Env:                    currentRuntimeConfigEnv(),
	}, &result); err != nil {
		t.Fatal(err)
	}
	if !result.Applied {
		t.Fatalf("script did not run: %s", result.Err)
	}
	managed := 0
	for _, raw := range doc["proxies"].([]any) {
		if entry, ok := raw.(map[string]any); ok && entry[desync.ManagedKey] == true {
			managed++
		}
	}
	if managed != len(desync.Candidates("youtube"))+len(desync.Candidates("discord")) {
		t.Fatal("enabled routes were not restored through the complete script/finalization pipeline")
	}
	if doc["rules"].([]any)[len(doc["rules"].([]any))-1] != "MATCH,DIRECT" {
		t.Fatal("profile fallback changed")
	}
}

func TestDesyncDiagnosticUsesExplicitNamedOutbound(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/proxies/"+desync.DiscordRoute+"/delay" || r.URL.Query().Get("url") != "https://discord.com/api/v10/gateway" || r.Header.Get("Authorization") != "Bearer local-secret" || r.URL.Query().Get("expected") != "200-299" {
			t.Errorf("unexpected probe: %s %v", r.URL, r.Header)
		}
		_, _ = w.Write([]byte(`{"delay":12}`))
	}))
	defer server.Close()
	delay, err := probeDesyncOutbound(context.Background(), strings.TrimPrefix(server.URL, "http://"), "local-secret", desync.DiscordRoute, "https://discord.com/api/v10/gateway")
	if err != nil || delay != 12 {
		t.Fatalf("probe: %d %v", delay, err)
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
		_, err := probeDesyncOutbound(context.Background(), strings.TrimPrefix(server.URL, "http://"), "", desync.YouTubeRoute, "https://www.youtube.com/generate_204")
		server.Close()
		if err == nil || !strings.Contains(err.Error(), "HTTP 503: TLS handshake failed") || strings.Contains(err.Error(), "private-test-token") {
			t.Fatalf("lost failure detail or exposed a credential: %v", err)
		}
	}
}
