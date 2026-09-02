package main

import (
	"embed"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// The editor-facing surface: save/clear, the recorded result, and the dry-run
// preview that must never touch the live runtime config or the core.

const scriptTestSubscription = `
log-level: info
proxy-groups:
  - name: MainGroup
    type: select
    proxies: [DIRECT]
rules:
  - MATCH,MainGroup
`

// isolateAppDataRoot asserts this test cannot reach the developer's real app
// data, and returns the root it will use instead.
//
// slothDataRoot() already redirects every `go test` binary to a throwaway
// directory (see core_manager.go) — this is the second belt: it overrides the
// per-platform config-dir variables as well, so the isolation survives even if
// someone runs the suite with SLOTH_ALLOW_REAL_DATA_ROOT=1, and it fails loudly
// rather than silently writing to the real root.
//
// Why this is not optional: App methods persist through persistProfilesLocked.
// A single unguarded test rewrites the real profiles.json, and the next app
// start then deletes every real runtime dir as an orphan. That is not
// hypothetical — it happened on 2026-09-02 and cost three profiles.
func isolateAppDataRoot(t *testing.T) string {
	t.Helper()
	// Resolve the REAL root before overriding anything, so we can prove we are
	// not pointing at it.
	realParent, err := os.UserConfigDir()
	if err != nil {
		t.Fatalf("user config dir: %v", err)
	}
	realRoot := filepath.Join(realParent, "SlothClash")

	tmp := t.TempDir()
	t.Setenv("AppData", tmp)         // windows
	t.Setenv("XDG_CONFIG_HOME", tmp) // linux
	t.Setenv("HOME", tmp)            // darwin (…/Library/Application Support)

	got, err := slothDataRoot()
	if err != nil {
		t.Fatalf("data root: %v", err)
	}
	if strings.EqualFold(got, realRoot) {
		t.Fatalf("data root resolved to the REAL app data (%s) — this test would destroy the developer's profiles", got)
	}
	return got
}

// newScriptTestApp builds an app holding one subscription profile served by a
// local test server, so generation never leaves the machine, and pins the app
// data root to a temp dir so persistence never leaves the test.
func newScriptTestApp(t *testing.T) (*App, Profile) {
	t.Helper()
	isolateAppDataRoot(t)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(scriptTestSubscription))
	}))
	t.Cleanup(srv.Close)

	var b embed.FS
	a := NewApp(b)
	p := Profile{ID: "profile-script-test", Name: "script test", Type: "subscription", URL: srv.URL}
	a.profiles = []Profile{p}
	a.state.Profile.Profiles = a.profiles
	a.state.Profile.ActiveProfileID = p.ID
	return a, p
}

func TestPreviewShowsTheDifferenceWithoutTouchingAnything(t *testing.T) {
	a, p := newScriptTestApp(t)

	root, err := slothDataRoot()
	if err != nil {
		t.Fatalf("data root: %v", err)
	}
	liveDir := filepath.Join(root, "runtime", p.ID)
	// Nothing about this profile exists on disk, and preview must keep it that way.
	if _, err := os.Stat(liveDir); err == nil {
		t.Fatalf("test profile dir unexpectedly exists: %s", liveDir)
	}

	preview, err := a.PreviewProfileScript(p.ID, `
function main(config) {
  console.log('groups: ' + config['proxy-groups'].length)
  config['log-level'] = 'debug'
  return config
}`)
	if err != nil {
		t.Fatalf("preview failed: %v", err)
	}

	if !preview.Result.Applied {
		t.Fatalf("preview reported a failure: %s", preview.Result.Err)
	}
	if !preview.Changed {
		t.Errorf("preview says nothing changed, but the script rewrites log-level")
	}
	if !strings.Contains(preview.WithScript, "log-level: debug") {
		t.Errorf("with-script document missing the script's change:\n%s", preview.WithScript)
	}
	if strings.Contains(preview.WithoutScript, "log-level: debug") {
		t.Errorf("without-script document must be the un-scripted baseline:\n%s", preview.WithoutScript)
	}
	if len(preview.Result.Console) == 0 || !strings.Contains(strings.Join(preview.Result.Console, "\n"), "groups: 1") {
		t.Errorf("captured console = %#v, want the script's log line", preview.Result.Console)
	}
	if _, err := os.Stat(liveDir); err == nil {
		t.Errorf("preview created the live runtime dir %s — it must be a dry run", liveDir)
	}
	// Nothing was saved on the profile either.
	if a.profiles[0].ScriptOverride != "" {
		t.Errorf("preview stored the script on the profile: %q", a.profiles[0].ScriptOverride)
	}
}

