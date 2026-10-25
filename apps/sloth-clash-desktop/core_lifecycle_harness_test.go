package main

// Core lifecycle harness: drives the REAL start / connect / reload / crash
// paths (startEmbeddedCore, runConnectJob, connectAfterCoreStarts,
// attemptCoreAutoRestart, applyRuntimeConfigWithGen) against a fake mihomo
// built from testdata/fakecore. The fake reaches the app the same way a dev
// build's core does — SLOTH_MIHOMO_PATH — so nothing in the start path is
// stubbed out.
//
// What keeps this safe and hermetic:
//   - slothDataRoot is a throwaway dir under `go test`, so runtime/<id>/ and
//     profiles never touch the developer's real data;
//   - osSystemProxyWritesAllowed is false under `go test`, so proxy-mode
//     Connect and stopCoreLocked never rewrite the OS proxy settings;
//   - profiles are "local" (body cache seeded), so config generation needs no
//     network; traffic is "proxy" (no TUN, no service takeover) and mode is
//     "global" (Home insight skips its direct-WAN lookup); the fake core never
//     opens the mixed-port, so the exit-IP probe fails on loopback at once;
//   - Service.Installed stays false, so the privileged-service branch of
//     startEmbeddedCore is never taken (it needs the real service; not covered).

import (
	"context"
	"crypto/rand"
	"embed"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"runtime"
	"strings"
	"sync"
	"testing"
	"time"

	"SlothClashDesktop/internal/runtimecfg"
)

var (
	fakeCoreOnce sync.Once
	fakeCoreDir  string
	fakeCorePath string
	fakeCoreErr  error
)

// TestMain is package-wide. Besides cleaning up the fake core build, it pins
// the debug JSON log (debugLog / traceEvent) to a throwaway file before any
// test runs. resolveDebugLogPath resolves os.UserConfigDir exactly once per
// process, so without this every traced test — the lifecycle harness alone
// writes hundreds of lines per run — appends to the developer's real
// %APPDATA%/SlothClash/runtime/logs/debug-cb9690.log, interleaved with the
// running app's own trace. Per-test env overrides (isolateAppDataRoot) cannot
// help here: whichever call comes first wins the sync.Once.
func TestMain(m *testing.M) {
	logDir, err := os.MkdirTemp("", "sloth-test-debug-log-*")
	if err != nil {
		fmt.Fprintf(os.Stderr, "debug log temp dir: %v\n", err)
		os.Exit(1)
	}
	debugLogPathOnce.Do(func() { debugLogPath = filepath.Join(logDir, "debug-cb9690.log") })
	if !strings.HasPrefix(resolveDebugLogPath(), logDir) {
		fmt.Fprintf(os.Stderr, "debug log already resolved to %s before TestMain\n", resolveDebugLogPath())
		os.Exit(1)
	}

	code := m.Run()
	if fakeCoreDir != "" {
		_ = os.RemoveAll(fakeCoreDir)
	}
	_ = os.RemoveAll(logDir)
	os.Exit(code)
}

// fakeCoreBinary builds testdata/fakecore once per test binary. Lazy, so test
// runs that never touch the lifecycle harness do not pay for the build.
func fakeCoreBinary(t *testing.T) string {
	t.Helper()
	fakeCoreOnce.Do(func() {
		goBin, err := exec.LookPath("go")
		if err != nil {
			goBin = filepath.Join(runtime.GOROOT(), "bin", "go")
		}
		dir, err := os.MkdirTemp("", "sloth-fakecore-*")
		if err != nil {
			fakeCoreErr = err
			return
		}
		fakeCoreDir = dir
		name := "sloth-mihomo-fake"
		if runtime.GOOS == "windows" {
			name += ".exe"
		}
		out := filepath.Join(dir, name)
		cmd := exec.Command(goBin, "build", "-o", out, "./testdata/fakecore")
		if b, err := cmd.CombinedOutput(); err != nil {
			fakeCoreErr = fmt.Errorf("build fake core: %v\n%s", err, b)
			return
		}
		fakeCorePath = out
	})
	if fakeCoreErr != nil {
		t.Fatal(fakeCoreErr)
	}
	return fakeCorePath
}

