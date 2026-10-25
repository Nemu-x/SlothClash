package main

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"SlothClashDesktop/internal/desync"
)

func TestDesyncReserveNeedsSeparatedRecoveryAndMinimumDwell(t *testing.T) {
	now := time.Now()
	r := desyncReserveRecovery{since: now}
	for i := 0; i < 10; i++ {
		if r.observe(now.Add(time.Second), "r3", true) {
			t.Fatal("burst restored reserve")
		}
	}
	if r.passed != 1 {
		t.Fatal("same instant counted more than once")
	}
	if r.observe(now.Add(time.Minute), "r3", true) || r.observe(now.Add(2*time.Minute), "r3", true) {
		t.Fatal("minimum dwell ignored")
	}
	if !r.observe(now.Add(3*time.Minute), "r3", true) {
		t.Fatal("separated confirmed recovery did not restore")
	}
	if r.ready(now.Add(5 * time.Minute)) {
		t.Fatal("stale probes authorized restoration")
	}
	if r.observe(now.Add(5*time.Minute), "r3", false) || r.passed != 0 {
		t.Fatal("failure retained success sequence")
	}
	if r.observe(now.Add(6*time.Minute), "r3", true) || r.observe(now.Add(7*time.Minute), "r1", true) || r.passed != 1 {
		t.Fatal("different candidates combined into success")
	}
}

func TestDesyncOneFailedCheckAndCancellationNeverTriggerRecovery(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	calls := 0
	check := func(context.Context) error {
		calls++
		if calls == 1 {
			return nil
		}
		return fmt.Errorf("timeout")
	}
	if confirmDesyncTCPFailure(ctx, check, func(context.Context) bool { return true }) {
		t.Fatal("a transient error triggered recovery")
	}
	if !confirmDesyncTCPFailure(ctx, check, func(context.Context) bool { return true }) {
		t.Fatal("persistent failure was ignored")
	}
	cancel()
	if confirmDesyncTCPFailure(ctx, check, func(context.Context) bool { return true }) {
		t.Fatal("canceled work authorized mutation")
	}
}

func TestDesyncUnavailableReserveCannotBePromotedOrLeakRawReason(t *testing.T) {
	for _, reply := range []string{`{"ok":true,"route":"direct"}`, `{"ok":false,"route":"remote","code":"private node token"}`, `{"ok":false,"route":"blocked","code":"reserve_blocked"}`} {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { fmt.Fprint(w, reply) }))
		_, err := probeDesyncProfile(context.Background(), strings.TrimPrefix(server.URL, "http://"), "", desync.YouTubeRoute, "")
		server.Close()
		if err == nil || strings.Contains(err.Error(), "private node") {
			t.Fatal("invalid/untrusted reserve result accepted")
		}
	}
}

