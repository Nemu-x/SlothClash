package main

// startEmbeddedCore edge paths on top of the core lifecycle harness
// (core_lifecycle_harness_test.go): early rejections, filesystem failures
// before and after preflight, an app shutdown during the readiness poll, and
// the two config sources the harness's seeded local profile does not take (a
// subscription URL fetched on first start, a hand-edited config.yaml).
//
// Still not covered, on purpose:
//   - the privileged-service branch (useServiceCore): windowsEnsureSlothIPCReachable
//     and ipcSlothStartClash talk to the real installed service and have no
//     seam; with Service.Installed=true a test would start a real core
//     through the developer's service;
//   - the darwin/linux "service IPC unreachable" fallback (same reason, and
//     not compiled into the condition on Windows);
//   - slothDataRoot / resolveMixedPort / pickFreePort / cmd.Start failures,
//     which need a broken OS rather than a broken input;
//   - ensureGeoInDataDir failing: the harness app has no embedded geo files,
//     so the copy is a no-op;
//   - rule-mode warmup (runConnectJob → RefreshHomeInsight) asks
//     api.ipify.org directly, which cannot be served locally.

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

// addProfileAs registers p (whose ID it fills in) without seeding a body
// cache, for profiles that bring their own config source.
func (h *lifecycleHarness) addProfileAs(p Profile, ctl fakeCoreControl) Profile {
	h.t.Helper()
	b := make([]byte, 6)
	_, _ = rand.Read(b)
	p.ID = "lc-" + hex.EncodeToString(b)
	h.setControl(p, ctl)
	h.app.mu.Lock()
	h.app.profiles = append(h.app.profiles, p)
	h.app.mu.Unlock()
	return p
}

func TestCoreLifecycleStartRejectsProfileWithoutSource(t *testing.T) {
	h, _ := newLifecycleHarness(t, fakeCoreControl{})
	a := h.app
	before := a.GetAppState().Core

	err := a.startEmbeddedCore(Profile{ID: "lc-nosource"}, a.connectGen.Add(1), false)
	if err == nil || err.Error() != "active profile has no subscription URL" {
		t.Fatalf("err = %v, want the no-subscription-URL rejection", err)
	}
	// Rejected before any state is touched.
	if got := a.GetAppState().Core; got != before {
		t.Fatalf("core state changed by a rejected start: %+v -> %+v", before, got)
	}
}

