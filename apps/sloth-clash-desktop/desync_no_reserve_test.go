package main

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"SlothClashDesktop/internal/desync"
)

// noReserveCore is a core API stub for the no-reserve controller: every
// delay probe answers with delayStatus, selector writes are recorded, and a
// reserve probe (profile-probe) is a test failure: a Desync-only profile has
// no subscription to verify.
type noReserveCore struct {
	t           *testing.T
	delayStatus atomic.Int32
	now         string
	mu          sync.Mutex
	puts        []string
	reserve     atomic.Int32
	srv         *httptest.Server
	// onDelay, when set before the controller starts, runs on every delay
	// probe.
	onDelay func()
}

func newNoReserveCore(t *testing.T, delayStatus int, now string) *noReserveCore {
	c := &noReserveCore{t: t, now: now}
	c.delayStatus.Store(int32(delayStatus))
	c.srv = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case strings.HasSuffix(r.URL.Path, "/profile-probe"):
			c.reserve.Add(1)
			t.Errorf("reserve probe %s issued for a Desync-only profile", r.URL.Path)
			fmt.Fprint(w, `{"ok":true,"route":"remote"}`)
		case r.Method == http.MethodPut:
			var b struct {
				Name string `json:"name"`
			}
			_ = json.NewDecoder(r.Body).Decode(&b)
			c.mu.Lock()
			c.puts = append(c.puts, b.Name)
			c.now = b.Name
			c.mu.Unlock()
			w.WriteHeader(http.StatusNoContent)
		case strings.HasSuffix(r.URL.Path, "/delay"):
			if c.onDelay != nil {
				c.onDelay()
			}
			if code := int(c.delayStatus.Load()); code != http.StatusOK {
				w.WriteHeader(code)
				fmt.Fprint(w, `{"message":"TLS timeout"}`)
				return
			}
			fmt.Fprint(w, `{"delay":12}`)
		default:
			c.mu.Lock()
			now := c.now
			c.mu.Unlock()
			fmt.Fprintf(w, `{"type":"Selector","all":["candidate","PASS"],"now":%q}`, now)
		}
	}))
	t.Cleanup(c.srv.Close)
	return c
}

func (c *noReserveCore) selections() []string {
	c.mu.Lock()
	defer c.mu.Unlock()
	return append([]string(nil), c.puts...)
}

func noReserveApp(t *testing.T, c *noReserveCore, s DesyncSettings) *App {
	t.Helper()
	prefsMu.Lock()
	previous := prefsCurrent
	prefsCurrent.Desync = s
	prefsCurrent.DesyncCache = nil
	prefsMu.Unlock()
	t.Cleanup(func() { prefsMu.Lock(); prefsCurrent = previous; prefsMu.Unlock() })
	a := &App{core: coreProcess{listen: strings.TrimPrefix(c.srv.URL, "http://")}}
	a.profiles = []Profile{{ID: "profile-1", Type: "local", Kind: profileKindDesyncOnly}}
	a.state.Profile.ActiveProfileID = "profile-1"
	a.state.Connection.Status = "connected"
	a.state.Core.Running = true
	a.state.Mode.Current = "rule"
	a.desyncCtl.epoch = 1
	a.desyncCtl.status.Network = desyncNetworkKey()
	return a
}

func waitPhase(t *testing.T, what string, timeout time.Duration, cond func() bool) {
	t.Helper()
	deadline := time.Now().Add(timeout)
	for !cond() {
		if time.Now().After(deadline) {
			t.Fatalf("timed out waiting for %s", what)
		}
		time.Sleep(5 * time.Millisecond)
	}
}

func TestDesyncStatusReportsDesyncOnlyProfile(t *testing.T) {
	a := &App{profiles: []Profile{{ID: "profile-1", Type: "subscription", URL: "https://example.com/s"}, {ID: "profile-2", Type: "local", Kind: profileKindDesyncOnly}}}
	a.state.Profile.ActiveProfileID = "profile-1"
	if a.GetDesyncStatus().DesyncOnly {
		t.Fatal("a subscription profile reported as Desync-only")
	}
	a.state.Profile.ActiveProfileID = "profile-2"
	if !a.GetDesyncStatus().DesyncOnly {
		t.Fatal("the Desync-only profile is not reported")
	}
}

// No strategy works: no reserve verification, the route falls through to
// PASS (MATCH,DIRECT in this profile) and the service says no_strategy.
func TestDesyncTCPNoReserveFailureGoesDirect(t *testing.T) {
	core := newNoReserveCore(t, http.StatusServiceUnavailable, desync.YouTubeRoute+"-"+candID("youtube", 0))
	a := noReserveApp(t, core, DesyncSettings{Enabled: true, YouTube: true})
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() {
		defer close(done)
		a.runDesyncTCPService(ctx, 1, a.desyncCtl.status.Network, a.core.listen, "", "youtube", desync.YouTubeRoute, true)
	}()
	waitPhase(t, "no_strategy", 15*time.Second, func() bool { return a.GetDesyncAutoStatus().YouTube.Phase == "no_strategy" })
	cancel()
	<-done
	state := a.GetDesyncAutoStatus().YouTube
	if state.Route != "direct" || state.Code == "" {
		t.Fatalf("state = %+v, want route direct with a reason", state)
	}
	if got := core.selections(); len(got) != 1 || got[0] != desyncProfileSelection {
		t.Fatalf("selections = %v, want exactly [PASS]", got)
	}
	if core.reserve.Load() != 0 {
		t.Fatal("reserve probes issued")
	}
	if report := a.GetDesyncDiagnosticReport(); !strings.Contains(report, `"code": "no_strategy"`) || !strings.Contains(report, `"desyncOnly": true`) {
		t.Fatalf("diagnostics miss the no-reserve outcome: %s", report)
	}
}

