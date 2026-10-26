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

// calibrateCustomToEnd runs calibration steps (baseline, then one search
// budget per step) until the entry leaves the search.
func calibrateCustomToEnd(a *App, lease uint64, network, endpoint, secret string, entry DesyncCustomEntry, plan *desyncCustomPlan) {
	for range 1000 {
		a.calibrateDesyncCustom(context.Background(), lease, network, endpoint, secret, entry, plan)
		if plan.phase != "searching" {
			return
		}
	}
	panic("custom calibration did not finish")
}

// withShortCustomTimers shrinks the worker's budget, re-check interval and
// budget gap for a test.
func withShortCustomTimers(t *testing.T, budget, checkEvery, gap time.Duration) {
	t.Helper()
	b, c, g := desyncCustomBudget, desyncCustomCheckEvery, desyncCustomBudgetGap
	desyncCustomBudget, desyncCustomCheckEvery, desyncCustomBudgetGap = budget, checkEvery, gap
	t.Cleanup(func() { desyncCustomBudget, desyncCustomCheckEvery, desyncCustomBudgetGap = b, c, g })
}

func TestDesyncCustomBlockedSearchLetsOtherRechecksRun(t *testing.T) {
	blocked := DesyncCustomEntry{ID: "1a2b3c4d", Domain: "blocked.example", Enabled: true}
	working := DesyncCustomEntry{ID: "5a6b7c8d", Domain: "example.net", Enabled: true}
	blockedRoute, workingRoute := desync.CustomRoute(blocked.ID), desync.CustomRoute(working.ID)
	// The blocked site behaves like an IP block: every probe hangs, then
	// fails. The working site passes directly and through its group.
	core := newCustomFakeCore(t, func(name, target string) bool {
		if strings.Contains(target, "blocked.example") {
			time.Sleep(120 * time.Millisecond)
			return false
		}
		return name == "DIRECT" || name == workingRoute
	})
	withShortCustomTimers(t, 300*time.Millisecond, 100*time.Millisecond, 0)
	a, network := customTestApp(t, core, blocked, working)
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() {
		defer close(done)
		a.runDesyncCustomService(ctx, 1, network, a.core.listen, "", DesyncSettings{})
	}()
	defer func() { cancel(); <-done }()
	deadline := time.Now().Add(20 * time.Second)
	for a.GetDesyncAutoStatus().Custom[blocked.ID].Phase != "unavailable" {
		if time.Now().After(deadline) {
			t.Fatalf("blocked entry never finished its search: %+v", a.GetDesyncAutoStatus().Custom)
		}
		time.Sleep(20 * time.Millisecond)
	}
	probes := core.probeLog()
	first, last := -1, -1
	for i, p := range probes {
		if strings.HasPrefix(p, blockedRoute+"-") {
			if first < 0 {
				first = i
			}
			last = i
		}
	}
	rechecks := 0
	for _, p := range probes[first+1 : last] {
		if strings.HasPrefix(p, workingRoute+" ") {
			rechecks++
		}
	}
	if first < 0 || rechecks == 0 {
		t.Fatalf("no re-check of the working entry ran during the blocked search: %v", probes)
	}
	if status := a.GetDesyncAutoStatus().Custom[working.ID]; status.Phase != "not_needed" {
		t.Fatalf("working entry: %+v", status)
	}
}

func TestDesyncCustomNextPicksMostOverdue(t *testing.T) {
	now := time.Now()
	a, b, c := DesyncCustomEntry{ID: "a"}, DesyncCustomEntry{ID: "b"}, DesyncCustomEntry{ID: "c"}
	plans := map[string]*desyncCustomPlan{
		"a": {phase: "searching", due: now.Add(-time.Second)},
		"b": {phase: "ready", due: now.Add(-time.Minute)},
		"c": {phase: "ready", due: now.Add(time.Minute)},
	}
	if e, _ := nextDesyncCustom([]DesyncCustomEntry{a, b, c}, plans, now); e.ID != "b" {
		t.Fatalf("picked %q, want the most overdue entry b", e.ID)
	}
	plans["b"].due = now.Add(time.Minute)
	if e, _ := nextDesyncCustom([]DesyncCustomEntry{a, b, c}, plans, now); e.ID != "a" {
		t.Fatalf("picked %q, want a", e.ID)
	}
	plans["a"].due = now.Add(time.Second)
	if _, p := nextDesyncCustom([]DesyncCustomEntry{a, b, c}, plans, now); p != nil {
		t.Fatalf("picked an entry that is not due: %+v", p)
	}
}