func TestCoreLifecycleStartUnknownTrafficFallsBackToProxy(t *testing.T) {
	h, p := newLifecycleHarness(t, fakeCoreControl{})
	a := h.app
	a.mu.Lock()
	a.state.Traffic = "bogus"
	a.mu.Unlock()
	// The profile script sees the traffic mode the config is generated for;
	// it is the one place that value is observable in the written YAML.
	p.ScriptOverride = `function main(config, ctx) { config['x-test-traffic'] = ctx.traffic; return config }`

	if _, err := a.ensureCoreForProfileEx(p, 0, false); err != nil {
		t.Fatalf("start: %v", err)
	}
	if !a.GetAppState().Core.Running {
		t.Fatal("core not running")
	}
	b, err := os.ReadFile(filepath.Join(h.dataDir(p), "config.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(b), "x-test-traffic: proxy") {
		t.Fatalf("config not generated for proxy traffic:\n%s", b)
	}
}

func TestCoreLifecycleStartFailsWhenDataDirUnusable(t *testing.T) {
	h, p := newLifecycleHarness(t, fakeCoreControl{})
	a := h.app
	// A regular file where the providers/ directory must go.
	if err := os.WriteFile(filepath.Join(h.dataDir(p), "providers"), []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}

	if _, err := a.ensureCoreForProfileEx(p, 0, false); err == nil {
		t.Fatal("start succeeded although providers/ could not be created")
	}
	if n := h.countEvents(p, "preflight "); n != 0 {
		t.Fatalf("preflight ran despite the data dir failure (%d runs)", n)
	}
	st := a.GetAppState().Core
	// The early return leaves the "starting" marker set by the pre-start
	// reset; pinned here so the phase split cannot change it silently.
	if st.Running || st.Lifecycle != "starting" {
		t.Fatalf("core state after early failure = %+v", st)
	}
}

func TestCoreLifecycleStartFailsWhenCoreLogUnwritable(t *testing.T) {
	h, p := newLifecycleHarness(t, fakeCoreControl{})
	a := h.app
	// core.log is a directory, and so is a non-empty core.log.1, so the
	// rotation cannot move it away on any OS: opening the log must fail.
	dir := h.dataDir(p)
	if err := os.MkdirAll(filepath.Join(dir, "core.log"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(dir, "core.log.1"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "core.log.1", "keep"), []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}

	if _, err := a.ensureCoreForProfileEx(p, 0, false); err == nil {
		t.Fatal("start succeeded although core.log could not be opened")
	}
	if h.countEvents(p, "preflight ok") != 1 {
		t.Fatalf("want the config to pass preflight first, events=%q", h.events(p))
	}
	if n := h.countEvents(p, "start "); n != 0 {
		t.Fatalf("core launched without a log file (%d starts)", n)
	}
	st := a.GetAppState().Core
	if st.Running || st.ControllerAddr != "" {
		t.Fatalf("core state published for a core that never started: %+v", st)
	}
}

func TestCoreLifecycleAppShutdownAbortsReadinessPoll(t *testing.T) {
	h, p := newLifecycleHarness(t, fakeCoreControl{NeverReady: true})
	a := h.app
	// a.ctx is the Wails app context; cancelling it is what shutdown does to
	// an in-flight start. Nothing on this path emits Wails events (status
	// stays disconnected), so a plain context is safe here.
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	a.ctx = ctx

	errCh := make(chan error, 1)
	go func() { errCh <- a.forceRestartCoreForProfile(p, 0, false) }()
	h.waitFor("fake core launched", 20*time.Second, func() bool {
		listen, _ := h.endpoint()
		return listen != "" && h.countEvents(p, "start ") == 1
	})
	listen, secret := h.endpoint()

	cancel()
	select {
	case err := <-errCh:
		if !errors.Is(err, errConnectAborted) {
			t.Fatalf("err = %v, want errConnectAborted", err)
		}
	case <-time.After(10 * time.Second):
		t.Fatal("readiness poll ignored the cancelled app context")
	}
	h.assertEndpointGone(listen, secret)
	if st := a.GetAppState().Core; st.Running || st.Lifecycle != "stopped" {
		t.Fatalf("core state after abort = %+v, want stopped", st)
	}
}

func TestCoreLifecycleStartFetchesSubscriptionOnFirstRun(t *testing.T) {
	var hits atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/sub" {
			http.NotFound(w, r)
			return
		}
		hits.Add(1)
		w.Header().Set("Content-Type", "text/yaml")
		_, _ = w.Write([]byte(lifecycleTestBody))
	}))
	defer srv.Close()

	h, _ := newLifecycleHarness(t, fakeCoreControl{})
	a := h.app
	p := h.addProfileAs(Profile{Name: "remote", URL: srv.URL + "/sub"}, fakeCoreControl{})

	if _, err := a.ensureCoreForProfileEx(p, 0, false); err != nil {
		t.Fatalf("start: %v", err)
	}
	if n := hits.Load(); n != 1 {
		t.Fatalf("subscription fetched %d times on a cold start, want 1", n)
	}
	st := a.GetAppState().Core
	if !st.Running || st.Version != "fake-1.0.0" {
		t.Fatalf("core state = %+v, want running fake-1.0.0", st)
	}
	b, err := os.ReadFile(filepath.Join(h.dataDir(p), "config.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(b), "node-a") {
		t.Fatalf("config.yaml does not carry the fetched proxies:\n%s", b)
	}
}

func TestCoreLifecycleStartKeepsHandEditedConfig(t *testing.T) {
	h, _ := newLifecycleHarness(t, fakeCoreControl{})
	a := h.app
	p := h.addProfileAs(Profile{Name: "hand", SkipAutoConfig: true}, fakeCoreControl{})
	hand := strings.ReplaceAll(lifecycleTestBody, "node-a", "hand-node")
	if err := os.WriteFile(filepath.Join(h.dataDir(p), "config.yaml"), []byte(hand), 0o644); err != nil {
		t.Fatal(err)
	}

	if _, err := a.ensureCoreForProfileEx(p, 0, false); err != nil {
		t.Fatalf("start: %v", err)
	}
	st := a.GetAppState().Core
	if !st.Running {
		t.Fatalf("core not running: %+v", st)
	}
	b, err := os.ReadFile(filepath.Join(h.dataDir(p), "config.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	cfg := string(b)
	if !strings.Contains(cfg, "hand-node") {
		t.Fatalf("hand-edited proxies lost:\n%s", cfg)
	}
	// The runtime overlay still owns the controller endpoint.
	if !strings.Contains(cfg, st.ControllerAddr) {
		t.Fatalf("config.yaml does not point external-controller at %s:\n%s", st.ControllerAddr, cfg)
	}
}