func TestPreviewReportsFailuresLikeTheConnectPath(t *testing.T) {
	a, p := newScriptTestApp(t)
	preview, err := a.PreviewProfileScript(p.ID, `function main(config) { throw new Error('no nodes matched') }`)
	if err != nil {
		t.Fatalf("preview returned a hard error, want a reported script failure: %v", err)
	}
	if preview.Result.Applied {
		t.Fatalf("a throwing script must not be applied")
	}
	if !strings.Contains(preview.Result.Err, "no nodes matched") {
		t.Errorf("error %q should carry the thrown message", preview.Result.Err)
	}
	// A failed script degrades to the un-scripted document — same as connecting.
	if preview.WithScript != preview.WithoutScript {
		t.Errorf("a failed preview must show the config generation would actually use")
	}
	if preview.Changed {
		t.Errorf("a failed script changes nothing")
	}
}

// What the preview shows is what applying it produces: the same pipeline, so the
// script's effect must appear in a real generation too.
func TestPreviewReflectsTheRealPipeline(t *testing.T) {
	a, p := newScriptTestApp(t)
	script := `function main(config) { config['log-level'] = 'debug'; config.mode = 'global'; return config }`

	preview, err := a.PreviewProfileScript(p.ID, script)
	if err != nil {
		t.Fatalf("preview failed: %v", err)
	}

	dir := t.TempDir()
	var res scriptResult
	if err := a.writeRuntimeConfigWithScript(
		dir, p.URL, "", "", "", "", script, 0, 7890, "preview", "proxy", false, false, &res,
	); err != nil {
		t.Fatalf("real generation failed: %v", err)
	}
	applied, err := os.ReadFile(filepath.Join(dir, "config.yaml"))
	if err != nil {
		t.Fatalf("read applied config: %v", err)
	}
	if string(applied) != preview.WithScript {
		t.Errorf("preview and applied config differ.\n--- preview ---\n%s\n--- applied ---\n%s", preview.WithScript, applied)
	}
}

func TestSaveAndClearScriptOverride(t *testing.T) {
	a, p := newScriptTestApp(t)
	// Pretend a previous generation recorded a failure.
	a.profiles[0].ScriptError = "old failure"
	a.profiles[0].ScriptErrorLine = 7

	if _, err := a.SetProfileScriptOverride(p.ID, `function main(config) { return config }`); err != nil {
		t.Fatalf("save failed: %v", err)
	}
	if a.profiles[0].ScriptOverride == "" {
		t.Errorf("script was not stored")
	}
	if a.profiles[0].ScriptError != "" || a.profiles[0].ScriptErrorLine != 0 {
		t.Errorf("saving must clear the previous error, got %q at line %d", a.profiles[0].ScriptError, a.profiles[0].ScriptErrorLine)
	}

	if _, err := a.ClearProfileScriptOverride(p.ID); err != nil {
		t.Fatalf("clear failed: %v", err)
	}
	if a.profiles[0].ScriptOverride != "" {
		t.Errorf("clear left %q behind", a.profiles[0].ScriptOverride)
	}
}

func TestSaveRejectsOversizedScript(t *testing.T) {
	a, p := newScriptTestApp(t)
	if _, err := a.SetProfileScriptOverride(p.ID, "function main(c){return c}"); err != nil {
		t.Fatalf("save failed: %v", err)
	}
	good := a.profiles[0].ScriptOverride

	oversized := strings.Repeat("x", maxProfileScriptBytes+1)
	_, err := a.SetProfileScriptOverride(p.ID, oversized)
	if err == nil {
		t.Fatalf("an oversized script must be refused")
	}
	if !strings.Contains(err.Error(), "limit") {
		t.Errorf("error %q should name the limit", err)
	}
	if a.profiles[0].ScriptOverride != good {
		t.Errorf("a refused save must leave the stored script untouched")
	}
}

func TestSaveUnknownProfileFails(t *testing.T) {
	a, _ := newScriptTestApp(t)
	if _, err := a.SetProfileScriptOverride("nope", "function main(c){return c}"); err == nil {
		t.Fatalf("saving to an unknown profile must fail")
	}
}

// The recorded result is what badges the profile in the UI; a successful run has
// to clear a previous failure.
func TestRecordProfileScriptResultClearsOnSuccess(t *testing.T) {
	a, p := newScriptTestApp(t)
	a.recordProfileScriptResult(p.ID, scriptResult{Ran: true, Err: "boom", Line: 3, Column: 5})
	if a.profiles[0].ScriptError != "boom" || a.profiles[0].ScriptErrorLine != 3 {
		t.Fatalf("failure was not recorded: %#v", a.profiles[0])
	}
	a.recordProfileScriptResult(p.ID, scriptResult{Ran: true, Applied: true, Console: []string{"ok"}})
	if a.profiles[0].ScriptError != "" || a.profiles[0].ScriptErrorLine != 0 {
		t.Errorf("a successful run must clear the recorded error, got %q line %d", a.profiles[0].ScriptError, a.profiles[0].ScriptErrorLine)
	}
}

func TestRecordProfileScriptResultIgnoresUnscriptedRuns(t *testing.T) {
	a, p := newScriptTestApp(t)
	a.profiles[0].ScriptError = "stale"
	a.recordProfileScriptResult(p.ID, scriptResult{}) // Ran == false
	if a.profiles[0].ScriptError != "stale" {
		t.Errorf("a generation with no script must not touch the recorded state")
	}
}