// fakeCoreControl mirrors testdata/fakecore's control struct.
type fakeCoreControl struct {
	PreflightError     string `json:"preflightError,omitempty"`
	NeverReady         bool   `json:"neverReady,omitempty"`
	BreakOnFirstReload bool   `json:"breakOnFirstReload,omitempty"`
	FailReload         bool   `json:"failReload,omitempty"`
}

const lifecycleTestBody = "" +
	"proxies:\n" +
	"  - {name: node-a, type: ss, server: 192.0.2.1, port: 443, cipher: aes-128-gcm, password: pw}\n" +
	"proxy-groups:\n" +
	"  - {name: PROXY, type: select, proxies: [node-a, DIRECT]}\n" +
	"rules:\n" +
	"  - MATCH,PROXY\n"

type lifecycleHarness struct {
	t   *testing.T
	app *App
}

// newLifecycleHarness returns an App with one active local profile whose core
// is the fake. Every harness gets its own profile ID, hence its own runtime
// dir, control file and event log.
func newLifecycleHarness(t *testing.T, ctl fakeCoreControl) (*lifecycleHarness, Profile) {
	t.Helper()
	// Build (or reuse) the fake before isolating: the build is a `go build`
	// child, and isolateAppDataRoot repoints HOME, which on Linux/macOS would
	// send it to an empty module and build cache.
	t.Setenv("SLOTH_MIHOMO_PATH", fakeCoreBinary(t))
	// Second belt on top of slothDataRoot's `go test` redirect: holds even
	// under SLOTH_ALLOW_REAL_DATA_ROOT=1. Registered before h.shutdown, so its
	// TempDir is removed only after the core is stopped (cleanups run LIFO).
	isolateAppDataRoot(t)
	h := &lifecycleHarness{t: t, app: NewApp(embed.FS{})}
	p := h.addProfile(ctl)
	h.app.mu.Lock()
	h.app.state.Profile.ActiveProfileID = p.ID
	h.app.state.Traffic = "proxy"
	h.app.state.Mode.Current = "global"
	h.app.mu.Unlock()
	t.Cleanup(h.shutdown)
	return h, p
}

func (h *lifecycleHarness) addProfile(ctl fakeCoreControl) Profile {
	h.t.Helper()
	b := make([]byte, 6)
	_, _ = rand.Read(b)
	p := Profile{ID: "lc-" + hex.EncodeToString(b), Name: "lifecycle", Type: "local"}
	runtimecfg.WriteSubscriptionBodyCache(h.dataDir(p), []byte(lifecycleTestBody))
	h.setControl(p, ctl)
	h.app.mu.Lock()
	h.app.profiles = append(h.app.profiles, p)
	h.app.mu.Unlock()
	return p
}

func (h *lifecycleHarness) dataDir(p Profile) string {
	h.t.Helper()
	root, err := slothDataRoot()
	if err != nil {
		h.t.Fatal(err)
	}
	return filepath.Join(root, "runtime", p.ID)
}

// setControl rewrites the fake's control file; it is read once per fake
// process start (and per `-t` preflight run).
func (h *lifecycleHarness) setControl(p Profile, ctl fakeCoreControl) {
	h.t.Helper()
	b, _ := json.Marshal(ctl)
	if err := os.MkdirAll(h.dataDir(p), 0o755); err != nil {
		h.t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(h.dataDir(p), "fakecore.json"), b, 0o644); err != nil {
		h.t.Fatal(err)
	}
}

// events returns the fake core's event log lines without the leading pid.
func (h *lifecycleHarness) events(p Profile) []string {
	b, err := os.ReadFile(filepath.Join(h.dataDir(p), "fakecore-events.log"))
	if err != nil {
		return nil
	}
	var out []string
	for _, line := range strings.Split(strings.TrimSpace(string(b)), "\n") {
		if _, rest, ok := strings.Cut(strings.TrimSpace(line), " "); ok {
			out = append(out, rest)
		}
	}
	return out
}

func (h *lifecycleHarness) countEvents(p Profile, prefix string) int {
	n := 0
	for _, e := range h.events(p) {
		if strings.HasPrefix(e, prefix) {
			n++
		}
	}
	return n
}