func TestDesyncCustomNotBlockedSelectsDirect(t *testing.T) {
	core := newCustomFakeCore(t, func(name, _ string) bool { return name == "DIRECT" })
	a, network := customTestApp(t, core, customEntry)
	plan := &desyncCustomPlan{phase: "queued"}
	calibrateCustomToEnd(a, 1, network, a.core.listen, "", customEntry, plan)
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
	calibrateCustomToEnd(a, 1, network, a.core.listen, "", customEntry, plan)
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
	calibrateCustomToEnd(a, 1, network, a.core.listen, "", customEntry, &desyncCustomPlan{})
	if probes := core2.probeLog(); len(probes) != 3 || !strings.HasPrefix(probes[1], route+"-r2 ") {
		t.Fatalf("cached winner not first: %v", probes)
	}
}

func TestDesyncCustomNothingWorksFallsBackToProfile(t *testing.T) {
	core := newCustomFakeCore(t, func(string, string) bool { return false })
	a, network := customTestApp(t, core, customEntry)
	plan := &desyncCustomPlan{phase: "queued"}
	calibrateCustomToEnd(a, 1, network, a.core.listen, "", customEntry, plan)
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
	calibrateCustomToEnd(a, 1, network, a.core.listen, "", customEntry, plan)
	if wait := time.Until(plan.due); plan.failures != 2 || wait < 110*time.Second {
		t.Fatalf("backoff did not double: %v %+v", wait, plan)
	}
}

func TestDesyncCustomStaleLeaseCannotSelect(t *testing.T) {
	core := newCustomFakeCore(t, func(name, _ string) bool { return name == "DIRECT" })
	a, network := customTestApp(t, core, customEntry)
	a.desyncCtl.epoch = 2 // a newer session took over
	calibrateCustomToEnd(a, 1, network, a.core.listen, "", customEntry, &desyncCustomPlan{})
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
	secret := strings.Repeat("ab", 32)
	withDesyncReportKey(t, secret)
	calibrateCustomToEnd(a, 1, network, a.core.listen, "", customEntry, &desyncCustomPlan{})
	a.state.Connection.Status = "disconnected"
	text := a.GetDesyncDiagnosticReport()
	if strings.Contains(text, "example.org") {
		t.Fatalf("custom domain leaked into the report: %s", text)
	}
	if strings.Contains(strings.ToLower(text), secret) {
		t.Fatalf("report secret leaked into the report: %s", text)
	}
	key, _ := hex.DecodeString(secret)
	digest := desyncReportDigest(key, "example.org")
	report := parseDesyncReport(t, text)
	if len(report.Custom) != 1 || report.Custom[0]["domain"] != digest || report.Custom[0]["id"] != customEntry.ID {
		t.Fatalf("custom entries: %v (want digest %s)", report.Custom, digest)
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
	// Stable across reports on one install.
	if again := parseDesyncReport(t, a.GetDesyncDiagnosticReport()); again.Custom[0]["domain"] != digest {
		t.Fatalf("digest changed between reports: %v", again.Custom)
	}
	// Nothing in the report reproduces the digest: neither the old network
	// salt nor any value the report carries works as a key or salt.
	for _, v := range desyncReportStrings(t, text) {
		salted := sha256.Sum256([]byte(v + "example.org"))
		if hex.EncodeToString(salted[:6]) == digest || desyncReportDigest([]byte(v), "example.org") == digest {
			t.Fatalf("digest reproducible from report value %q", v)
		}
	}
	if salted := sha256.Sum256([]byte(network + "example.org")); hex.EncodeToString(salted[:6]) == digest {
		t.Fatal("digest reproducible from the network key")
	}
	// Another install (another secret) yields another digest.
	withDesyncReportKey(t, strings.Repeat("cd", 32))
	if other := parseDesyncReport(t, a.GetDesyncDiagnosticReport()); other.Custom[0]["domain"] == digest {
		t.Fatalf("a different secret produced the same digest %s", digest)
	}
}

type parsedDesyncReport struct {
	Custom    []map[string]any `json:"customDomains"`
	Automatic DesyncAutoStatus `json:"automatic"`
}

func parseDesyncReport(t *testing.T, text string) parsedDesyncReport {
	t.Helper()
	var report parsedDesyncReport
	if err := json.Unmarshal([]byte(text), &report); err != nil {
		t.Fatal(err)
	}
	return report
}

// desyncReportStrings returns every string (and every object key) in the
// report, plus "" (an empty salt / key).
func desyncReportStrings(t *testing.T, text string) []string {
	t.Helper()
	var root any
	if err := json.Unmarshal([]byte(text), &root); err != nil {
		t.Fatal(err)
	}
	out := []string{""}
	var walk func(v any)
	walk = func(v any) {
		switch x := v.(type) {
		case string:
			out = append(out, x)
		case []any:
			for _, e := range x {
				walk(e)
			}
		case map[string]any:
			for k, e := range x {
				out = append(out, k)
				walk(e)
			}
		}
	}
	walk(root)
	return out
}