func TestDesyncTCPFailedCandidatesUseVerifiedReserveWithoutReload(t *testing.T) {
	prefsMu.Lock()
	previous := prefsCurrent
	prefsCurrent.Desync = DesyncSettings{Enabled: true, YouTube: true}
	prefsCurrent.DesyncCache = nil
	prefsMu.Unlock()
	defer func() { prefsMu.Lock(); prefsCurrent = previous; prefsMu.Unlock() }()
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	var verified atomic.Int32
	selected := make(chan string, 1)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case strings.HasSuffix(r.URL.Path, "/profile-probe"):
			verified.Add(1)
			fmt.Fprint(w, `{"ok":true,"route":"remote","durationMS":12}`)
		case r.Method == http.MethodPut:
			var b struct {
				Name string `json:"name"`
			}
			_ = json.NewDecoder(r.Body).Decode(&b)
			if verified.Load() != 2 {
				t.Error("reserve selected without independent confirmations")
			}
			selected <- b.Name
			w.WriteHeader(204)
		case strings.HasSuffix(r.URL.Path, "/delay"):
			w.WriteHeader(503)
			fmt.Fprint(w, `{"message":"TLS timeout"}`)
		default:
			fmt.Fprint(w, `{"type":"Selector","all":["candidate","PASS"],"now":"candidate"}`)
		}
	}))
	defer server.Close()
	a := &App{core: coreProcess{listen: strings.TrimPrefix(server.URL, "http://")}}
	a.state.Connection.Status = "connected"
	a.state.Core.Running = true
	a.state.Mode.Current = "rule"
	a.desyncAuto.epoch = 1
	network := desyncNetworkKey()
	a.desyncAuto.status.Network = network
	done := make(chan struct{})
	go func() {
		defer close(done)
		a.runDesyncTCPService(ctx, 1, network, a.core.listen, "", "youtube", desync.YouTubeRoute)
	}()
	select {
	case value := <-selected:
		if value != "PASS" {
			t.Fatalf("wrong reserve %s", value)
		}
	case <-time.After(6 * time.Second):
		t.Fatal("bounded failure did not select reserve")
	}
	deadline := time.Now().Add(time.Second)
	for a.GetDesyncAutoStatus().YouTube.Phase != "profile" && time.Now().Before(deadline) {
		time.Sleep(time.Millisecond)
	}
	cancel()
	<-done
	report := a.GetDesyncDiagnosticReport()
	if !strings.Contains(report, "fallback_applied") || !strings.Contains(report, `"route": "profile"`) {
		t.Fatalf("reserve transition missing: %s", report)
	}
}

func TestDesyncReserveRecheckProtectsRecoveredAndUnknownPeers(t *testing.T) {
	now := time.Now()
	connections := []ConnectionItem{monitorConnection("104.29.1.1")}
	p := desyncUDPActivity{Endpoint: "104.29.1.1:19330", FirstMS: now.Add(-2 * time.Minute).UnixMilli(), LastTXMS: now.UnixMilli(), LastRXMS: now.Add(-time.Minute).UnixMilli()}
	if !desyncAllPeersStillStalled(now, connections, []desyncUDPActivity{p}) {
		t.Fatal("persistent failed peer rejected")
	}
	p.LastRXMS = now.UnixMilli()
	if desyncAllPeersStillStalled(now, connections, []desyncUDPActivity{p}) {
		t.Fatal("recovered voice could be replaced")
	}
	p.LastRXMS = now.Add(-time.Minute).UnixMilli()
	connections = append(connections, monitorConnection("104.29.1.2"))
	if desyncAllPeersStillStalled(now, connections, []desyncUDPActivity{p}) {
		t.Fatal("unobserved new call could be replaced")
	}
}

func TestDesyncVoiceReserveClosesOnlyConfirmedFailedOwnedFlows(t *testing.T) {
	prefsMu.Lock()
	previous := prefsCurrent
	prefsCurrent.Desync = DesyncSettings{Enabled: true, DiscordUDP: true}
	prefsMu.Unlock()
	defer func() { prefsMu.Lock(); prefsCurrent = previous; prefsMu.Unlock() }()
	for _, recovered := range []bool{false, true} {
		t.Run(fmt.Sprint(recovered), func(t *testing.T) {
			var puts, deletes atomic.Int32
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				switch {
				case r.Method == http.MethodPost:
					fmt.Fprint(w, `{"ok":true,"route":"remote","sent":12,"received":12,"durationMS":2200}`)
				case r.Method == http.MethodPut:
					puts.Add(1)
					w.WriteHeader(204)
				case r.Method == http.MethodDelete:
					if r.URL.Path != "/connections/failed-owned" {
						t.Error("unrelated flow closed")
					}
					deletes.Add(1)
					w.WriteHeader(204)
				case r.URL.Path == "/connections":
					c := monitorConnection("104.29.1.1")
					c.ID = "failed-owned"
					other := c
					other.ID = "profile-owned"
					other.Chains = []string{"private-profile-node"}
					raw, _ := json.Marshal(map[string]any{"connections": []ConnectionItem{c, other}})
					_, _ = w.Write(raw)
				default:
					now := time.Now()
					lastRX := now.Add(-time.Minute).UnixMilli()
					if recovered {
						lastRX = now.UnixMilli()
					}
					fmt.Fprintf(w, `{"desyncUDP":{"activity":[{"endpoint":"104.29.1.1:19330","firstMS":%d,"lastTXMS":%d,"lastRXMS":%d}]}}`, now.Add(-2*time.Minute).UnixMilli(), now.UnixMilli(), lastRX)
				}
			}))
			defer server.Close()
			a := &App{core: coreProcess{listen: strings.TrimPrefix(server.URL, "http://")}}
			a.state.Connection.Status = "connected"
			a.state.Core.Running = true
			a.state.Mode.Current = "rule"
			a.desyncAuto.epoch = 1
			a.desyncAuto.status.Network = desyncNetworkKey()
			changed := a.tryDesyncVoiceReserve(context.Background(), 1, a.core.listen, "", "104.29.1.1:19330", desync.DiscordUDPRoute+"-f4")
			if changed == recovered || puts.Load() != deletes.Load() || (!recovered && puts.Load() != 1) || (recovered && puts.Load() != 0) {
				t.Fatalf("changed=%t puts=%d deletes=%d", changed, puts.Load(), deletes.Load())
			}
		})
	}
}