// hasDiag reports whether the runtime-diag ring holds category with a message
// starting with outcome (traceEvent writes "<outcome> [dur] [err=...]").
func (h *lifecycleHarness) hasDiag(category, outcome string) bool {
	for _, ev := range h.app.GetRuntimeDiagEvents() {
		if ev.Category == category && strings.HasPrefix(ev.Message, outcome) {
			return true
		}
	}
	return false
}

func (h *lifecycleHarness) waitFor(what string, timeout time.Duration, cond func() bool) {
	h.t.Helper()
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		if cond() {
			return
		}
		time.Sleep(20 * time.Millisecond)
	}
	st := h.app.GetAppState()
	h.t.Fatalf("timed out after %s waiting for %s; connection=%+v core=%+v diag=%+v",
		timeout, what, st.Connection, st.Core, h.app.GetRuntimeDiagEvents())
}

func (h *lifecycleHarness) endpoint() (listen, secret string) {
	h.app.mu.RLock()
	defer h.app.mu.RUnlock()
	return h.app.core.listen, h.app.core.secret
}

// waitConnectSettled waits for the whole Connect job: connected + warmup done
// + the background Home-insight refresh finished (it would otherwise outlive
// the test and race the next one).
func (h *lifecycleHarness) waitConnectSettled() {
	h.t.Helper()
	h.waitFor("connected + warmup ok", 20*time.Second, func() bool {
		return h.app.GetAppState().Connection.Status == ConnConnected &&
			h.hasDiag("pipeline.connect.warmup", "ok")
	})
	h.waitFor("home insight refresh to finish", 20*time.Second, func() bool {
		st := h.app.GetAppState()
		return !h.app.insightRefreshRunning.Load() && (st.Insight.LastError != "" || st.Insight.ExitIP != "")
	})
}

// crashCore makes the running fake exit with a non-zero code, like a core
// that died under the user.
func (h *lifecycleHarness) crashCore() {
	h.t.Helper()
	listen, secret := h.endpoint()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	resp, err := coreDoWithEndpoint(ctx, listen, secret, http.MethodPost, "/debug/exit?code=3", nil)
	if err != nil {
		h.t.Fatalf("crash request: %v", err)
	}
	_ = resp.Body.Close()
}

func (h *lifecycleHarness) assertEndpointGone(listen, secret string) {
	h.t.Helper()
	if err := waitForCoreEndpointStop(context.Background(), "test", listen, secret, 5*time.Second); err != nil {
		h.t.Fatalf("core process still serving after stop: %v", err)
	}
}

// coreExitWatcherFrame matches a stack frame of the cmd.Wait goroutine that
// startEmbeddedCore spawns per launched core — its only `go func()` closure.
// That goroutine is what turns a core exit into state (crash -> ConnError +
// auto-restart, intentional stop -> nothing) and closes core.log afterwards.
// Frames are qualified by the module path ("SlothClashDesktop."), not "main.",
// so match any qualifier; the "created by" line never starts a match.
var coreExitWatcherFrame = regexp.MustCompile(`(?m)^[\w./-]+\.\(\*App\)\.startEmbeddedCore\.func\d+\(`)

// coreExitWatchers returns the IDs of the live core exit-watcher goroutines.
func coreExitWatchers() map[string]bool {
	buf := make([]byte, 1<<20)
	for {
		n := runtime.Stack(buf, true)
		if n < len(buf) {
			buf = buf[:n]
			break
		}
		buf = make([]byte, 2*len(buf))
	}
	ids := map[string]bool{}
	for _, g := range strings.Split(string(buf), "\n\n") {
		if !coreExitWatcherFrame.MatchString(g) {
			continue
		}
		// Header: "goroutine 123 [state]:".
		if f := strings.Fields(g); len(f) >= 2 && f[0] == "goroutine" {
			ids[f[1]] = true
		}
	}
	return ids
}

