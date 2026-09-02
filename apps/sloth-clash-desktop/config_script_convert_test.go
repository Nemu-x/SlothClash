package main

import (
	"strings"
	"testing"
)

// Value conversion is the single most likely source of subtle bugs in this
// feature: goja hands every number back as a float64 once arithmetic touched it,
// and `mixed-port: 7890.0` is a different config than `mixed-port: 7890`. These
// tests lock the round trip.

// scriptRoundTripConfig is a realistic post-overlay config: the shapes our own
// pipeline actually produces, including the []string containers (dns-hijack) that
// a naive conversion would turn into an object with numeric keys.
func scriptRoundTripConfig() map[string]any {
	return map[string]any{
		"mixed-port":          7890,
		"socks-port":          0,
		"mode":                "rule",
		"ipv6":                true,
		"unified-delay":       true,
		"log-level":           "info",
		"external-controller": "127.0.0.1:9097",
		"secret":              "deadbeef",
		"profile":             map[string]any{"store-selected": true, "store-fake-ip": false},
		"dns": map[string]any{
			"enable":         true,
			"listen":         ":1053",
			"enhanced-mode":  "fake-ip",
			"fake-ip-range":  "198.18.0.1/16",
			"ipv6":           true,
			"nameserver":     []any{"https://1.1.1.1/dns-query", "8.8.8.8"},
			"fake-ip-filter": []any{"*.lan", "+.ru"},
			"nameserver-policy": map[string]any{
				"+.ru": []any{"https://77.88.8.8/dns-query"},
			},
		},
		"tun": map[string]any{
			"enable":       true,
			"stack":        "gvisor",
			"dns-hijack":   []string{"any:53"},
			"auto-route":   true,
			"strict-route": false,
			"mtu":          9000,
		},
		"sniffer": map[string]any{
			"enable": true,
			"sniff": map[string]any{
				"TLS": map[string]any{"ports": []any{443, "8443"}},
			},
		},
		"proxies": []any{
			map[string]any{"name": "n1", "type": "ss", "server": "s.example.com", "port": 443, "udp": true},
		},
		"proxy-groups": []any{
			map[string]any{"name": "PROXY", "type": "url-test", "interval": 300, "tolerance": 50, "proxies": []any{"n1", "DIRECT"}},
		},
		"rules": []any{"DOMAIN-SUFFIX,example.com,PROXY", "MATCH,DIRECT"},
	}
}

// The identity script must be a no-op down to the byte. If this fails, every
// scripted config silently differs from its unscripted twin.
func TestScriptIdentityRoundTripIsByteIdentical(t *testing.T) {
	before := scriptRoundTripConfig()
	wantYAML, err := marshalRuntimeYAML(before)
	if err != nil {
		t.Fatalf("marshal before: %v", err)
	}

	after, res := runScript(t, `main = function (config) { return config }`, scriptRoundTripConfig())
	if !res.Applied {
		t.Fatalf("identity script failed: %s", res.Err)
	}
	gotYAML, err := marshalRuntimeYAML(after)
	if err != nil {
		t.Fatalf("marshal after: %v", err)
	}

	if string(gotYAML) != string(wantYAML) {
		t.Errorf("identity script changed the generated YAML.\n--- without script ---\n%s\n--- with identity script ---\n%s", wantYAML, gotYAML)
	}
}

// Arrow functions and let/const are ES2015 that goja supports; scripts will be
// written that way, so the identity path must hold there too.
func TestScriptIdentityRoundTripArrowFunction(t *testing.T) {
	wantYAML, err := marshalRuntimeYAML(scriptRoundTripConfig())
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	after, res := runScript(t, `const main = (config) => config`, scriptRoundTripConfig())
	if !res.Applied {
		t.Fatalf("arrow-function script failed: %s", res.Err)
	}
	gotYAML, err := marshalRuntimeYAML(after)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	if string(gotYAML) != string(wantYAML) {
		t.Errorf("arrow identity changed the YAML:\n%s", gotYAML)
	}
}

