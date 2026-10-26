package main

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"SlothClashDesktop/internal/desync"
)

// customFakeCore answers the calls custom calibration makes. passes decides a
// delay probe by the probed proxy name ("DIRECT" or a candidate).
type customFakeCore struct {
	t        *testing.T
	mu       sync.Mutex
	passes   func(name, target string) bool
	selected map[string][]string // group -> PUT selections, in order
	probes   []string            // "name target expected"
	inflight atomic.Int32
	maxSeen  atomic.Int32
	srv      *httptest.Server
}

func newCustomFakeCore(t *testing.T, passes func(name, target string) bool) *customFakeCore {
	c := &customFakeCore{t: t, passes: passes, selected: map[string][]string{}}
	c.srv = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		name, _ := url.PathUnescape(strings.TrimPrefix(r.URL.EscapedPath(), "/proxies/"))
		switch {
		case r.Method == http.MethodPut:
			var body struct {
				Name string `json:"name"`
			}
			_ = json.NewDecoder(r.Body).Decode(&body)
			c.mu.Lock()
			c.selected[name] = append(c.selected[name], body.Name)
			c.mu.Unlock()
			w.WriteHeader(http.StatusNoContent)
		case strings.HasSuffix(name, "/delay"):
			n := c.inflight.Add(1)
			defer c.inflight.Add(-1)
			for {
				seen := c.maxSeen.Load()
				if n <= seen || c.maxSeen.CompareAndSwap(seen, n) {
					break
				}
			}
			proxy := strings.TrimSuffix(name, "/delay")
			target, expected := r.URL.Query().Get("url"), r.URL.Query().Get("expected")
			c.mu.Lock()
			c.probes = append(c.probes, proxy+" "+target+" "+expected)
			c.mu.Unlock()
			time.Sleep(2 * time.Millisecond)
			if c.passes(proxy, target) {
				fmt.Fprint(w, `{"delay":9}`)
				return
			}
			w.WriteHeader(http.StatusServiceUnavailable)
			fmt.Fprint(w, `{"message":"Head: EOF"}`)
		default:
			fmt.Fprint(w, `{"type":"Selector","all":["x"],"now":"x"}`)
		}
	}))
	t.Cleanup(c.srv.Close)
	return c
}

func (c *customFakeCore) selections(group string) []string {
	c.mu.Lock()
	defer c.mu.Unlock()
	return append([]string(nil), c.selected[group]...)
}

func (c *customFakeCore) probeLog() []string {
	c.mu.Lock()
	defer c.mu.Unlock()
	return append([]string(nil), c.probes...)
}

func customTestApp(t *testing.T, core *customFakeCore, entries ...DesyncCustomEntry) (*App, string) {
	t.Helper()
	prefsMu.Lock()
	previous := prefsCurrent
	prefsCurrent.Desync = DesyncSettings{Enabled: true, DiscordTTL: 4, Custom: entries}
	prefsCurrent.DesyncCache = nil
	prefsMu.Unlock()
	fake := desyncFakeTTLSupported
	desyncFakeTTLSupported = false
	t.Cleanup(func() {
		prefsMu.Lock()
		prefsCurrent = previous
		prefsMu.Unlock()
		desyncFakeTTLSupported = fake
	})
	a := &App{core: coreProcess{listen: strings.TrimPrefix(core.srv.URL, "http://")}}
	a.state.Connection.Status = "connected"
	a.state.Core.Running = true
	a.state.Mode.Current = "rule"
	network := desyncNetworkKey()
	a.desyncCtl.epoch = 1
	a.desyncCtl.status.Network = network
	return a, network
}

var customEntry = DesyncCustomEntry{ID: "0a1b2c3d", Domain: "example.org", Enabled: true}

func TestDesyncCustomNotBlockedSelectsDirect(t *testing.T) {
	core := newCustomFakeCore(t, func(name, _ string) bool { return name == "DIRECT" })
	a, network := customTestApp(t, core, customEntry)
	plan := &desyncCustomPlan{phase: "queued"}
	a.calibrateDesyncCustom(context.Background(), 1, network, a.core.listen, "", customEntry, plan)
	route := desync.CustomRoute(customEntry.ID)
	if got := core.selections(route); len(got) != 1 || got[0] != "DIRECT" {
		t.Fatalf("selections: %v", got)
	}
	state := a.GetDesyncAutoStatus().Custom[customEntry.ID]
	if state.Phase != "not_needed" || state.Route != "direct" || plan.phase != "not_needed" {
		t.Fatalf("state %+v plan %+v", state, plan)
	}
	if desyncCachedSelection(network, desync.CustomService("example.org")) != desync.DirectID {
		t.Fatal("not-needed outcome not cached")
	}
	// Two rounds through DIRECT, with the reachability filter, at the site.
	if probes := core.probeLog(); len(probes) != 2 || probes[0] != "DIRECT https://example.org/ 100-599" {
		t.Fatalf("probes: %v", probes)
	}
}

