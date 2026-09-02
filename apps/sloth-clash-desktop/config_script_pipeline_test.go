package main

import (
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"gopkg.in/yaml.v3"
)

// The script step inside the real pipeline: what a script may change, what it
// can never win, and how failures degrade.

// runPipelineWithScript runs the representative profile through the real
// pipeline with a script and returns the resulting config plus the script result.
func runPipelineWithScript(t *testing.T, script string, enableTun bool) (map[string]any, scriptResult) {
	t.Helper()
	withConnectionPrefs(t, ConnectionSettings{})
	m := representativeFullProfile()
	var res scriptResult
	if err := finalizeRuntimeConfigPipelineWithScript(
		m, t.TempDir(), 54333, 9097, "s3cr3t", "tun", true, enableTun, script, &res,
	); err != nil {
		t.Fatalf("pipeline error: %v", err)
	}
	return m, res
}

func TestPipelineAppliesScript(t *testing.T) {
	m, res := runPipelineWithScript(t, `
function main(config) {
  config['log-level'] = 'debug'
  config.dns.listen = ':1054'
  config.tun.stack = 'system'
  config.sniffer.enable = false
  config.rules = ['DOMAIN-SUFFIX,scripted.example,PROXY', 'MATCH,PROXY']
  config.proxies = config.proxies.map(function (p) { p.name = 'S-' + p.name; return p })
  config['proxy-groups'] = [{ name: 'PROXY', type: 'select', proxies: ['S-n1', 'DIRECT'] }]
  return config
}`, true)

	if !res.Applied {
		t.Fatalf("script should have applied: %s", res.Err)
	}
	if m["log-level"] != "debug" {
		t.Errorf("log-level = %v, want debug", m["log-level"])
	}
	if got := m["dns"].(map[string]any)["listen"]; got != ":1054" {
		t.Errorf("dns.listen = %v, want :1054 — the script owns dns", got)
	}
	if got := m["tun"].(map[string]any)["stack"]; got != "system" {
		t.Errorf("tun.stack = %v, want system — the script owns tun", got)
	}
	if got := m["sniffer"].(map[string]any)["enable"]; got != false {
		t.Errorf("sniffer.enable = %v, want false — the script owns sniffer", got)
	}
	rules, _ := m["rules"].([]any)
	if len(rules) != 2 || !strings.Contains(rules[0].(string), "scripted.example") {
		t.Errorf("rules = %#v, want the script's rules", m["rules"])
	}
	proxies := m["proxies"].([]any)
	if name := proxies[0].(map[string]any)["name"]; name != "S-n1" {
		t.Errorf("proxies[0].name = %v, want S-n1", name)
	}
}

// The core of the whole design: a script may reshape anything EXCEPT the way the
// app reaches its own core.
func TestPipelineHostileScriptCannotBlindTheApp(t *testing.T) {
	m, res := runPipelineWithScript(t, `
function main(config) {
  delete config['external-controller']
  config.secret = 'stolen'
  config['mixed-port'] = 0
  config['socks-port'] = 1080
  config.port = 1081
  return config
}`, true)

	if !res.Applied {
		t.Fatalf("the hostile script should still run and be applied: %s", res.Err)
	}
	if m["mixed-port"] != 54333 {
		t.Errorf("mixed-port = %#v, want 54333 — a script must not be able to zero our port", m["mixed-port"])
	}
	if m["external-controller"] != "127.0.0.1:9097" {
		t.Errorf("external-controller = %#v, want ours back — deleting it would blind the whole UI", m["external-controller"])
	}
	if m["secret"] != "s3cr3t" {
		t.Errorf("secret = %#v, want ours back", m["secret"])
	}
	if m["socks-port"] != 0 || m["port"] != 0 {
		t.Errorf("socks-port/port = %#v/%#v, want 0/0", m["socks-port"], m["port"])
	}
}

// Same invariants when the app runs the core WITHOUT a controller: the script
// must not be able to add one back.
func TestPipelineScriptCannotAddExternalController(t *testing.T) {
	withConnectionPrefs(t, ConnectionSettings{})
	m := representativeFullProfile()
	var res scriptResult
	if err := finalizeRuntimeConfigPipelineWithScript(
		m, t.TempDir(), 54333, 9097, "s3cr3t", "tun", false /* withExternalController */, true,
		`function main(config) { config['external-controller'] = '0.0.0.0:9090'; return config }`, &res,
	); err != nil {
		t.Fatalf("pipeline error: %v", err)
	}
	if !res.Applied {
		t.Fatalf("script failed: %s", res.Err)
	}
	if _, present := m["external-controller"]; present {
		t.Errorf("external-controller = %#v, want it removed — we run this core without one", m["external-controller"])
	}
}