// waitExitWatchersDone blocks until every goroutine in ids has returned, i.e.
// each watched core's exit has been fully observed and acted on. This is the
// deterministic replacement for "sleep and hope cmd.Wait has fired".
func waitExitWatchersDone(ids map[string]bool, timeout time.Duration) error {
	deadline := time.Now().Add(timeout)
	for {
		live := coreExitWatchers()
		pending := 0
		for id := range ids {
			if live[id] {
				pending++
			}
		}
		if pending == 0 {
			return nil
		}
		if time.Now().After(deadline) {
			return fmt.Errorf("%d core exit watcher(s) still running after %s", pending, timeout)
		}
		time.Sleep(10 * time.Millisecond)
	}
}

// shutdown invalidates any in-flight job and kills the core, then waits for
// the old endpoint to go quiet and for the exit watchers to return, so no fake
// process (or its core.log handle) outlives its test.
func (h *lifecycleHarness) shutdown() {
	h.app.connectGen.Add(1)
	listen, secret := h.endpoint()
	watchers := coreExitWatchers()
	h.app.mu.Lock()
	h.app.stopCoreLocked()
	h.app.mu.Unlock()
	if listen != "" {
		_ = waitForCoreEndpointStop(context.Background(), "test", listen, secret, 5*time.Second)
	}
	if err := waitExitWatchersDone(watchers, 10*time.Second); err != nil {
		h.t.Errorf("shutdown: %v", err)
	}
}

func TestCoreLifecycleStartReadyReuseStop(t *testing.T) {
	h, p := newLifecycleHarness(t, fakeCoreControl{})
	a := h.app

	cold, err := a.ensureCoreForProfileEx(p, 0, false)
	if err != nil {
		t.Fatalf("cold start: %v", err)
	}
	if !cold {
		t.Fatal("first ensureCoreForProfileEx must report a cold start")
	}
	st := a.GetAppState()
	if !st.Core.Running || st.Core.Lifecycle != "running" || st.Core.Version != "fake-1.0.0" {
		t.Fatalf("core state after start = %+v, want running fake-1.0.0", st.Core)
	}
	if st.Core.MixedPort <= 0 || !strings.HasPrefix(st.Core.ControllerAddr, "127.0.0.1:") {
		t.Fatalf("core endpoints not published: %+v", st.Core)
	}
	a.mu.RLock()
	activeID := a.core.activeProfileID
	a.mu.RUnlock()
	if activeID != p.ID {
		t.Fatalf("core.activeProfileID = %q, want %q", activeID, p.ID)
	}
	if h.countEvents(p, "preflight ok") != 1 || h.countEvents(p, "start ") != 1 {
		t.Fatalf("want one preflight + one start, events=%q", h.events(p))
	}
	if st, err := os.Stat(filepath.Join(h.dataDir(p), "core.log")); err != nil || st.IsDir() {
		t.Fatalf("core.log not created: %v", err)
	}

	// Same profile, core healthy: reused, no second process.
	cold, err = a.ensureCoreForProfileEx(p, 0, false)
	if err != nil || cold {
		t.Fatalf("reuse: cold=%v err=%v, want reuse without restart", cold, err)
	}
	if n := h.countEvents(p, "start "); n != 1 {
		t.Fatalf("reuse spawned another core: %d starts", n)
	}

	// Intentional stop: process gone, state reset, and the exit is NOT
	// reported as a crash (connection status untouched).
	listen, secret := h.endpoint()
	watchers := coreExitWatchers()
	if len(watchers) == 0 {
		t.Fatal("no core exit watcher goroutine found for the running core")
	}
	a.mu.Lock()
	a.stopCoreLocked()
	a.mu.Unlock()
	h.assertEndpointGone(listen, secret)
	st = a.GetAppState()
	if st.Core.Running || st.Core.Lifecycle != "stopped" || st.Core.ControllerAddr != "" || st.Core.MixedPort != 0 {
		t.Fatalf("core state after stop = %+v", st.Core)
	}
	// The exit watcher has seen the killed process and returned: whatever it
	// was going to do to the state is done.
	if err := waitExitWatchersDone(watchers, 10*time.Second); err != nil {
		t.Fatal(err)
	}
	if s := a.GetAppState().Connection.Status; s != ConnDisconnected {
		t.Fatalf("intentional stop flipped connection status to %q", s)
	}

	// Restart: the previous run's log is kept as core.log.1.
	if _, err := a.ensureCoreForProfileEx(p, 0, false); err != nil {
		t.Fatalf("restart: %v", err)
	}
	if _, err := os.Stat(filepath.Join(h.dataDir(p), "core.log.1")); err != nil {
		t.Fatalf("previous core.log not rotated: %v", err)
	}
}