func TestDesyncCustomFindsWinnerAfterFailedBaseline(t *testing.T) {
	core := newCustomFakeCore(t, func(name, _ string) bool { return strings.HasSuffix(name, "-r2") })
	a, network := customTestApp(t, core, customEntry)
	plan := &desyncCustomPlan{phase: "queued"}
	a.calibrateDesyncCustom(context.Background(), 1, network, a.core.listen, "", customEntry, plan)
	route := desync.CustomRoute(customEntry.ID)
	if got := core.selections(route); len(got) != 1 || got[0] != route+"-r2" {
		t.Fatalf("selections: %v", got)
	}
	state := a.GetDesyncAutoStatus().Custom[customEntry.ID]
	if state.Phase != "ready" || state.Candidate != "r2" || plan.phase != "ready" {
		t.Fatalf("state %+v", state)
	}
	if desyncCachedSelection(network, desync.CustomService("example.org")) != "r2" {
		t.Fatal("winner not cached")
	}
	probes := core.probeLog()
	if !strings.HasPrefix(probes[0], "DIRECT ") || !strings.HasPrefix(probes[1], route+"-r3 ") {
		t.Fatalf("baseline must come first, then the YouTube set: %v", probes)
	}
	for _, p := range probes {
		if !strings.HasSuffix(p, " https://example.org/ 100-599") {
			t.Fatalf("probe outside reachability mode: %s", p)
		}
	}
	// The cached winner is tried right after the baseline next time.
	core2 := newCustomFakeCore(t, func(name, _ string) bool { return strings.HasSuffix(name, "-r2") })
	a.core.listen = strings.TrimPrefix(core2.srv.URL, "http://")
	a.calibrateDesyncCustom(context.Background(), 1, network, a.core.listen, "", customEntry, &desyncCustomPlan{})
	if probes := core2.probeLog(); len(probes) != 3 || !strings.HasPrefix(probes[1], route+"-r2 ") {
		t.Fatalf("cached winner not first: %v", probes)
	}
}

func TestDesyncCustomNothingWorksFallsBackToProfile(t *testing.T) {
	core := newCustomFakeCore(t, func(string, string) bool { return false })
	a, network := customTestApp(t, core, customEntry)
	plan := &desyncCustomPlan{phase: "queued"}
	a.calibrateDesyncCustom(context.Background(), 1, network, a.core.listen, "", customEntry, plan)
	route := desync.CustomRoute(customEntry.ID)
	if got := core.selections(route); len(got) != 1 || got[0] != "PASS" {
		t.Fatalf("selections: %v", got)
	}
	state := a.GetDesyncAutoStatus().Custom[customEntry.ID]
	if state.Phase != "unavailable" || state.Route != "profile" || plan.failures != 1 {
		t.Fatalf("state %+v plan %+v", state, plan)
	}
	// Every candidate once (one round each when it fails) plus the baseline.
	if got, want := len(core.probeLog()), len(desync.CustomCandidates(false))+1; got != want {
		t.Fatalf("probes %d, want %d", got, want)
	}
	if desyncCachedSelection(network, desync.CustomService("example.org")) != "" {
		t.Fatal("a failure was cached as a winner")
	}
	if wait := time.Until(plan.due); wait < 50*time.Second || wait > 61*time.Second {
		t.Fatalf("first backoff %v", wait)
	}
	a.calibrateDesyncCustom(context.Background(), 1, network, a.core.listen, "", customEntry, plan)
	if wait := time.Until(plan.due); plan.failures != 2 || wait < 110*time.Second {
		t.Fatalf("backoff did not double: %v %+v", wait, plan)
	}
}

