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

func TestDesyncTCPNeedsAllTargetsAndTwoRounds(t *testing.T) {
	candidates := []desync.Candidate{{ID: "first"}, {ID: "second"}, {ID: "unused"}}
	calls := map[string]int{}
	records := 0
	winner, err := chooseDesyncTCP(context.Background(), "discord", candidates, func(_ context.Context, c desync.Candidate, target string) error {
		calls[c.ID]++
		if c.ID == "first" && strings.Contains(target, "cdn.") {
			return fmt.Errorf("TLS handshake failed")
		}
		return nil
	}, func(_ desync.Candidate, _ int, _ error, _ time.Duration) { records++ })
	if err != nil || winner.ID != "second" || calls["first"] != 2 || calls["second"] != 4 || calls["unused"] != 0 || records != 2 {
		t.Fatalf("selection: %s %v %v", winner.ID, err, calls)
	}
}
func TestDesyncLateProbeCannotWinAfterCancellation(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	winner, err := chooseDesyncTCP(ctx, "youtube", []desync.Candidate{{ID: "late"}}, func(context.Context, desync.Candidate, string) error { cancel(); return nil }, func(desync.Candidate, int, error, time.Duration) {})
	if err == nil || winner.ID != "" {
		t.Fatal("a canceled result was selected")
	}
	a := &App{}
	a.desyncAuto.epoch = 2
	if err = a.applyDesyncCandidate(context.Background(), 1, "must-not-be-contacted.invalid", "private", "group", "candidate"); err != context.Canceled {
		t.Fatalf("old session mutation: %v", err)
	}
	a.connectGen.Add(1)
	if err = a.applyDesyncCandidate(context.Background(), 2, "must-not-be-contacted.invalid", "private", "group", "candidate"); err != context.Canceled {
		t.Fatalf("disconnect generation did not cancel selection: %v", err)
	}
}
func TestDesyncFirstPingAndSilenceAreNotVerifiedVoice(t *testing.T) {
	now := time.Now()
	p := desyncUDPActivity{FirstMS: now.Add(-30 * time.Second).UnixMilli(), LastTXMS: now.UnixMilli(), LastRXMS: now.Add(-25 * time.Second).UnixMilli(), TXPackets: 20, RXPackets: 2, DiscoveryRX: 1, TXBytes: 3000, RXBytes: 134}
	if got := desyncVoiceHealth(now, p, 0, 0, time.Time{}); got != "stalled" {
		t.Fatalf("first reply masked stall: %s", got)
	}
	if got := desyncVoiceHealth(now, p, 0, 0, now.Add(time.Minute)); got == "stalled" {
		t.Fatal("reconnect grace ignored")
	}
	p.LastRXMS = now.UnixMilli()
	p.RXPackets = 10
	p.DiscoveryRX = 1
	if got := desyncVoiceHealth(now, p, 3, 3, time.Time{}); got != "transport" {
		t.Fatalf("quiet channel classified as %s", got)
	}
	p.MediaTX = 10
	p.MediaRX = 10
	p.RXBytes = 4000
	if got := desyncVoiceHealth(now, p, 2, 2, time.Time{}); got == "media" {
		t.Fatal("one short burst marked stable")
	}
	if got := desyncVoiceHealth(now, p, 3, 3, time.Time{}); got != "media" {
		t.Fatalf("sustained media: %s", got)
	}
}
func TestDesyncUDPProbeDoesNotRewardFirstThreeReplies(t *testing.T) {
	if (desyncUDPProbe{Sent: 12, Received: 3, MaxConsecutiveLoss: 9, DurationMS: 5000}).passed() {
		t.Fatal("initial discovery was accepted")
	}
	if !(desyncUDPProbe{Sent: 12, Received: 11, MaxConsecutiveLoss: 1, DurationMS: 2500}).passed() {
		t.Fatal("sustained sequence rejected")
	}
}
func TestDesyncReportDoesNotSerializeSecretsAndProfiles(t *testing.T) {
	a := &App{coreSecret: "test-controller-secret"}
	a.state.Connection.LastError = "https://private.example/subscription-token"
	a.coreActiveProfileID = "secret-profile-id"
	a.desyncAuto.status.Attempts = []DesyncAutoAttempt{{Service: "voice", Candidate: "f4", Code: "udp_stalled"}}
	report := a.GetDesyncDiagnosticReport()
	for _, secret := range []string{"test-controller-secret", "private.example", "subscription-token", "secret-profile-id"} {
		if strings.Contains(report, secret) {
			t.Fatalf("private data in report: %s", secret)
		}
	}
	if !strings.Contains(report, "udp_stalled") {
		t.Fatal("useful attempt diagnostics missing")
	}
}