func TestCoreLifecycleConnectJobSuccessDisconnectReconnect(t *testing.T) {
	h, p := newLifecycleHarness(t, fakeCoreControl{})
	a := h.app

	st, err := a.Connect()
	if err != nil {
		t.Fatalf("Connect: %v", err)
	}
	if st.Connection.Status != ConnConnecting && st.Connection.Status != ConnConnected {
		t.Fatalf("Connect returned status %q, want connecting", st.Connection.Status)
	}
	h.waitConnectSettled()

	st = a.GetAppState()
	if st.Connection.Health != "ready" || st.Connection.LastError != "" || st.Connection.LastWarning != "" {
		t.Fatalf("connection after warmup = %+v, want ready with no warning", st.Connection)
	}
	if len(st.Proxy.Groups) == 0 {
		t.Fatal("warmup did not pull proxy groups")
	}
	if st.Proxy.ActiveGroup != "GLOBAL" {
		t.Fatalf("global mode: ActiveGroup = %q, want GLOBAL", st.Proxy.ActiveGroup)
	}
	if h.countEvents(p, "mode global") == 0 || h.countEvents(p, "select GLOBAL=") == 0 {
		t.Fatalf("warmup did not apply mode / GLOBAL outbound, events=%q", h.events(p))
	}
	if !h.hasDiag("pipeline.connect.apply_runtime", "skip") {
		t.Fatal("cold start must skip the redundant hot reload")
	}
	if n := h.countEvents(p, "reload "); n != 0 {
		t.Fatalf("cold-start Connect hot-reloaded %d times, want 0", n)
	}

	// Connect while connected is a no-op.
	if _, err := a.Connect(); err != nil {
		t.Fatalf("second Connect: %v", err)
	}
	if n := h.countEvents(p, "start "); n != 1 {
		t.Fatalf("idempotent Connect spawned a core: %d starts", n)
	}

	// Disconnect keeps the core (reload model) and clears session state.
	st = a.Disconnect()
	if st.Connection.Status != ConnDisconnected || len(st.Proxy.Groups) != 0 {
		t.Fatalf("after Disconnect: status=%q groups=%d", st.Connection.Status, len(st.Proxy.Groups))
	}
	if !a.GetAppState().Core.Running {
		t.Fatal("Disconnect must keep the core running for an instant reconnect")
	}

	// Reconnect: warm path — same process, one hot reload of the fresh YAML.
	if _, err := a.Connect(); err != nil {
		t.Fatalf("reconnect: %v", err)
	}
	h.waitFor("hot-path apply ok", 20*time.Second, func() bool {
		return h.hasDiag("pipeline.connect.apply_runtime", "ok")
	})
	h.waitConnectSettled()
	if n := h.countEvents(p, "start "); n != 1 {
		t.Fatalf("warm reconnect restarted the core: %d starts", n)
	}
	if n := h.countEvents(p, "reload ok"); n != 1 {
		t.Fatalf("warm reconnect: %d successful reloads, want 1 (events=%q)", n, h.events(p))
	}
}

func TestCoreLifecycleConnectFailsOnBadConfig(t *testing.T) {
	h, p := newLifecycleHarness(t, fakeCoreControl{PreflightError: "proxy 0: missing server"})
	a := h.app

	if _, err := a.Connect(); err != nil {
		t.Fatalf("Connect: %v", err)
	}
	h.waitFor("connect error", 20*time.Second, func() bool {
		return a.GetAppState().Connection.Status == ConnError
	})
	st := a.GetAppState()
	if !strings.Contains(st.Connection.LastError, "configuration preflight failed") ||
		!strings.Contains(st.Connection.LastError, "missing server") {
		t.Fatalf("LastError = %q, want the core's parse error surfaced", st.Connection.LastError)
	}
	if st.Core.Running {
		t.Fatal("core must not be running after a failed preflight")
	}
	if n := h.countEvents(p, "start "); n != 0 {
		t.Fatalf("core was launched despite a failed preflight (%d starts)", n)
	}
}

