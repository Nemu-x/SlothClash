package runtimecfg

import (
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"runtime"
	"strings"
	"testing"

	"SlothClashDesktop/internal/desync"

	"gopkg.in/yaml.v3"
)

func TestDesyncOverlayPreservesProfileAndRemovesOnlyGeneratedEntries(t *testing.T) {
	userProxy := map[string]any{"name": "subscription-node", "type": "socks5", "server": "example.test", "port": 1080}
	doc := map[string]any{"proxies": []any{userProxy}, "rules": []any{"DOMAIN-SUFFIX,internal.test,DIRECT", "MATCH,subscription-node"}, "mode": "rule"}
	settings := DesyncSettings{Enabled: true, YouTube: true, Discord: true}
	applyDesyncOverlay(doc, settings, nil, nil)
	if len(doc["proxies"].([]any)) != 1+len(desync.Candidates("youtube"))+len(desync.Candidates("discord")) {
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
	applyDesyncOverlay(doc, settings, nil, nil)
	second, _ := json.Marshal(doc)
	if string(first) != string(second) {
		t.Fatal("overlay is not idempotent")
	}
	applyDesyncOverlay(doc, DesyncSettings{}, nil, nil)
	if !reflect.DeepEqual(doc["proxies"], []any{userProxy}) || !reflect.DeepEqual(doc["rules"], []any{"DOMAIN-SUFFIX,internal.test,DIRECT", "MATCH,subscription-node"}) {
		t.Fatalf("disable failed to restore profile routes: %v", doc)
	}
}

func TestDesyncOverlayDisabledDoesNotCreateKeys(t *testing.T) {
	doc := map[string]any{"mode": "rule"}
	applyDesyncOverlay(doc, DesyncSettings{}, nil, nil)
	if len(doc) != 1 {
		t.Fatalf("disabled overlay created entries: %v", doc)
	}
}

func TestDesyncRuntimeConfigPreflight(t *testing.T) {
	bin := os.Getenv("SLOTH_MIHOMO_BIN")
	if bin == "" {
		t.Skip("set SLOTH_MIHOMO_BIN for patched-core preflight")
	}
	doc := map[string]any{"mode": "rule", "rules": []any{"MATCH,DIRECT"}, "proxies": []any{}}
	settings := DesyncSettings{Enabled: true, YouTube: true, Discord: runtime.GOOS == "windows", DiscordUDP: runtime.GOOS == "windows", FakeTTL: runtime.GOOS == "windows", Custom: []DesyncCustomRoute{{ID: "0a1b2c3d", Domain: "example.org"}, {ID: "5a6b7c8d", Domain: "media.example.org"}}}
	applyDesyncOverlay(doc, settings, nil, nil)
	data, err := yaml.Marshal(doc)
	if err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "config.yaml"), data, 0600); err != nil {
		t.Fatal(err)
	}
	if err := Preflight(bin, dir, testPrefs()); err != nil {
		t.Fatal(err)
	}
}

func TestDesyncUDPOverlayIsExplicitAndReversible(t *testing.T) {
	doc := map[string]any{"rules": []any{"PROCESS-NAME,Discord.exe,subscription-node", "MATCH,subscription-node"}, "proxies": []any{map[string]any{"name": "subscription-node", "type": "direct"}}}
	s := DesyncSettings{Enabled: true, YouTube: true, Discord: true, DiscordUDP: true}
	applyDesyncOverlay(doc, s, nil, nil)
	if doc["rules"].([]any)[0] != desync.DiscordUDPRules()[0] {
		t.Fatal("Discord UDP did not override only the intended desktop rule")
	}
	proxies := doc["proxies"].([]any)
	var udp, youtube map[string]any
	for _, raw := range proxies {
		p := raw.(map[string]any)
		switch p["name"] {
		case desync.DiscordUDPRoute + "-f4":
			udp = p
		case desync.YouTubeRoute + "-r3":
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
	if len(groups) != 3 || groups[0].(map[string]any)["name"] != desync.DiscordUDPRoute || groups[0].(map[string]any)["hidden"] != true {
		t.Fatal("missing hidden voice selector")
	}
	s.DiscordUDP = false
	applyDesyncOverlay(doc, s, nil, nil)
	if len(doc["proxies"].([]any)) != 1+len(desync.Candidates("youtube"))+len(desync.Candidates("discord")) {
		t.Fatal("UDP disable removed or retained the wrong outbounds")
	}
	for _, rule := range doc["rules"].([]any) {
		if strings.Contains(rule.(string), desync.DiscordUDPRoute) {
			t.Fatal("UDP route survived disable")
		}
	}
	if last := doc["rules"].([]any); last[len(last)-1] != "MATCH,subscription-node" {
		t.Fatal("subscription fallback changed")
	}
}

func TestDesyncReserveOverlayRestoresQUICAndMigratesOldRejectRules(t *testing.T) {
	old := "AND,((NETWORK,udp),(DST-PORT,443),(DOMAIN-SUFFIX,youtube.com)),REJECT"
	doc := map[string]any{"rules": []any{old, "DOMAIN-SUFFIX,private.test,REJECT", "MATCH,profile"}}
	settings := DesyncSettings{Enabled: true, YouTube: true, Discord: true, DiscordUDP: true}
	applyDesyncOverlay(doc, settings, nil, nil)
	for _, raw := range desyncSlice(doc["rules"]) {
		if raw == old {
			t.Fatal("old static QUIC rejection survived")
		}
	}
	for _, raw := range desyncSlice(doc["proxy-groups"]) {
		g := raw.(map[string]any)
		names := g["proxies"].([]string)
		if names[len(names)-1] != "PASS" {
			t.Fatal("missing original-rule reserve")
		}
	}
	applyDesyncOverlay(doc, DesyncSettings{}, nil, nil)
	if !reflect.DeepEqual(doc["rules"], []any{"DOMAIN-SUFFIX,private.test,REJECT", "MATCH,profile"}) {
		t.Fatal("profile rules changed")
	}
	for _, service := range []string{"youtube", "discord", "voice"} {
		opts := desync.Candidates(service)[0].Proxy("test")["desync"].(map[string]any)
		if (opts["reject-quic"] == true) != (service != "voice") {
			t.Fatal("conditional QUIC scope changed")
		}
	}
}

func TestDesyncVoiceRuleUpgradeRemovesOldAllUDPMatch(t *testing.T) {
	old := "AND,((NETWORK,udp),(PROCESS-NAME,Discord.exe))," + desync.DiscordUDPRoute
	doc := map[string]any{"rules": []any{old, "MATCH,DIRECT"}}
	applyDesyncOverlay(doc, DesyncSettings{Enabled: true, DiscordUDP: true}, nil, nil)
	for _, r := range doc["rules"].([]any) {
		if r == old {
			t.Fatal("legacy broad UDP rule retained")
		}
	}
	if !strings.Contains(doc["rules"].([]any)[0].(string), "DST-PORT,1024-65535") {
		t.Fatal("voice includes unrelated low-port UDP")
	}
}