func TestDesyncConnectedReportAnonymizesUDPEndpoints(t *testing.T) {
	prefsMu.Lock()
	previous := prefsCurrent
	prefsCurrent.Desync = DesyncSettings{Enabled: true, DiscordUDP: true}
	prefsMu.Unlock()
	defer func() { prefsMu.Lock(); prefsCurrent = previous; prefsMu.Unlock() }()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/proxies/"+desync.DiscordUDPRoute {
			fmt.Fprintf(w, `{"now":%q}`, desync.DiscordUDPRoute+"-f4")
			return
		}
		fmt.Fprint(w, `{"desyncUDP":{"counters":{"packets":100,"fakePackets":3},"activity":[{"endpoint":"104.29.2.2:19330","txPackets":100,"rxPackets":80}],"binding":{"enabled":true,"nativeSockets":2,"handovers":13,"queuedBytes":0,"ssrc":"private-session-id","address":"private-binding-address","process":"private-process"}}}`)
	}))
	defer server.Close()
	a := &App{coreListen: strings.TrimPrefix(server.URL, "http://"), coreSecret: "private-controller-token"}
	a.state.Connection.Status = "connected"
	a.state.Core.Running = true
	text := a.GetDesyncDiagnosticReport()
	if strings.Contains(text, "104.29.2.2") || strings.Contains(text, "private-controller-token") || strings.Contains(text, server.URL) {
		t.Fatal("private transport metadata leaked")
	}
	var report map[string]any
	if err := json.Unmarshal([]byte(text), &report); err != nil {
		t.Fatal(err)
	}
	if report["udp"] == nil || !strings.Contains(text, `"packets": 100`) {
		t.Fatalf("transport evidence missing: %s", text)
	}
	if !strings.Contains(text, `"handovers": 13`) || strings.Contains(text, "private-session-id") || strings.Contains(text, "private-binding-address") || strings.Contains(text, "private-process") {
		t.Fatal("binding counters missing or private binding fields leaked")
	}
}

func TestDesyncVoiceTargetSkipsUnrelatedQUICAndOtherApplications(t *testing.T) {
	connections := []ConnectionItem{{Metadata: ConnectionMeta{Network: "udp", Process: "Discord.exe", DestinationIP: "1.1.1.1", DestinationPort: "443"}, Chains: []string{desync.DiscordUDPRoute}}, {Metadata: ConnectionMeta{Network: "udp", Process: "Discord.exe", DestinationIP: "104.29.1.1", DestinationPort: "19299"}, Chains: []string{"subscription"}}, {Metadata: ConnectionMeta{Network: "udp", Process: "Discord.exe", DestinationIP: "104.29.2.2", DestinationPort: "19330"}, Chains: []string{desync.DiscordUDPRoute}}}
	if got := desyncVoiceTarget(connections); got != "104.29.2.2:19330" {
		t.Fatalf("wrong target %q", got)
	}
}