// Every failure mode degrades to "generated without the script", never to a
// refused connect.
func TestPipelineScriptFailuresDegrade(t *testing.T) {
	cases := map[string]struct {
		src      string
		wantErr  string
		wantLine bool
	}{
		"syntax error":    {src: `function main(config) { return config `, wantErr: "syntax error", wantLine: true},
		"missing main":    {src: `var notMain = function (c) { return c }`, wantErr: "no main(config) function"},
		"main not a func": {src: `var main = 42`, wantErr: "not a function"},
		"throws":          {src: `function main(config) { throw new Error('no nodes matched') }`, wantErr: "no nodes matched"},
		"returns nothing": {src: `function main(config) { }`, wantErr: "must return an object"},
		"returns string":  {src: `function main(config) { return 'nope' }`, wantErr: "must return an object"},
		"returns array":   {src: `function main(config) { return [1,2] }`, wantErr: "must return an object"},
		"unrepresentable": {src: `function main(config) { config.x = 0/0; return config }`, wantErr: "NaN"},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			m, res := runPipelineWithScript(t, tc.src, true)
			if res.Applied {
				t.Fatalf("%s must not be applied", name)
			}
			if !strings.Contains(res.Err, tc.wantErr) {
				t.Errorf("error %q should contain %q", res.Err, tc.wantErr)
			}
			if tc.wantLine && res.Line == 0 {
				t.Errorf("a syntax error should report a line, got %d:%d (%s)", res.Line, res.Column, res.Err)
			}
			// ...and the config is the one the pipeline would have produced anyway.
			if m["mixed-port"] != 54333 || m["external-controller"] != "127.0.0.1:9097" {
				t.Errorf("degraded config lost its invariants: %#v / %#v", m["mixed-port"], m["external-controller"])
			}
			if m["log-level"] == "debug" {
				t.Errorf("a failed script must leave no partial changes behind")
			}
		})
	}
}

// A script that never returns must cost us the budget and nothing more.
func TestPipelineScriptTimeoutIsInterrupted(t *testing.T) {
	m, res := runPipelineWithScript(t, `function main(config) { while (true) {} }`, true)
	if res.Applied {
		t.Fatalf("an endless script must not be applied")
	}
	if !strings.Contains(res.Err, "time limit") {
		t.Errorf("error %q should report a timeout", res.Err)
	}
	if res.DurationMS > int64(profileScriptTimeout/1e6)+2000 {
		t.Errorf("script ran for %dms, well past the %s budget", res.DurationMS, profileScriptTimeout)
	}
	if m["mixed-port"] != 54333 {
		t.Errorf("config must still be usable after a timeout")
	}
}

func TestPipelineScriptDeepRecursionIsBounded(t *testing.T) {
	_, res := runPipelineWithScript(t, `
function boom(n) { return boom(n + 1) }
function main(config) { boom(0); return config }`, true)
	if res.Applied {
		t.Fatalf("runaway recursion must not be applied")
	}
	if !strings.Contains(res.Err, "call stack") && !strings.Contains(strings.ToLower(res.Err), "stack") {
		t.Errorf("error %q should mention the call stack bound", res.Err)
	}
}

// The empty-script path must not even construct an engine, and must generate the
// same bytes as a pipeline that never heard of scripts.
func TestPipelineWithoutScriptIsUnchanged(t *testing.T) {
	withConnectionPrefs(t, ConnectionSettings{})
	plain := representativeFullProfile()
	if err := finalizeRuntimeConfigPipeline(plain, t.TempDir(), 54333, 9097, "s3cr3t", "tun", true, true); err != nil {
		t.Fatalf("pipeline error: %v", err)
	}
	wantYAML, err := marshalRuntimeYAML(plain)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}

	for _, script := range []string{"", "   ", "\n\t\n"} {
		scripted, res := runPipelineWithScript(t, script, true)
		if res.Ran {
			t.Errorf("script %q must not construct an engine", script)
		}
		gotYAML, err := marshalRuntimeYAML(scripted)
		if err != nil {
			t.Fatalf("marshal: %v", err)
		}
		if string(gotYAML) != string(wantYAML) {
			t.Errorf("empty script changed the generated config:\n%s", gotYAML)
		}
	}
}