func TestCoreLifecycleConnectFailsWhenCoreNeverReady(t *testing.T) {
	prev := coreReadyTimeout
	coreReadyTimeout = 1500 * time.Millisecond
	t.Cleanup(func() { coreReadyTimeout = prev })

	h, p := newLifecycleHarness(t, fakeCoreControl{NeverReady: true})
	a := h.app

	if _, err := a.Connect(); err != nil {
		t.Fatalf("Connect: %v", err)
	}
	h.waitFor("connect error", 20*time.Second, func() bool {
		return a.GetAppState().Connection.Status == ConnError
	})
	st := a.GetAppState()
	if !strings.Contains(st.Connection.LastError, "did not become ready in time") {
		t.Fatalf("LastError = %q, want the readiness timeout", st.Connection.LastError)
	}
	if st.Core.Running || st.Core.Lifecycle != "stopped" {
		t.Fatalf("core state after readiness timeout = %+v, want stopped", st.Core)
	}
	if h.countEvents(p, "start ") != 1 {
		t.Fatalf("want exactly one launch, events=%q", h.events(p))
	}
}

func TestCoreLifecycleDisconnectCancelsInFlightConnect(t *testing.T) {
	// Default 45 s readiness deadline: if supersede did not work, the job
	// would still be polling long after waitFor gives up.
	h, p := newLifecycleHarness(t, fakeCoreControl{NeverReady: true})
	a := h.app

	if _, err := a.Connect(); err != nil {
		t.Fatalf("Connect: %v", err)
	}
	// Both conditions: the fake is serving AND the app has published its
	// endpoint (i.e. the job is inside the readiness poll, not before it).
	h.waitFor("fake core launched", 20*time.Second, func() bool {
		listen, _ := h.endpoint()
		return listen != "" && h.countEvents(p, "start ") == 1
	})
	listen, secret := h.endpoint()

	st := a.Disconnect()
	if st.Connection.Status != ConnDisconnected {
		t.Fatalf("Disconnect status = %q", st.Connection.Status)
	}
	h.waitFor("connect job aborted", 10*time.Second, func() bool {
		return h.hasDiag("pipeline.connect.ensure_core", "fail")
	})
	h.assertEndpointGone(listen, secret)

	st = a.GetAppState()
	if st.Connection.Status != ConnDisconnected || st.Connection.LastError != "" {
		t.Fatalf("aborted job leaked into state: %+v", st.Connection)
	}
	if st.Core.Running || st.Core.Lifecycle != "stopped" {
		t.Fatalf("superseded core not stopped: %+v", st.Core)
	}
	for _, ev := range a.GetRuntimeDiagEvents() {
		if ev.Category == "pipeline.connect.ensure_core" && strings.HasPrefix(ev.Message, "fail") &&
			!strings.Contains(ev.Message, errConnectAborted.Error()) {
			t.Fatalf("job failed for another reason than supersede: %q", ev.Message)
		}
	}
}

func withFastAutoRestart(t *testing.T) {
	prev := coreAutoRestartBackoffs
	coreAutoRestartBackoffs = []time.Duration{20 * time.Millisecond, 20 * time.Millisecond, 20 * time.Millisecond}
	t.Cleanup(func() { coreAutoRestartBackoffs = prev })
}