func TestDesyncCacheIsNetworkScopedExpiresAndIsBounded(t *testing.T) {
	prefsMu.Lock()
	previous := prefsCurrent
	prefsCurrent.DesyncCache = nil
	prefsMu.Unlock()
	defer func() { prefsMu.Lock(); prefsCurrent = previous; prefsMu.Unlock() }()
	for i := 0; i < 8; i++ {
		saveDesyncSelection(fmt.Sprintf("network-%d", i), "voice", "f5")
	}
	if len(currentDesktopPrefs().DesyncCache) != 6 || desyncCachedSelection("network-0", "voice") != "" || desyncCachedSelection("network-7", "voice") != "f5" {
		t.Fatal("cache crossed network boundary or exceeded its bound")
	}
	prefsMu.Lock()
	entries := append([]DesyncNetworkCache(nil), prefsCurrent.DesyncCache...)
	entries[len(entries)-1].Updated = time.Now().Add(-25 * time.Hour).Unix()
	prefsCurrent.DesyncCache = entries
	prefsMu.Unlock()
	if desyncCachedSelection("network-7", "voice") != "" {
		t.Fatal("expired cache reused")
	}
	if got := desync.Ordered("voice", "not-a-strategy"); got[0].ID != "f4" {
		t.Fatal("invalid saved strategy accepted")
	}
	if got := desync.Ordered("voice", "f8"); got[0].ID != "f8" || len(got) != len(desync.Candidates("voice")) {
		t.Fatal("cached candidate removed alternatives")
	}
}

func TestDesyncCalibrationAppliesOnlyVerifiedCandidateAndCachesIt(t *testing.T) {
	prefsMu.Lock()
	previous := prefsCurrent
	prefsCurrent.Desync = DesyncSettings{Enabled: true, Discord: true}
	prefsCurrent.DesyncCache = nil
	prefsMu.Unlock()
	defer func() { prefsMu.Lock(); prefsCurrent = previous; prefsMu.Unlock() }()
	selected := ""
	checks := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodPut:
			var body struct {
				Name string `json:"name"`
			}
			if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
				t.Error(err)
			}
			selected = body.Name
			w.WriteHeader(204)
		case strings.HasSuffix(r.URL.Path, "/delay"):
			checks++
			if strings.Contains(r.URL.Path, "-f4/") {
				w.WriteHeader(503)
				fmt.Fprint(w, `{"message":"TLS handshake failed"}`)
			} else {
				fmt.Fprint(w, `{"delay":12}`)
			}
		default:
			fmt.Fprint(w, `{"type":"Selector","all":["candidate"]}`)
		}
	}))
	defer server.Close()
	a := &App{coreListen: strings.TrimPrefix(server.URL, "http://")}
	a.state.Connection.Status = "connected"
	a.state.Core.Running = true
	a.state.Mode.Current = "rule"
	network := desyncNetworkKey()
	a.desyncAuto.epoch = 1
	a.desyncAuto.status.Network = network
	err := a.calibrateDesyncTCP(context.Background(), 1, network, a.coreListen, "", "discord", desync.DiscordRoute)
	if err != nil || selected != desync.DiscordRoute+"-f5" || checks != 5 || desyncCachedSelection(network, "discord") != "f5" {
		t.Fatalf("calibration not applied: %q checks=%d err=%v", selected, checks, err)
	}
	status := a.GetDesyncAutoStatus()
	if status.Discord.Phase != "ready" || len(status.Attempts) != 2 || status.Attempts[0].OK || !status.Attempts[1].OK {
		t.Fatalf("wrong result timeline: %+v", status)
	}
}