// A script sees the config AFTER our overlays — that is the whole point of
// running it late.
func TestScriptObservesPostOverlayConfig(t *testing.T) {
	m, res := runPipelineWithScript(t, `
function main(config, ctx) {
  config.observed = {
    traffic: ctx.traffic,
    dnsListen: config.dns.listen,
    tunEnabled: config.tun.enable,
    hasController: !!config['external-controller']
  }
  return config
}`, true)
	if !res.Applied {
		t.Fatalf("script failed: %s", res.Err)
	}
	obs, ok := m["observed"].(map[string]any)
	if !ok {
		t.Fatalf("observed = %#v", m["observed"])
	}
	if obs["traffic"] != "tun" {
		t.Errorf("ctx.traffic = %v, want tun", obs["traffic"])
	}
	if obs["dnsListen"] != ":1053" {
		t.Errorf("the script saw dns.listen = %v, want our overlaid :1053", obs["dnsListen"])
	}
	if obs["tunEnabled"] != true {
		t.Errorf("the script saw tun.enable = %v, want true", obs["tunEnabled"])
	}
	if obs["hasController"] != true {
		t.Errorf("the script should see the external-controller we set")
	}
}

// Scripts come from the user's editor and nowhere else. A subscription that
// ships a `script:` block must be inert.
func TestSubscriptionBodyCannotInjectScript(t *testing.T) {
	t.Parallel()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`
script: |
  function main(config) { config['log-level'] = 'debug'; return config }
code: |
  function main(config) { config['log-level'] = 'debug'; return config }
log-level: info
proxy-groups:
  - name: MainGroup
    type: select
    proxies: [DIRECT]
rules:
  - MATCH,MainGroup
`))
	}))
	defer srv.Close()

	tmp := t.TempDir()
	if _, err := tryWriteMergedFullProfile(tmp, srv.URL, "", "", "", "", 9090, 7890, "secret", "tun", true, true); err != nil {
		t.Fatalf("pipeline: %v", err)
	}
	b, err := os.ReadFile(filepath.Join(tmp, "config.yaml"))
	if err != nil {
		t.Fatalf("read config: %v", err)
	}
	var got map[string]any
	if err := yaml.Unmarshal(b, &got); err != nil {
		t.Fatalf("parse config: %v", err)
	}
	if got["log-level"] == "debug" {
		t.Fatalf("a script shipped by the subscription was EXECUTED — provider-controlled code execution")
	}
}

// The structural guarantee behind the requirement above: only the user's own
// editing action writes the field. If a future change starts assigning it from
// the subscription/import path, this fails loudly.
func TestOnlyTheEditorWritesScriptOverride(t *testing.T) {
	entries, err := os.ReadDir(".")
	if err != nil {
		t.Fatalf("read package dir: %v", err)
	}
	allowed := map[string]bool{"profile_script_api.go": true}
	for _, e := range entries {
		name := e.Name()
		if e.IsDir() || !strings.HasSuffix(name, ".go") || strings.HasSuffix(name, "_test.go") {
			continue
		}
		src, err := os.ReadFile(name)
		if err != nil {
			t.Fatalf("read %s: %v", name, err)
		}
		for i, line := range strings.Split(string(src), "\n") {
			trimmed := strings.TrimSpace(line)
			if !strings.Contains(trimmed, ".ScriptOverride =") && !strings.Contains(trimmed, ".ScriptOverride=") {
				continue
			}
			if allowed[name] {
				continue
			}
			t.Errorf("%s:%d assigns ScriptOverride outside the editor path (%q) — a script must only ever come from the user", name, i+1, trimmed)
		}
	}
}

// The invariant re-assertion has to produce a config the CORE accepts, not just
// one our own validator likes. Runs against the shipped binary when the required
// gate provides it.
func TestHostileScriptConfigStillPassesTheCore(t *testing.T) {
	bin := strings.TrimSpace(os.Getenv("SLOTH_MIHOMO_BIN"))
	if bin == "" {
		t.Skip("set SLOTH_MIHOMO_BIN to run the hostile-script core check")
	}
	m, res := runPipelineWithScript(t, `
function main(config) {
  delete config['external-controller']
  config['mixed-port'] = 0
  config.secret = 'stolen'
  delete config.tun['route-exclude-address']
  config.rules = ['MATCH,PROXY']
  return config
}`, true)
	if !res.Applied {
		t.Fatalf("script failed: %s", res.Err)
	}

	dir := t.TempDir()
	out, err := marshalRuntimeYAML(m)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	if err := os.WriteFile(filepath.Join(dir, "config.yaml"), out, 0o644); err != nil {
		t.Fatalf("write: %v", err)
	}
	if err := runConfigPreflight(bin, dir); err != nil {
		t.Fatalf("the core rejected the config a hostile script produced:\n%v\n---\n%s", err, out)
	}
}