func TestCoreLifecycleAutoRestartAfterCrash(t *testing.T) {
	withFastAutoRestart(t)
	h, p := newLifecycleHarness(t, fakeCoreControl{})
	a := h.app

	if _, err := a.Connect(); err != nil {
		t.Fatalf("Connect: %v", err)
	}
	h.waitConnectSettled()
	oldListen, oldSecret := h.endpoint()

	h.crashCore()
	h.waitFor("auto-restart success", 20*time.Second, func() bool {
		return h.hasDiag("core.autorestart.success", "ok")
	})
	h.assertEndpointGone(oldListen, oldSecret)

	st := a.GetAppState()
	if st.Connection.Status != ConnConnected || st.Connection.Health != "ready" || st.Connection.LastError != "" {
		t.Fatalf("connection after auto-restart = %+v, want connected/ready", st.Connection)
	}
	if !st.Core.Running || st.Core.Version != "fake-1.0.0" {
		t.Fatalf("core after auto-restart = %+v", st.Core)
	}
	if !h.hasDiag("core.exit.unexpected", "fail") {
		t.Fatal("unexpected exit not traced")
	}
	if n := h.countEvents(p, "start "); n != 2 {
		t.Fatalf("want 2 launches (original + restart), got %d: %q", n, h.events(p))
	}
	if n := h.countEvents(p, "reload ok"); n != 1 {
		t.Fatalf("auto-restart must re-apply the runtime config once, got %d reloads", n)
	}
}

func TestCoreLifecycleAutoRestartGivesUp(t *testing.T) {
	withFastAutoRestart(t)
	h, p := newLifecycleHarness(t, fakeCoreControl{})
	a := h.app

	if _, err := a.Connect(); err != nil {
		t.Fatalf("Connect: %v", err)
	}
	h.waitConnectSettled()

	// From now on every restart fails at preflight.
	h.setControl(p, fakeCoreControl{PreflightError: "broken after crash"})
	h.crashCore()
	h.waitFor("auto-restart exhausted", 20*time.Second, func() bool {
		return h.hasDiag("core.autorestart.exhausted", "fail")
	})
	st := a.GetAppState()
	if st.Connection.Status != ConnError || !strings.Contains(st.Connection.LastError, "auto-restarts failed") {
		t.Fatalf("connection after exhausted restarts = %+v", st.Connection)
	}
	if st.Core.Running {
		t.Fatal("core reported running after every restart failed")
	}
	if n := h.countEvents(p, "preflight fail"); n != len(coreAutoRestartBackoffs) {
		t.Fatalf("want %d failed restart attempts, got %d", len(coreAutoRestartBackoffs), n)
	}
}

func TestCoreLifecycleAutoRestartStandsDownAfterDisconnect(t *testing.T) {
	withFastAutoRestart(t)
	h, p := newLifecycleHarness(t, fakeCoreControl{})
	a := h.app
	if _, err := a.ensureCoreForProfileEx(p, 0, false); err != nil {
		t.Fatalf("start: %v", err)
	}
	// Disconnected user + intentional stop: attemptCoreAutoRestart must bail
	// on its first re-validation without touching state or spawning a core.
	a.mu.Lock()
	a.stopCoreLocked()
	a.mu.Unlock()
	a.attemptCoreAutoRestart(p.ID, "proxy")
	if n := h.countEvents(p, "start "); n != 1 {
		t.Fatalf("auto-restart ran after an intentional stop: %d starts", n)
	}
	if s := a.GetAppState().Connection.Status; s != ConnDisconnected {
		t.Fatalf("status = %q, want untouched disconnected", s)
	}
}