// A []string container must come back as a YAML sequence, not as an object with
// "0", "1" keys — the failure mode of handing Go slices straight to the engine.
func TestScriptTypedSliceSurvivesAsArray(t *testing.T) {
	after, res := runScript(t, `function main(config) { config.tun['dns-hijack'].push('any:5353'); return config }`,
		map[string]any{"tun": map[string]any{"dns-hijack": []string{"any:53"}}})
	if !res.Applied {
		t.Fatalf("script failed: %s", res.Err)
	}
	tun := after["tun"].(map[string]any)
	list, ok := tun["dns-hijack"].([]any)
	if !ok {
		t.Fatalf("dns-hijack came back as %T, want a list — a Go slice must reach the script as a real JS array", tun["dns-hijack"])
	}
	if len(list) != 2 || list[0] != "any:53" || list[1] != "any:5353" {
		t.Errorf("dns-hijack = %#v, want [any:53 any:5353]", list)
	}
}

// The regression this whole conversion exists for.
func TestScriptArithmeticKeepsPortsIntegral(t *testing.T) {
	after, res := runScript(t, `
function main(config) {
  config['mixed-port'] = config['mixed-port'] * 1
  config.tun.mtu = config.tun.mtu / 2 + 500
  config['proxy-groups'][0].interval = config['proxy-groups'][0].interval + 0.0
  return config
}`, scriptRoundTripConfig())
	if !res.Applied {
		t.Fatalf("script failed: %s", res.Err)
	}

	if got, ok := after["mixed-port"].(int); !ok || got != 7890 {
		t.Errorf("mixed-port = %#v, want int 7890 (JS arithmetic must not leave a float behind)", after["mixed-port"])
	}
	if got, ok := after["tun"].(map[string]any)["mtu"].(int); !ok || got != 5000 {
		t.Errorf("tun.mtu = %#v, want int 5000", after["tun"].(map[string]any)["mtu"])
	}
	group := after["proxy-groups"].([]any)[0].(map[string]any)
	if got, ok := group["interval"].(int); !ok || got != 300 {
		t.Errorf("proxy-groups[0].interval = %#v, want int 300", group["interval"])
	}

	out, err := marshalRuntimeYAML(after)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	if !strings.Contains(string(out), "mixed-port: 7890") {
		t.Errorf("generated YAML lost the integer port:\n%s", out)
	}
}

// A genuinely fractional value stays a float — normalization must not round.
func TestScriptKeepsFractionalNumbers(t *testing.T) {
	after, res := runScript(t, `function main(config) { config.ratio = 1.5; return config }`, map[string]any{})
	if !res.Applied {
		t.Fatalf("script failed: %s", res.Err)
	}
	if got, ok := after["ratio"].(float64); !ok || got != 1.5 {
		t.Errorf("ratio = %#v, want float64 1.5", after["ratio"])
	}
}

func TestScriptRebuiltGroupsKeepPerValueTypes(t *testing.T) {
	after, res := runScript(t, `
function main(config) {
  config['proxy-groups'] = config.proxies.map(function (p, i) {
    return {
      name: 'G-' + p.name,
      type: 'select',
      interval: 300 + i,
      lazy: true,
      proxies: [p.name, 'DIRECT'],
      nested: { timeout: 5, weights: [1, 2, 3] }
    }
  })
  return config
}`, scriptRoundTripConfig())
	if !res.Applied {
		t.Fatalf("script failed: %s", res.Err)
	}

	groups, ok := after["proxy-groups"].([]any)
	if !ok || len(groups) != 1 {
		t.Fatalf("proxy-groups = %#v, want a single-element list", after["proxy-groups"])
	}
	g := groups[0].(map[string]any)
	if g["name"] != "G-n1" {
		t.Errorf("name = %#v, want string G-n1", g["name"])
	}
	if v, ok := g["interval"].(int); !ok || v != 300 {
		t.Errorf("interval = %#v, want int 300", g["interval"])
	}
	if v, ok := g["lazy"].(bool); !ok || !v {
		t.Errorf("lazy = %#v, want bool true", g["lazy"])
	}
	if list, ok := g["proxies"].([]any); !ok || len(list) != 2 || list[0] != "n1" {
		t.Errorf("proxies = %#v, want [n1 DIRECT]", g["proxies"])
	}
	nested := g["nested"].(map[string]any)
	if v, ok := nested["timeout"].(int); !ok || v != 5 {
		t.Errorf("nested.timeout = %#v, want int 5", nested["timeout"])
	}
	weights, ok := nested["weights"].([]any)
	if !ok || len(weights) != 3 {
		t.Fatalf("nested.weights = %#v, want a 3-element list", nested["weights"])
	}
	for i, w := range weights {
		if v, ok := w.(int); !ok || v != i+1 {
			t.Errorf("nested.weights[%d] = %#v, want int %d", i, w, i+1)
		}
	}
}

