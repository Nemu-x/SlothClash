package runtimecfg

import (
	"strings"
	"testing"

	dlib "github.com/Nemu-x/desync"
	"gopkg.in/yaml.v3"

	"SlothClashDesktop/internal/desync"
	"SlothClashDesktop/internal/desync/desynctest"
)

// The generated YAML on each desktop OS: only "type: desync" outbounds of
// strategies that OS implements, every group member defined, and no legacy
// direct.desync block.
func TestDesyncOverlayYAMLPerOS(t *testing.T) {
	for _, tc := range []struct {
		caps dlib.Capabilities
		// absent are preset fields this OS must never see; present must
		// appear somewhere.
		absent, present []string
		tcp             int
	}{
		{desynctest.Windows(), nil, []string{"fake:", "disorder: true", "oob: true", "split-wait: sent", "split-wait: acked", "stable-binding: true"}, 22},
		{desynctest.Linux(), nil, []string{"fake:", "disorder: true", "oob: true", "split-wait: sent", "split-wait: acked", "stable-binding: true"}, 22},
		{desynctest.Darwin(), []string{"fake:", "split-wait: sent"}, []string{"disorder: true", "oob: true", "split-wait: acked", "stable-binding: true"}, 15},
	} {
		t.Run(tc.caps.OS, func(t *testing.T) {
			doc := map[string]any{"rules": []any{"MATCH,DIRECT"}}
			settings := DesyncSettings{Enabled: true, YouTube: true, Discord: true, DiscordUDP: true, Caps: tc.caps,
				Custom: []DesyncCustomRoute{{ID: "aa11", Domain: "example.org"}}}
			applyDesyncOverlay(doc, settings, nil, nil)
			out, err := yaml.Marshal(doc)
			if err != nil {
				t.Fatal(err)
			}
			text := string(out)
			for _, s := range tc.absent {
				if strings.Contains(text, s) {
					t.Errorf("%q generated on %s", s, tc.caps.OS)
				}
			}
			for _, s := range tc.present {
				if !strings.Contains(text, s) {
					t.Errorf("%q missing on %s", s, tc.caps.OS)
				}
			}
			if strings.Contains(text, "type: direct") || strings.Contains(text, "\n    desync:") {
				t.Fatal("legacy direct.desync outbound generated")
			}
			defined := map[string]bool{"DIRECT": true, "PASS": true}
			for _, raw := range desyncSlice(doc["proxies"]) {
				p := raw.(map[string]any)
				if p["type"] != "desync" {
					t.Fatalf("generated %v", p)
				}
				defined[p["name"].(string)] = true
			}
			counts := map[string]int{}
			for _, raw := range desyncSlice(doc["proxy-groups"]) {
				g := raw.(map[string]any)
				for _, member := range g["proxies"].([]string) {
					if !defined[member] {
						t.Fatalf("%s: undefined member %s", g["name"], member)
					}
				}
				counts[g["name"].(string)] = len(g["proxies"].([]string))
			}
			if counts[desync.YouTubeRoute] != tc.tcp+1 || counts[desync.DiscordRoute] != tc.tcp+1 ||
				counts[desync.CustomRoute("aa11")] != tc.tcp+2 || counts[desync.DiscordUDPRoute] != 4+1 {
				t.Fatalf("group sizes %v", counts)
			}
		})
	}
}