func TestApplyRuntimeConfigWithGen(t *testing.T) {
	t.Run("no running core writes config only", func(t *testing.T) {
		h, p := newLifecycleHarness(t, fakeCoreControl{})
		if err := h.app.applyRuntimeConfig(p, "proxy", false); err != nil {
			t.Fatalf("apply: %v", err)
		}
		if _, err := os.Stat(filepath.Join(h.dataDir(p), "config.yaml")); err != nil {
			t.Fatalf("config.yaml not written: %v", err)
		}
		if !h.hasDiag("pipeline.apply.done", "skip") {
			t.Fatal("expected the no_running_core skip trace")
		}
		if h.countEvents(p, "start ") != 0 {
			t.Fatal("apply without a core must not start one")
		}
	})

	t.Run("superseded gen aborts before any work", func(t *testing.T) {
		h, p := newLifecycleHarness(t, fakeCoreControl{})
		stale := h.app.connectGen.Add(1)
		h.app.connectGen.Add(1)
		if err := h.app.applyRuntimeConfigWithGen(p, "proxy", false, stale); !errors.Is(err, errConnectAborted) {
			t.Fatalf("err = %v, want errConnectAborted", err)
		}
		if _, err := os.Stat(filepath.Join(h.dataDir(p), "config.yaml")); err == nil {
			t.Fatal("aborted apply still wrote config.yaml")
		}
	})

	t.Run("profile without source is rejected", func(t *testing.T) {
		h, _ := newLifecycleHarness(t, fakeCoreControl{})
		if err := h.app.applyRuntimeConfig(Profile{ID: "lc-nosource"}, "proxy", false); err == nil {
			t.Fatal("want an error for a profile with no URL and no local config")
		}
	})

	t.Run("running core hot reloads the written file", func(t *testing.T) {
		h, p := newLifecycleHarness(t, fakeCoreControl{})
		if _, err := h.app.ensureCoreForProfileEx(p, 0, false); err != nil {
			t.Fatalf("start: %v", err)
		}
		if err := h.app.applyRuntimeConfig(p, "proxy", false); err != nil {
			t.Fatalf("apply: %v", err)
		}
		want, _ := filepath.Abs(filepath.Join(h.dataDir(p), "config.yaml"))
		if got := h.countEvents(p, "reload ok path="+want); got != 1 {
			t.Fatalf("want one reload of %s, events=%q", want, h.events(p))
		}
		if !h.hasDiag("pipeline.apply.reload", "ok") {
			t.Fatal("reload ok not traced")
		}
	})

	t.Run("failed reload with healthy core is accepted", func(t *testing.T) {
		h, p := newLifecycleHarness(t, fakeCoreControl{FailReload: true})
		if _, err := h.app.ensureCoreForProfileEx(p, 0, false); err != nil {
			t.Fatalf("start: %v", err)
		}
		if err := h.app.applyRuntimeConfig(p, "proxy", false); err != nil {
			t.Fatalf("apply: %v", err)
		}
		if !h.hasDiag("pipeline.apply.reload", "soft_ok") {
			t.Fatal("want the soft_ok path (reload failed, /version healthy)")
		}
		if n := h.countEvents(p, "start "); n != 1 {
			t.Fatalf("soft failure must not restart the core: %d starts", n)
		}
	})

	t.Run("unresponsive core is restarted and reload retried", func(t *testing.T) {
		h, p := newLifecycleHarness(t, fakeCoreControl{BreakOnFirstReload: true})
		if _, err := h.app.ensureCoreForProfileEx(p, 0, false); err != nil {
			t.Fatalf("start: %v", err)
		}
		if err := h.app.applyRuntimeConfig(p, "proxy", false); err != nil {
			t.Fatalf("apply: %v", err)
		}
		if !h.hasDiag("pipeline.apply.restart", "ok") || !h.hasDiag("pipeline.apply.retry_reload", "ok") {
			t.Fatalf("want restart + retried reload, diag=%+v", h.app.GetRuntimeDiagEvents())
		}
		if n := h.countEvents(p, "start "); n != 2 {
			t.Fatalf("want 2 launches, got %d: %q", n, h.events(p))
		}
		if !h.app.GetAppState().Core.Running {
			t.Fatal("core not running after fallback restart")
		}
	})

	t.Run("other profile switches the core", func(t *testing.T) {
		h, p1 := newLifecycleHarness(t, fakeCoreControl{})
		p2 := h.addProfile(fakeCoreControl{})
		if _, err := h.app.ensureCoreForProfileEx(p1, 0, false); err != nil {
			t.Fatalf("start p1: %v", err)
		}
		oldListen, oldSecret := h.endpoint()
		if err := h.app.applyRuntimeConfig(p2, "proxy", false); err != nil {
			t.Fatalf("apply p2: %v", err)
		}
		h.assertEndpointGone(oldListen, oldSecret)
		h.app.mu.RLock()
		active := h.app.core.activeProfileID
		h.app.mu.RUnlock()
		if active != p2.ID {
			t.Fatalf("core bound to %q, want %q", active, p2.ID)
		}
		if h.countEvents(p2, "start ") != 1 || h.countEvents(p2, "reload ") != 0 {
			t.Fatalf("profile switch must cold-start p2 without a reload, events=%q", h.events(p2))
		}
	})
}
