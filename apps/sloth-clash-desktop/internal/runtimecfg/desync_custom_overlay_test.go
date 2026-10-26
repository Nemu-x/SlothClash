package runtimecfg

import (
	"encoding/json"
	"fmt"
	"reflect"
	"strings"
	"testing"
	"time"

	"SlothClashDesktop/internal/desync"
)

func TestDesyncCustomOverlayGroupsRulesAndRemoval(t *testing.T) {
	doc := map[string]any{"rules": []any{"DOMAIN-SUFFIX,example.org,profile-node", "MATCH,profile-node"}}
	settings := DesyncSettings{Enabled: true, YouTube: true, FakeTTL: true, Custom: []DesyncCustomRoute{
		{ID: "aa11", Domain: "example.org"},
		{ID: "bb22", Domain: "cdn.example.org"},
	}}
	cache := []desync.CacheEntry{{Network: "net", Custom: map[string]string{"example.org": desync.DirectID, "cdn.example.org": "f7"}, Updated: time.Now().Unix()}}
	network := func() string { return "net" }
	applyDesyncOverlay(doc, settings, cache, network)
	groups := map[string]map[string]any{}
	for _, raw := range desyncSlice(doc["proxy-groups"]) {
		g := raw.(map[string]any)
		groups[g["name"].(string)] = g
	}
	broad, narrow := groups[desync.CustomRoute("aa11")], groups[desync.CustomRoute("bb22")]
	if broad == nil || narrow == nil || broad["hidden"] != true || broad[desync.ManagedKey] != true {
		t.Fatalf("custom groups missing: %v", groups)
	}
	names := narrow["proxies"].([]string)
	if names[0] != desync.CustomRoute("bb22")+"-f7" || names[len(names)-2] != "DIRECT" || names[len(names)-1] != "PASS" || len(names) != len(desync.CustomCandidates(true))+2 {
		t.Fatalf("custom members: %v", names)
	}
	if narrow["default-selected"] != names[0] || broad["default-selected"] != "DIRECT" {
		t.Fatalf("cached selections: %v / %v", narrow["default-selected"], broad["default-selected"])
	}
	rules := desyncSlice(doc["rules"])
	index := func(rule string) int {
		for i, r := range rules {
			if r == rule {
				return i
			}
		}
		return -1
	}
	narrowTCP := index("AND,((NETWORK,tcp),(DOMAIN-SUFFIX,cdn.example.org))," + desync.CustomRoute("bb22"))
	broadTCP := index("AND,((NETWORK,tcp),(DOMAIN-SUFFIX,example.org))," + desync.CustomRoute("aa11"))
	broadQUIC := index("AND,((NETWORK,udp),(DST-PORT,443),(DOMAIN-SUFFIX,example.org))," + desync.CustomRoute("aa11"))
	youtube := index(desync.Rules(desync.YouTubeDomains, desync.YouTubeRoute)[0])
	profile := index("DOMAIN-SUFFIX,example.org,profile-node")
	if youtube != 0 || narrowTCP < 0 || broadTCP < 0 || broadQUIC < 0 || !(narrowTCP < broadTCP && broadTCP < profile) {
		t.Fatalf("rule order youtube=%d narrow=%d broad=%d profile=%d: %v", youtube, narrowTCP, broadTCP, profile, rules)
	}
	first, _ := json.Marshal(doc)
	applyDesyncOverlay(doc, settings, cache, network)
	if second, _ := json.Marshal(doc); string(first) != string(second) {
		t.Fatal("custom overlay is not idempotent")
	}
	// Removing an entry removes exactly its group, candidates and rules.
	settings.Custom = settings.Custom[:1]
	applyDesyncOverlay(doc, settings, cache, network)
	for _, raw := range append(append(desyncSlice(doc["rules"]), desyncSlice(doc["proxies"])...), desyncSlice(doc["proxy-groups"])...) {
		if strings.Contains(fmt.Sprint(raw), desync.CustomRoute("bb22")) {
			t.Fatalf("removed entry survived: %v", raw)
		}
	}
	if groups := desyncSlice(doc["proxy-groups"]); len(groups) != 2 {
		t.Fatalf("remaining groups: %d", len(groups))
	}
	applyDesyncOverlay(doc, DesyncSettings{}, nil, nil)
	if !reflect.DeepEqual(doc["rules"], []any{"DOMAIN-SUFFIX,example.org,profile-node", "MATCH,profile-node"}) || len(desyncSlice(doc["proxies"])) != 0 {
		t.Fatalf("disable did not restore the profile: %v", doc)
	}
}

func TestDesyncCustomOverlayWithoutFakeTTLAndWithoutBuiltins(t *testing.T) {
	doc := map[string]any{"rules": []any{"MATCH,DIRECT"}}
	applyDesyncOverlay(doc, DesyncSettings{Enabled: true, Custom: []DesyncCustomRoute{{ID: "cc33", Domain: "example.net"}}}, nil, nil)
	if got := len(desyncSlice(doc["proxies"])); got != len(desync.CustomCandidates(false)) {
		t.Fatalf("candidates without fake TTL: %d", got)
	}
	for _, raw := range desyncSlice(doc["proxies"]) {
		if raw.(map[string]any)["desync"].(map[string]any)["fake"] != nil {
			t.Fatal("fake-TTL candidate on an unsupported platform")
		}
	}
	if groups := desyncSlice(doc["proxy-groups"]); len(groups) != 1 || groups[0].(map[string]any)["default-selected"] != desync.CustomRoute("cc33")+"-r3" {
		t.Fatalf("groups: %v", groups)
	}
	if rules := desyncSlice(doc["rules"]); len(rules) != 3 || rules[2] != "MATCH,DIRECT" {
		t.Fatalf("rules: %v", rules)
	}
}
