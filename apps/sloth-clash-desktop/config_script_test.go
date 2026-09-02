package main

import (
	"strings"
	"testing"
)

func testScriptContext() scriptContext {
	return scriptContext{Traffic: "tun", Platform: "windows", AppVersion: "test"}
}

// runScript is the common "run this source over this config" helper.
func runScript(t *testing.T, src string, cfg map[string]any) (map[string]any, scriptResult) {
	t.Helper()
	out, res := runProfileScript(src, cfg, testScriptContext())
	if out == nil {
		t.Fatalf("runProfileScript returned a nil config — it must always return a usable map")
	}
	return out, res
}

func TestScriptTransformsConfig(t *testing.T) {
	cfg := map[string]any{"log-level": "info", "mixed-port": 7890}
	out, res := runScript(t, `function main(config) { config['log-level'] = 'debug'; return config }`, cfg)

	if !res.Applied {
		t.Fatalf("script should have applied, got error: %s", res.Err)
	}
	if out["log-level"] != "debug" {
		t.Errorf("log-level = %v, want debug", out["log-level"])
	}
	if out["mixed-port"] != 7890 {
		t.Errorf("mixed-port = %#v, want int 7890 (untouched values must keep their type)", out["mixed-port"])
	}
	if cfg["log-level"] != "info" {
		t.Errorf("the caller's map was mutated (log-level = %v); the script must work on a copy", cfg["log-level"])
	}
}

// The sandbox is empty BY CONSTRUCTION: goja ships none of these and we inject
// nothing but console. This test is the tripwire for someone later "helpfully"
// adding a host binding.
func TestScriptSandboxHasNoHostAccess(t *testing.T) {
	forbidden := []string{
		"require", "fetch", "process", "setTimeout", "setInterval", "clearTimeout",
		"Deno", "global", "globalThis.fs", "globalThis.child_process", "XMLHttpRequest",
		"WebSocket", "importScripts", "Buffer", "__dirname", "eval2",
	}
	for _, name := range forbidden {
		t.Run(name, func(t *testing.T) {
			src := `function main(config) { config.probe = (typeof ` + name + `); return config }`
			out, res := runScript(t, src, map[string]any{})
			if !res.Applied {
				// A bare identifier that does not exist throws a ReferenceError —
				// that is also "not available", which is what we are asserting.
				if !strings.Contains(res.Err, "not defined") {
					t.Fatalf("unexpected failure for %s: %s", name, res.Err)
				}
				return
			}
			if out["probe"] != "undefined" {
				t.Errorf("%s is %v inside the sandbox, want undefined — the sandbox must expose no host API", name, out["probe"])
			}
		})
	}
}

// console is the one host object we DO provide, and it must stay bounded.
func TestScriptConsoleIsCaptured(t *testing.T) {
	_, res := runScript(t, `
function main(config) {
  console.log('kept', 3, 'nodes')
  console.warn('careful')
  console.error('bad')
  return config
}`, map[string]any{})

	if !res.Applied {
		t.Fatalf("script failed: %s", res.Err)
	}
	joined := strings.Join(res.Console, "\n")
	for _, want := range []string{"kept 3 nodes", "warn: careful", "error: bad"} {
		if !strings.Contains(joined, want) {
			t.Errorf("console output %q missing %q", joined, want)
		}
	}
	if res.ConsoleTruncated {
		t.Errorf("three lines should not trip the truncation marker")
	}
}

func TestScriptConsoleTruncates(t *testing.T) {
	_, res := runScript(t, `
function main(config) {
  for (var i = 0; i < 100000; i++) { console.log('spam line number ' + i) }
  return config
}`, map[string]any{})

	if !res.ConsoleTruncated {
		t.Errorf("runaway logging must be truncated with a marker")
	}
	if len(res.Console) > profileScriptMaxConsoleLines {
		t.Errorf("captured %d lines, want at most %d", len(res.Console), profileScriptMaxConsoleLines)
	}
}

// console.log of an object must show its shape, not [object Object] — this is a
// debugging tool or it is nothing.
func TestScriptConsoleRendersObjects(t *testing.T) {
	_, res := runScript(t, `function main(config) { console.log({a: 1, b: [2, 3]}); return config }`, map[string]any{})
	joined := strings.Join(res.Console, "\n")
	if !strings.Contains(joined, `"a":1`) || !strings.Contains(joined, `"b":[2,3]`) {
		t.Errorf("object logging rendered as %q, want JSON-ish structure", joined)
	}
}

func TestScriptEmptyShortCircuits(t *testing.T) {
	cfg := map[string]any{"mode": "rule"}
	out, res := runScript(t, "   \n\t ", cfg)
	if res.Ran {
		t.Errorf("an empty script must not construct an engine (Ran = true)")
	}
	if res.Applied || res.Err != "" {
		t.Errorf("empty script must be a silent no-op, got applied=%v err=%q", res.Applied, res.Err)
	}
	if len(out) != 1 || out["mode"] != "rule" {
		t.Errorf("empty script changed the config: %#v", out)
	}
}

func TestScriptOverSizeCapIsRefused(t *testing.T) {
	src := "function main(config) { return config } // " + strings.Repeat("x", maxProfileScriptBytes)
	cfg := map[string]any{"mode": "rule"}
	out, res := runScript(t, src, cfg)
	if res.Applied {
		t.Fatalf("an oversized script must not run")
	}
	if !strings.Contains(res.Err, "limit") {
		t.Errorf("error %q should name the size limit", res.Err)
	}
	if out["mode"] != "rule" {
		t.Errorf("config must be returned untouched")
	}
}

func TestScriptContextIsExposed(t *testing.T) {
	out, res := runScript(t, `
function main(config, ctx) {
  config.seen = ctx.traffic + '/' + ctx.platform + '/' + ctx.appVersion
  return config
}`, map[string]any{})
	if !res.Applied {
		t.Fatalf("script failed: %s", res.Err)
	}
	if out["seen"] != "tun/windows/test" {
		t.Errorf("ctx = %v, want tun/windows/test", out["seen"])
	}
}

// Each run gets a fresh interpreter, so nothing a script leaves behind can
// reach the next generation (or another profile).
func TestScriptStateDoesNotLeakBetweenRuns(t *testing.T) {
	src := `
function main(config) {
  config.before = (typeof leaked)
  leaked = 'yes'
  return config
}`
	first, res1 := runScript(t, src, map[string]any{})
	second, res2 := runScript(t, src, map[string]any{})
	if !res1.Applied || !res2.Applied {
		t.Fatalf("script failed: %s / %s", res1.Err, res2.Err)
	}
	if first["before"] != "undefined" || second["before"] != "undefined" {
		t.Errorf("global leaked between runs: first=%v second=%v", first["before"], second["before"])
	}
}