// Unrepresentable values are refused BY PATH, so the user can find them.
func TestScriptUnrepresentableValuesAreRefused(t *testing.T) {
	cases := map[string]struct{ src, wantPath, wantWhat string }{
		"NaN": {
			src:      `function main(config) { config.dns.ttl = 0/0; return config }`,
			wantPath: "config.dns.ttl", wantWhat: "NaN",
		},
		"Infinity": {
			src:      `function main(config) { config.dns.ttl = 1/0; return config }`,
			wantPath: "config.dns.ttl", wantWhat: "Infinity",
		},
		"function": {
			src:      `function main(config) { config.hook = function () {}; return config }`,
			wantPath: "config.hook", wantWhat: "function",
		},
		"cycle": {
			src:      `function main(config) { config.self = config; return config }`,
			wantPath: "config.self", wantWhat: "cyclic",
		},
		"nested in array": {
			src:      `function main(config) { config.rules = ['MATCH,DIRECT', 0/0]; return config }`,
			wantPath: "config.rules[1]", wantWhat: "NaN",
		},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			before := scriptRoundTripConfig()
			after, res := runScript(t, tc.src, before)
			if res.Applied {
				t.Fatalf("script returning %s must be refused", name)
			}
			if !strings.Contains(res.Err, tc.wantPath) {
				t.Errorf("error %q should name the offending path %q", res.Err, tc.wantPath)
			}
			if !strings.Contains(res.Err, tc.wantWhat) {
				t.Errorf("error %q should say what was wrong (%q)", res.Err, tc.wantWhat)
			}
			// And the pre-script config is what generation continues from.
			if after["mixed-port"] != 7890 || after["log-level"] != "info" {
				t.Errorf("failed script must return the untouched config, got %#v / %#v", after["mixed-port"], after["log-level"])
			}
		})
	}
}

// Objects JS has but YAML does not (Date, Map, Set, RegExp) must be named, not
// silently written out as empty maps.
func TestScriptExoticObjectsAreNamed(t *testing.T) {
	for _, tc := range []struct{ name, src, want string }{
		{"Date", `function main(config) { config.when = new Date(); return config }`, "Date"},
		{"Map", `function main(config) { config.m = new Map(); return config }`, "Map"},
		{"Set", `function main(config) { config.s = new Set(); return config }`, "Set"},
		{"RegExp", `function main(config) { config.r = /x/; return config }`, "RegExp"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			_, res := runScript(t, tc.src, map[string]any{})
			if res.Applied {
				t.Fatalf("a %s in the config must be refused, not written out", tc.name)
			}
			if !strings.Contains(res.Err, tc.want) {
				t.Errorf("error %q should name the %s type", res.Err, tc.want)
			}
		})
	}
}

// null and undefined are legitimate: YAML has null, and a script that deletes a
// key by assigning undefined should not blow up.
func TestScriptNullAndUndefinedAreAccepted(t *testing.T) {
	after, res := runScript(t, `function main(config) { config.a = null; config.b = undefined; return config }`, map[string]any{})
	if !res.Applied {
		t.Fatalf("script failed: %s", res.Err)
	}
	if v, present := after["a"]; !present || v != nil {
		t.Errorf("a = %#v (present=%v), want nil", v, present)
	}
	if v, present := after["b"]; !present || v != nil {
		t.Errorf("b = %#v (present=%v), want nil", v, present)
	}
}