func TestDesyncUnavailableProfileRetainsSelectorAndBackgroundSearchCannotSwitch(t *testing.T) {
	prefsMu.Lock()
	previous := prefsCurrent
	prefsCurrent.Desync = DesyncSettings{Enabled: true, YouTube: true}
	prefsCurrent.DesyncCache = nil
	prefsMu.Unlock()
	defer func() { prefsMu.Lock(); prefsCurrent = previous; prefsMu.Unlock() }()
	var puts atomic.Int32
	var candidatesPass atomic.Bool
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodPut {
			puts.Add(1)
			w.WriteHeader(204)
			return
		}
		if strings.HasSuffix(r.URL.Path, "/profile-probe") {
			fmt.Fprint(w, `{"ok":false,"route":"direct","code":"reserve_direct"}`)
			return
		}
		if strings.HasSuffix(r.URL.Path, "/delay") {
			if candidatesPass.Load() {
				fmt.Fprint(w, `{"delay":12}`)
			} else {
				w.WriteHeader(503)
			}
			return
		}
		fmt.Fprint(w, `{"type":"Selector","all":["candidate","PASS"]}`)
	}))
	defer server.Close()
	a := &App{core: coreProcess{listen: strings.TrimPrefix(server.URL, "http://")}}
	a.state.Connection.Status = "connected"
	a.state.Core.Running = true
	a.state.Mode.Current = "rule"
	a.desyncAuto.epoch = 1
	a.desyncAuto.status.Network = desyncNetworkKey()
	ctx, cancel := context.WithTimeout(context.Background(), 250*time.Millisecond)
	a.runDesyncTCPService(ctx, 1, a.desyncAuto.status.Network, a.core.listen, "", "youtube", desync.YouTubeRoute)
	cancel()
	if puts.Load() != 0 || a.GetDesyncAutoStatus().YouTube.Code != "reserve_direct" {
		t.Fatal("unavailable reserve changed selector or hid reason")
	}
	candidatesPass.Store(true)
	a.desyncService(1, "youtube", DesyncAutoService{Phase: "profile", Candidate: "profile"})
	first, err := a.findDesyncTCP(context.Background(), 1, a.desyncAuto.status.Network, a.core.listen, "", "youtube", desync.YouTubeRoute, true)
	if err != nil {
		t.Fatal(err)
	}
	second, err := a.findDesyncTCP(context.Background(), 1, a.desyncAuto.status.Network, a.core.listen, "", "youtube", desync.YouTubeRoute, true)
	if err != nil || first.ID != second.ID {
		t.Fatal("background confirmation rotated a passing candidate")
	}
	if puts.Load() != 0 || a.GetDesyncAutoStatus().YouTube.Phase != "profile" {
		t.Fatal("prospective recovery changed live selector/status")
	}
}
