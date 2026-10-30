package runtimecfg

import (
	"errors"
	"os"
	"path/filepath"
	"testing"

	"SlothClashDesktop/internal/desync"
	"SlothClashDesktop/internal/desync/desynctest"

	"gopkg.in/yaml.v3"
)

func desyncOnlyInput(t *testing.T, s DesyncSettings, enableTun bool) Input {
	t.Helper()
	env := testEnv()
	env.Prefs.Desync = s
	traffic := "proxy"
	if enableTun {
		traffic = "tun"
	}
	return Input{
		DataDir:                t.TempDir(),
		DesyncOnly:             true,
		CtrlPort:               9090,
		MixedPort:              7890,
		Secret:                 "s",
		WithExternalController: true,
		Traffic:                traffic,
		EnableTun:              enableTun,
		Env:                    env,
	}
}

func readGenerated(t *testing.T, dataDir string) map[string]any {
	t.Helper()
	b, err := os.ReadFile(filepath.Join(dataDir, "config.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	var m map[string]any
	if err := yaml.Unmarshal(b, &m); err != nil {
		t.Fatal(err)
	}
	return m
}

// The Desync routes come first, everything else ends at MATCH,DIRECT, the
// route groups end at PASS (which therefore falls through to DIRECT), and no
// subscription is involved: nothing fetched, no body cache, no provider.
func TestWriteDesyncOnly(t *testing.T) {
	for _, tun := range []bool{false, true} {
		in := desyncOnlyInput(t, DesyncSettings{Enabled: true, YouTube: true, Custom: []DesyncCustomRoute{{ID: "aa11", Domain: "example.org"}}, Caps: desynctest.Windows()}, tun)
		if err := Write(in, nil); err != nil {
			t.Fatalf("tun=%v: %v", tun, err)
		}
		m := readGenerated(t, in.DataDir)
		if m["mode"] != "rule" {
			t.Fatalf("mode = %v", m["mode"])
		}
		rules := desyncSlice(m["rules"])
		if len(rules) < 2 || rules[len(rules)-1] != "MATCH,DIRECT" {
			t.Fatalf("rules = %v", rules)
		}
		if rules[0] != desync.Rules(desync.YouTubeDomains, desync.YouTubeRoute)[0] {
			t.Fatalf("first rule = %v, want the YouTube route", rules[0])
		}
		for _, r := range rules[:len(rules)-1] {
			s, _ := r.(string)
			if isCustomDesyncRule(s) || stringsHasRoute(s, desync.YouTubeRoute) {
				continue
			}
			t.Fatalf("unexpected rule %q in a Desync-only config", s)
		}
		for _, raw := range desyncSlice(m["proxy-groups"]) {
			g := raw.(map[string]any)
			members := desyncSlice(g["proxies"])
			if g[desync.ManagedKey] != true || members[len(members)-1] != "PASS" {
				t.Fatalf("group %v", g)
			}
		}
		for _, raw := range desyncSlice(m["proxies"]) {
			if raw.(map[string]any)[desync.ManagedKey] != true {
				t.Fatalf("a non-Desync proxy: %v", raw)
			}
		}
		if _, has := m["proxy-providers"]; has {
			t.Fatal("a Desync-only config has a proxy provider")
		}
		if tunBlock, _ := m["tun"].(map[string]any); tunBlock["enable"] != tun {
			t.Fatalf("tun.enable = %v, want %v", tunBlock["enable"], tun)
		}
		if _, has := m["dns"].(map[string]any); !has {
			t.Fatal("no default DNS block")
		}
		if len(ReadSubscriptionBodyCache(in.DataDir)) != 0 {
			t.Fatal("a Desync-only generation wrote a subscription body cache")
		}
	}
}

func stringsHasRoute(rule, route string) bool {
	return len(rule) > len(route) && rule[len(rule)-len(route)-1:] == ","+route
}

// Desync off: the profile is plain direct.
func TestWriteDesyncOnlyWithDesyncOff(t *testing.T) {
	in := desyncOnlyInput(t, DesyncSettings{YouTube: true}, false)
	if err := Write(in, nil); err != nil {
		t.Fatal(err)
	}
	m := readGenerated(t, in.DataDir)
	if rules := desyncSlice(m["rules"]); len(rules) != 1 || rules[0] != "MATCH,DIRECT" {
		t.Fatalf("rules = %v", rules)
	}
	if _, has := m["proxy-groups"]; has {
		t.Fatalf("groups with Desync off: %v", m["proxy-groups"])
	}
}

// A subscription, template or script handed to a Desync-only generation is a
// bug upstream of it; refuse instead of building something else.
func TestWriteDesyncOnlyRejectsSubscriptionInputs(t *testing.T) {
	for name, set := range map[string]func(*Input){
		"url":    func(in *Input) { in.SubURL = "https://example.com/s" },
		"age":    func(in *Input) { in.AgeKey = "AGE-SECRET-KEY-1" },
		"extend": func(in *Input) { in.ExtendTemplate = "prepend: {}" },
		"proxy":  func(in *Input) { in.ProxyTemplate = "append: {}" },
		"rules":  func(in *Input) { in.RulesTemplate = "append: {}" },
		"script": func(in *Input) { in.Script = "function main(c){return c}" },
	} {
		in := desyncOnlyInput(t, DesyncSettings{Enabled: true, YouTube: true}, false)
		set(&in)
		if err := Write(in, nil); !errors.Is(err, errDesyncOnlyInput) {
			t.Errorf("%s: err = %v", name, err)
		}
		if _, err := os.Stat(filepath.Join(in.DataDir, "config.yaml")); err == nil {
			t.Errorf("%s: config.yaml written", name)
		}
	}
}