func TestDesyncCalibrationResumesAfterInterruptedBudget(t *testing.T) {
	prefsMu.Lock()
	previous := prefsCurrent
	prefsCurrent.Desync = DesyncSettings{Enabled: true, Discord: true}
	prefsCurrent.DesyncCache = nil
	prefsMu.Unlock()
	defer func() { prefsMu.Lock(); prefsCurrent = previous; prefsMu.Unlock() }()
	firstCtx, cancel := context.WithCancel(context.Background())
	defer cancel()
	var firstPass atomic.Bool
	firstPass.Store(true)
	var firstCalls atomic.Int32
	selections := make(chan string, 1)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodPut {
			var body struct {
				Name string `json:"name"`
			}
			_ = json.NewDecoder(r.Body).Decode(&body)
			selections <- body.Name
			w.WriteHeader(204)
			return
		}
		if strings.HasSuffix(r.URL.Path, "/delay") {
			if firstPass.Load() {
				firstCalls.Add(1)
				if strings.Contains(r.URL.Path, "-f4/") {
					w.WriteHeader(503)
					fmt.Fprint(w, `{"message":"TLS handshake failed"}`)
					return
				}
				cancel()
				<-r.Context().Done()
				return
			}
			fmt.Fprint(w, `{"delay":12}`)
			return
		}
		fmt.Fprint(w, `{"type":"Selector","all":["candidate"]}`)
	}))
	defer server.Close()
	a := &App{coreListen: strings.TrimPrefix(server.URL, "http://")}
	a.state.Connection.Status = "connected"
	a.state.Core.Running = true
	a.state.Mode.Current = "rule"
	a.desyncAuto.epoch = 1
	network := desyncNetworkKey()
	a.desyncAuto.status.Network = network
	if err := a.calibrateDesyncTCP(firstCtx, 1, network, a.coreListen, "", "discord", desync.DiscordRoute); err == nil {
		t.Fatal("interrupted calibration succeeded")
	}
	firstPass.Store(false)
	if err := a.calibrateDesyncTCP(context.Background(), 1, network, a.coreListen, "", "discord", desync.DiscordRoute); err != nil {
		t.Fatal(err)
	}
	select {
	case selected := <-selections:
		if selected != desync.DiscordRoute+"-f3" || firstCalls.Load() != 2 {
			t.Fatalf("retried the prefix instead of resuming: %s calls=%d", selected, firstCalls.Load())
		}
	default:
		t.Fatal("resumed calibration did not apply a candidate")
	}
}

func TestDesyncKeepsWorkingApplicationTrafficWithoutSeparateProbe(t *testing.T) {
	prefsMu.Lock()
	previous := prefsCurrent
	prefsCurrent.Desync = DesyncSettings{Enabled: true, DiscordUDP: true}
	prefsCurrent.DesyncCache = nil
	prefsMu.Unlock()
	defer func() { prefsMu.Lock(); prefsCurrent = previous; prefsMu.Unlock() }()
	var probes, reads atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodPost {
			probes.Add(1)
			w.WriteHeader(503)
			return
		}
		if r.URL.Path == "/connections" {
			fmt.Fprintf(w, `{"connections":[{"id":"active-voice","start":"2026-10-25T01:00:00Z","metadata":{"network":"udp","process":"Discord.exe","destinationIP":"104.29.2.2","destinationPort":"19330"},"chains":[%q,%q]}]}`, desync.DiscordUDPRoute+"-f4", desync.DiscordUDPRoute)
			return
		}
		if r.URL.Path == "/proxies/"+desync.DiscordUDPRoute {
			fmt.Fprintf(w, `{"now":%q}`, desync.DiscordUDPRoute+"-f4")
			return
		}
		n := reads.Add(1)
		now := time.Now().UnixMilli()
		fmt.Fprintf(w, `{"desyncUDP":{"activity":[{"endpoint":"104.29.2.2:19330","firstMS":%d,"lastTXMS":%d,"lastRXMS":%d,"txPackets":%d,"rxPackets":%d,"txBytes":%d,"rxBytes":%d,"mediaTX":%d,"mediaRX":%d}]}}`, now-60000, now, now, n*20, n*20, n*1000, n*1000, n*10, n*10)
	}))
	defer server.Close()
	a := &App{coreListen: strings.TrimPrefix(server.URL, "http://")}
	a.state.Connection.Status = "connected"
	a.state.Core.Running = true
	a.state.Mode.Current = "rule"
	a.state.Traffic = "tun"
	a.desyncAuto.epoch = 1
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	finished := make(chan struct{})
	go func() {
		defer close(finished)
		a.runDesyncAutoSession(ctx, 1, desyncNetworkKey(), a.coreListen, "", DesyncStatus{Settings: currentDesyncSettings(), Traffic: "tun"})
	}()
	deadline := time.After(12 * time.Second)
	ticker := time.NewTicker(20 * time.Millisecond)
	defer ticker.Stop()
	for {
		select {
		case <-deadline:
			cancel()
			<-finished
			t.Fatal("healthy application traffic was not observed")
		case <-ticker.C:
			if a.GetDesyncAutoStatus().Voice.Phase == "media" {
				cancel()
				<-finished
				if probes.Load() != 0 {
					t.Fatal("independent probe overrode a working call")
				}
				return
			}
		}
	}
}