// A PASS left selected from an earlier session is not a reserve here: the
// controller searches and applies a working strategy at once.
func TestDesyncTCPNoReserveLeavesStalePass(t *testing.T) {
	core := newNoReserveCore(t, http.StatusOK, desyncProfileSelection)
	a := noReserveApp(t, core, DesyncSettings{Enabled: true, YouTube: true})
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() {
		defer close(done)
		a.runDesyncTCPService(ctx, 1, a.desyncCtl.status.Network, a.core.listen, "", "youtube", desync.YouTubeRoute, true)
	}()
	waitPhase(t, "ready", 15*time.Second, func() bool { return a.GetDesyncAutoStatus().YouTube.Phase == "ready" })
	cancel()
	<-done
	if got := core.selections(); len(got) != 1 || got[0] != desync.YouTubeRoute+"-"+candID("youtube", 0) {
		t.Fatalf("selections = %v, want the first strategy", got)
	}
	if core.reserve.Load() != 0 {
		t.Fatal("reserve probes issued")
	}
}

// Voice: a PASS selection is replaced by the first candidate instead of
// being watched as a subscription reserve.
func TestDesyncVoiceNoReserveLeavesPass(t *testing.T) {
	core := newNoReserveCore(t, http.StatusOK, desyncProfileSelection)
	a := noReserveApp(t, core, DesyncSettings{Enabled: true, DiscordUDP: true})
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() {
		defer close(done)
		a.runDesyncVoiceService(ctx, 1, a.desyncCtl.status.Network, a.core.listen, "", true)
	}()
	first := desync.Ordered("voice", desyncCaps, "")[0].Name(desync.DiscordUDPRoute)
	waitPhase(t, "a voice candidate", 10*time.Second, func() bool {
		got := core.selections()
		return len(got) > 0
	})
	cancel()
	<-done
	if got := core.selections(); got[0] != first {
		t.Fatalf("selections = %v, want %s first", got, first)
	}
	if phase := a.GetDesyncAutoStatus().Voice.Phase; phase == "profile" || phase == "profile_failed" {
		t.Fatalf("voice phase %q is a reserve state", phase)
	}
}

func TestDesyncNoStrategyBackoffDoubles(t *testing.T) {
	want := []time.Duration{time.Minute, 2 * time.Minute, 4 * time.Minute, 8 * time.Minute, 16 * time.Minute, 30 * time.Minute, 30 * time.Minute}
	for i, w := range want {
		if got := desyncNoStrategyBackoff(i + 1); got != w {
			t.Fatalf("retry %d waits %v, want %v", i+1, got, w)
		}
	}
	if got := desyncNoStrategyBackoff(1000); got != desyncNoStrategyBackoffMax {
		t.Fatalf("a late retry waits %v, want the cap", got)
	}
}

// After no_strategy the search repeats in the background: the visible state
// stays no_strategy (no testing/failed flashes) until a strategy works,
// which is then applied and reported ready.
func TestDesyncTCPNoReserveRetriesQuietly(t *testing.T) {
	retry := desyncNoStrategyRetry
	desyncNoStrategyRetry = 20 * time.Millisecond
	t.Cleanup(func() { desyncNoStrategyRetry = retry })
	core := newNoReserveCore(t, http.StatusServiceUnavailable, desync.YouTubeRoute+"-"+candID("youtube", 0))
	a := noReserveApp(t, core, DesyncSettings{Enabled: true, YouTube: true})
	var retried atomic.Int32
	var flashed atomic.Value
	var settled atomic.Bool
	core.onDelay = func() {
		if !settled.Load() {
			return // the first, visible search
		}
		if phase := a.GetDesyncAutoStatus().YouTube.Phase; phase != "no_strategy" {
			flashed.Store(phase)
		}
		retried.Add(1)
	}
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() {
		defer close(done)
		a.runDesyncTCPService(ctx, 1, a.desyncCtl.status.Network, a.core.listen, "", "youtube", desync.YouTubeRoute, true)
	}()
	defer func() { cancel(); <-done }()
	waitPhase(t, "no_strategy", 15*time.Second, func() bool { return a.GetDesyncAutoStatus().YouTube.Phase == "no_strategy" })
	settled.Store(true)
	// Several failed background rounds (a round probes every candidate).
	rounds := int32(2 * len(desync.Ordered("youtube", desyncCaps, "")))
	waitPhase(t, "background retries", 15*time.Second, func() bool { return retried.Load() > rounds })
	if phase := flashed.Load(); phase != nil {
		t.Fatalf("a background retry showed phase %q", phase)
	}
	core.delayStatus.Store(http.StatusOK)
	waitPhase(t, "ready", 15*time.Second, func() bool { return a.GetDesyncAutoStatus().YouTube.Phase == "ready" })
	if phase := flashed.Load(); phase != nil {
		t.Fatalf("a background retry showed phase %q", phase)
	}
	got := core.selections()
	if len(got) < 2 || got[0] != desyncProfileSelection || got[len(got)-1] == desyncProfileSelection {
		t.Fatalf("selections = %v, want PASS then a strategy", got)
	}
}

