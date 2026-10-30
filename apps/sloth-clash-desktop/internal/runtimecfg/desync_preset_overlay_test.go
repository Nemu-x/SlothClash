package runtimecfg

import (
	"strings"
	"testing"
	"time"

	"SlothClashDesktop/internal/desync"
	"SlothClashDesktop/internal/desync/desynctest"
)

// presetRuleRoutes maps each DOMAIN-SUFFIX of the generated custom rules to
// the groups routing it (TCP rules only), and returns the rule order.
func presetRuleRoutes(doc map[string]any) (map[string][]string, []string) {
	routes := map[string][]string{}
	var order []string
	for _, raw := range desyncSlice(doc["rules"]) {
		rule, _ := raw.(string)
		if !isCustomDesyncRule(rule) || !strings.HasPrefix(rule, "AND,((NETWORK,tcp),(DOMAIN-SUFFIX,") {
			continue
		}
		rest := strings.TrimPrefix(rule, "AND,((NETWORK,tcp),(DOMAIN-SUFFIX,")
		domain, route, _ := strings.Cut(rest, ")),")
		routes[domain] = append(routes[domain], route)
		order = append(order, domain)
	}
	return routes, order
}

func TestDesyncPresetOverlayRoutesCatalogDomains(t *testing.T) {
	doc := map[string]any{"rules": []any{"MATCH,DIRECT"}}
	settings := DesyncSettings{Enabled: true, Caps: desynctest.Windows(), Custom: []DesyncCustomRoute{
		{ID: "ig01", Preset: "instagram"},
		{ID: "pl01", Domain: "media.example.org"},
		{ID: "fb01", Preset: "facebook"},
	}}
	cache := []desync.CacheEntry{{Network: "net", Custom: map[string]string{"preset:facebook": "r3", "facebook": "r8"}, Updated: time.Now().Unix()}}
	applyDesyncOverlay(doc, settings, cache, func() string { return "net" })
	routes, order := presetRuleRoutes(doc)
	fb, ig := desync.CustomRoute("fb01"), desync.CustomRoute("ig01")
	// Every catalog domain is routed once; the shared CDN goes to Facebook
	// (catalog order) although Instagram is first in the list.
	for _, p := range desync.Services[:2] {
		for _, d := range p.Domains {
			if len(routes[d]) != 1 {
				t.Fatalf("%s routed by %v", d, routes[d])
			}
		}
	}
	if routes["fbcdn.net"][0] != fb || routes["instagram.com"][0] != ig || routes["cdninstagram.com"][0] != ig {
		t.Fatalf("routes: %v", routes)
	}
	// The more specific plain entry goes first; then list order, catalog
	// order within a preset.
	if order[0] != "media.example.org" || order[1] != "instagram.com" || order[len(order)-1] != "m.me" {
		t.Fatalf("order: %v", order)
	}
	// One hidden group per preset entry, cached under preset:<id>.
	selected := map[string]any{}
	for _, raw := range desyncSlice(doc["proxy-groups"]) {
		g := raw.(map[string]any)
		selected[g["name"].(string)] = g["default-selected"]
	}
	if len(selected) != 3 || selected[fb] != fb+"-"+presetID(t, "tlsrec-3s") || selected[ig] != ig+"-"+presetID(t, "tlsrec-1s") {
		t.Fatalf("groups: %v", selected)
	}
	// Without Facebook, Instagram carries the shared CDN itself.
	settings.Custom = settings.Custom[:2]
	applyDesyncOverlay(doc, settings, cache, func() string { return "net" })
	routes, _ = presetRuleRoutes(doc)
	if len(routes["fbcdn.net"]) != 1 || routes["fbcdn.net"][0] != ig || routes["facebook.com"] != nil {
		t.Fatalf("instagram alone: %v", routes)
	}
	// Each routed domain also has its UDP/443 rule.
	for _, raw := range desyncSlice(doc["rules"]) {
		rule := raw.(string)
		if strings.HasPrefix(rule, "AND,((NETWORK,tcp),(DOMAIN-SUFFIX,") && isCustomDesyncRule(rule) {
			quic := strings.Replace(rule, "(NETWORK,tcp),", "(NETWORK,udp),(DST-PORT,443),", 1)
			found := false
			for _, other := range desyncSlice(doc["rules"]) {
				found = found || other == quic
			}
			if !found {
				t.Fatalf("no UDP/443 rule for %s", rule)
			}
		}
	}
}