func TestDesyncCustomStaleLeaseCannotSelect(t *testing.T) {
	core := newCustomFakeCore(t, func(name, _ string) bool { return name == "DIRECT" })
	a, network := customTestApp(t, core, customEntry)
	a.desyncCtl.epoch = 2 // a newer session took over
	a.calibrateDesyncCustom(context.Background(), 1, network, a.core.listen, "", customEntry, &desyncCustomPlan{})
	if got := core.selections(desync.CustomRoute(customEntry.ID)); len(got) != 0 {
		t.Fatalf("superseded session changed the selector: %v", got)
	}
	if _, ok := a.GetDesyncAutoStatus().Custom[customEntry.ID]; ok {
		t.Fatal("superseded session published state")
	}
}

func TestDesyncCustomWorkerWaitsForBuiltinsAndServesOneEntryAtATime(t *testing.T) {
	second := DesyncCustomEntry{ID: "5a6b7c8d", Domain: "example.net", Enabled: true}
	core := newCustomFakeCore(t, func(name, target string) bool {
		return name == "DIRECT" && strings.Contains(target, "example.org") || strings.HasSuffix(name, "-split")
	})
	a, network := customTestApp(t, core, customEntry, second)
	a.desyncCtl.status.YouTube = DesyncAutoService{Phase: "testing"}
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() {
		defer close(done)
		a.runDesyncCustomService(ctx, 1, network, a.core.listen, "", DesyncSettings{YouTube: true})
	}()
	defer func() { cancel(); <-done }()
	time.Sleep(1500 * time.Millisecond)
	if n := len(core.probeLog()); n != 0 {
		t.Fatalf("custom search started during the built-in search: %d probes", n)
	}
	a.desyncCtl.setService(1, "youtube", DesyncAutoService{Phase: "ready"})
	deadline := time.Now().Add(15 * time.Second)
	for {
		status := a.GetDesyncAutoStatus()
		if status.Custom[customEntry.ID].Phase == "not_needed" && status.Custom[second.ID].Phase == "ready" {
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("entries not calibrated: %+v", status.Custom)
		}
		time.Sleep(20 * time.Millisecond)
	}
	if core.maxSeen.Load() != 1 {
		t.Fatalf("%d probes ran at once", core.maxSeen.Load())
	}
	if got := core.selections(desync.CustomRoute(second.ID)); got[len(got)-1] != desync.CustomRoute(second.ID)+"-split" {
		t.Fatalf("second entry selections: %v", got)
	}
	// Removing an entry drops its state without restarting the session.
	prefsMu.Lock()
	prefsCurrent.Desync.Custom = []DesyncCustomEntry{second}
	prefsMu.Unlock()
	deadline = time.Now().Add(5 * time.Second)
	for {
		status := a.GetDesyncAutoStatus()
		if _, ok := status.Custom[customEntry.ID]; !ok && status.Custom[second.ID].Phase == "ready" {
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("removed entry kept its state: %+v", status.Custom)
		}
		time.Sleep(20 * time.Millisecond)
	}
}

func TestDesyncReportHashesCustomDomains(t *testing.T) {
	core := newCustomFakeCore(t, func(name, _ string) bool { return name == "DIRECT" })
	a, network := customTestApp(t, core, customEntry)
	a.calibrateDesyncCustom(context.Background(), 1, network, a.core.listen, "", customEntry, &desyncCustomPlan{})
	a.state.Connection.Status = "disconnected"
	text := a.GetDesyncDiagnosticReport()
	if strings.Contains(text, "example.org") {
		t.Fatalf("custom domain leaked into the report: %s", text)
	}
	sum := sha256.Sum256([]byte(network + "example.org"))
	digest := hex.EncodeToString(sum[:6])
	var report struct {
		Custom    []map[string]any `json:"customDomains"`
		Automatic DesyncAutoStatus `json:"automatic"`
	}
	if err := json.Unmarshal([]byte(text), &report); err != nil {
		t.Fatal(err)
	}
	if len(report.Custom) != 1 || report.Custom[0]["domain"] != digest || report.Custom[0]["id"] != customEntry.ID {
		t.Fatalf("custom entries: %v", report.Custom)
	}
	matched := 0
	for _, attempt := range report.Automatic.Attempts {
		if attempt.Service == "custom" && attempt.Target == digest {
			matched++
		}
	}
	if matched == 0 || report.Automatic.Custom[customEntry.ID].Phase != "not_needed" {
		t.Fatalf("attempts do not match the entry digest: %+v", report.Automatic)
	}
}