// Voice, no reserve: a stalled call whose strategy search finds nothing keeps
// the current candidate (plain UDP is no better), never tries a reserve and
// reports no_strategy until application traffic recovers.
func TestDesyncVoiceNoReserveNoStrategyKeepsCandidate(t *testing.T) {
	tick := desyncVoiceTick
	desyncVoiceTick = 100 * time.Millisecond
	t.Cleanup(func() { desyncVoiceTick = tick })
	prefsMu.Lock()
	previous := prefsCurrent
	prefsCurrent.Desync = DesyncSettings{Enabled: true, DiscordUDP: true}
	prefsCurrent.DesyncCache = nil
	prefsMu.Unlock()
	t.Cleanup(func() { prefsMu.Lock(); prefsCurrent = previous; prefsMu.Unlock() })

	selected := desync.DiscordUDPRoute + "-" + candID("voice", 0)
	started := time.Now()
	var healthy atomic.Bool
	var puts, probes, reserve atomic.Int32
	var reads atomic.Uint64
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodPut:
			puts.Add(1)
			w.WriteHeader(http.StatusNoContent)
		case strings.HasSuffix(r.URL.Path, "/profile-probe"):
			reserve.Add(1)
			w.WriteHeader(http.StatusServiceUnavailable)
		case strings.HasSuffix(r.URL.Path, "/udp-probe"):
			probes.Add(1)
			fmt.Fprint(w, `{"sent":12,"received":0,"maxConsecutiveLoss":12,"durationMS":2200}`)
		case r.URL.Path == "/connections":
			_ = json.NewEncoder(w).Encode(map[string]any{"connections": []ConnectionItem{monitorConnection("104.29.1.1")}})
		case r.URL.Path == "/proxies/"+desync.DiscordUDPRoute:
			fmt.Fprintf(w, `{"type":"Selector","now":%q}`, selected)
		default:
			// The candidate's activity: sending, nothing received for 30 s
			// (stalled), or receiving again once healthy.
			n := reads.Add(1)
			now := time.Now().UnixMilli()
			p := desyncUDPActivity{Endpoint: "104.29.1.1:19330", FirstMS: started.Add(-time.Minute).UnixMilli(), LastTXMS: now, LastRXMS: started.Add(-30 * time.Second).UnixMilli(), TXPackets: 100 + n, RXPackets: 10, TXBytes: 6000, RXBytes: 600, DiscoveryRX: 3}
			if healthy.Load() {
				p.LastRXMS, p.RXPackets = now, 10+n
			}
			_ = json.NewEncoder(w).Encode(map[string]any{"desyncUDP": map[string]any{"activity": []desyncUDPActivity{p}}})
		}
	}))
	t.Cleanup(server.Close)
	a := &App{core: coreProcess{listen: strings.TrimPrefix(server.URL, "http://")}}
	a.desyncCtl.epoch = 1
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() {
		defer close(done)
		a.runDesyncVoiceService(ctx, 1, desyncNetworkKey(), a.core.listen, "", true)
	}()
	defer func() { cancel(); <-done }()

	// The stall needs 8 s of separated samples before the search.
	waitPhase(t, "voice no_strategy", 20*time.Second, func() bool { return a.GetDesyncAutoStatus().Voice.Phase == "no_strategy" })
	state := a.GetDesyncAutoStatus().Voice
	if state.Candidate != candID("voice", 0) || state.Route != "desync" || state.Code == "" {
		t.Fatalf("voice state = %+v, want the kept candidate f4 on the Desync route with a reason", state)
	}
	if probes.Load() == 0 {
		t.Fatal("no strategy search ran")
	}
	// The line keeps saying so while the call stays stalled.
	seen := reads.Load()
	waitPhase(t, "more observations", 5*time.Second, func() bool { return reads.Load() >= seen+5 })
	if phase := a.GetDesyncAutoStatus().Voice.Phase; phase != "no_strategy" {
		t.Fatalf("voice phase %q while still stalled, want no_strategy", phase)
	}
	// Traffic recovers: the status follows it again.
	healthy.Store(true)
	waitPhase(t, "voice recovery", 5*time.Second, func() bool { return a.GetDesyncAutoStatus().Voice.Phase == "transport" })
	if puts.Load() != 0 || reserve.Load() != 0 {
		t.Fatalf("selector writes = %d, reserve probes = %d; want the candidate kept and no reserve", puts.Load(), reserve.Load())
	}
}
