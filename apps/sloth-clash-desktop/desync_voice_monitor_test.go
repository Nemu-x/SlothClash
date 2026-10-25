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

func monitorConnection(ip string) ConnectionItem {
	return ConnectionItem{Metadata: ConnectionMeta{Network: "udp", Process: "Discord.exe", DestinationIP: ip, DestinationPort: "19330"}, Chains: []string{desync.DiscordUDPRoute + "-f4", desync.DiscordUDPRoute}}
}

func TestDesyncStreamStallDoesNotReplaceHealthyVoice(t *testing.T) {
	// Regression shape from the owner's r5 report: large receive-only stream
	// stalls while a different endpoint continues two-way voice traffic.
	now := time.Now()
	connections := []ConnectionItem{monitorConnection("104.29.1.1"), monitorConnection("104.29.1.2")}
	monitor := desyncVoiceMonitor{}
	for sample := 0; sample < 25; sample++ {
		at := now.Add(time.Duration(sample) * 4 * time.Second)
		activity := []desyncUDPActivity{
			{Endpoint: "104.29.1.1:19330", FirstMS: now.Add(-5 * time.Minute).UnixMilli(), LastTXMS: at.UnixMilli(), LastRXMS: now.Add(-25 * time.Second).UnixMilli(), TXPackets: 1252 + uint64(sample), RXPackets: 23402, TXBytes: 75558, RXBytes: 24519462, DiscoveryTX: 3, DiscoveryRX: 3, MediaRX: 23266},
			{Endpoint: "104.29.1.2:19330", FirstMS: now.Add(-6 * time.Minute).UnixMilli(), LastTXMS: at.UnixMilli(), LastRXMS: at.UnixMilli(), TXPackets: 6314 + uint64(sample*10), RXPackets: 1756 + uint64(sample*10), TXBytes: 1219839, RXBytes: 250843, MediaTX: 6136 + uint64(sample), MediaRX: 1168 + uint64(sample)},
		}
		d := monitor.observe(at, connections, activity)
		if d.target != "" {
			t.Fatalf("sample %d: independent stream failure requested a global strategy change: %+v", sample, d)
		}
		if sample >= 2 && (d.phase != "partial" || d.healthy != 1 || d.stalled != 1) {
			t.Fatalf("lost healthy voice/degraded stream distinction: %+v", d)
		}
	}
}

func TestDesyncReceiveOnlyMediaIsHealthyAndNewPeerDoesNotResetIt(t *testing.T) {
	now := time.Now()
	connections := []ConnectionItem{monitorConnection("104.29.1.1")}
	monitor := desyncVoiceMonitor{}
	for sample := 0; sample < 8; sample++ {
		at := now.Add(time.Duration(sample) * 4 * time.Second)
		if sample == 4 {
			connections = append(connections, monitorConnection("104.29.1.2"))
		}
		p := desyncUDPActivity{Endpoint: "104.29.1.1:19330", FirstMS: now.Add(-time.Minute).UnixMilli(), LastTXMS: at.UnixMilli(), LastRXMS: at.UnixMilli(), TXPackets: 200 + uint64(sample), RXPackets: 1000 + uint64(sample*50), TXBytes: 5000, RXBytes: 500000, MediaRX: 900 + uint64(sample*50)}
		d := monitor.observe(at, connections, []desyncUDPActivity{p})
		if sample >= 2 && (d.phase != "receiving" || d.target != "") {
			t.Fatalf("receive-only stream misclassified: %+v", d)
		}
	}
}

func TestDesyncGlobalRecoveryNeedsPersistentFailureOfAllActivePeers(t *testing.T) {
	now := time.Now()
	monitor := desyncVoiceMonitor{}
	connections := []ConnectionItem{monitorConnection("104.29.1.1")}
	for sample := 0; sample < 18; sample++ {
		at := now.Add(time.Duration(sample) * 4 * time.Second)
		p := desyncUDPActivity{Endpoint: "104.29.1.1:19330", FirstMS: now.Add(-time.Minute).UnixMilli(), LastTXMS: at.UnixMilli(), LastRXMS: now.Add(-30 * time.Second).UnixMilli(), TXPackets: 100 + uint64(sample), RXPackets: 100, TXBytes: 6000, RXBytes: 6000}
		d := monitor.observe(at, connections, []desyncUDPActivity{p})
		if sample < 15 && d.target != "" {
			t.Fatalf("established flow replaced before grace: %+v", d)
		}
		if sample >= 15 && d.target == "" {
			t.Fatalf("persistent whole-service failure could not recover: %+v", d)
		}
	}
}

func TestDesyncProbeWinnerCannotOverrideRecoveredApplicationTraffic(t *testing.T) {
	puts := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodPut:
			puts++
			w.WriteHeader(204)
		case r.Method == http.MethodPost:
			fmt.Fprint(w, `{"sent":12,"received":12,"maxConsecutiveLoss":0,"durationMS":2200}`)
		case r.URL.Path == "/proxies/"+desync.DiscordUDPRoute:
			fmt.Fprintf(w, `{"now":%q}`, desync.DiscordUDPRoute+"-f4")
		default:
			fmt.Fprintf(w, `{"desyncUDP":{"activity":[{"lastRXMS":%d,"rxPackets":100,"discoveryRX":3}]}}`, time.Now().UnixMilli())
		}
	}))
	defer server.Close()
	a := &App{}
	selected, changed, err := a.calibrateDesyncUDP(context.Background(), 0, "test-network", strings.TrimPrefix(server.URL, "http://"), "", "104.29.1.1:19330", map[string]bool{"f4": true})
	if err != nil || changed || puts != 0 || selected != desync.DiscordUDPRoute+"-f4" {
		t.Fatalf("recovered application overridden: %s %t %d %v", selected, changed, puts, err)
	}
}

func TestDesyncControllerErrorsKeepAllowlistedReason(t *testing.T) {
	for _, test := range []struct {
		status        int
		message, code string
	}{{400, "expected an owned UDP candidate and an active public Discord UDP endpoint", "endpoint_changed"}, {503, "UDP candidate socket failed", "native_socket"}, {429, "UDP calibration is already running", "busy"}, {503, "private arbitrary detail", "transport"}} {
		t.Run(test.code, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.WriteHeader(test.status)
				_ = json.NewEncoder(w).Encode(map[string]string{"message": test.message})
			}))
			defer server.Close()
			err := autoCoreJSON(context.Background(), strings.TrimPrefix(server.URL, "http://"), "", http.MethodGet, "/", nil, nil)
			if desyncErrorCode(err) != test.code || strings.Contains(err.Error(), "private arbitrary") {
				t.Fatalf("wrong error category: %v", err)
			}
		})
	}
}

func TestDesyncReportSeparatesDirectAndProfileFlowsWithoutPrivateRoutes(t *testing.T) {
	prefsMu.Lock()
	previous := prefsCurrent
	prefsCurrent.Desync = DesyncSettings{Enabled: true, DiscordUDP: true}
	prefsMu.Unlock()
	defer func() { prefsMu.Lock(); prefsCurrent = previous; prefsMu.Unlock() }()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/connections" {
			fmt.Fprintf(w, `{"connections":[{"metadata":{"network":"udp","process":"Discord.exe","destinationIP":"104.29.1.1","destinationPort":"19330"},"chains":[%q,%q]},{"metadata":{"network":"udp","process":"Discord.exe","destinationIP":"104.29.1.2","destinationPort":"19330"},"chains":["private subscription node"]}]}`, desync.DiscordUDPRoute+"-f4", desync.DiscordUDPRoute)
			return
		}
		fmt.Fprint(w, `{}`)
	}))
	defer server.Close()
	a := &App{coreListen: strings.TrimPrefix(server.URL, "http://")}
	a.state.Connection.Status = "connected"
	a.state.Core.Running = true
	a.desyncAttempt(0, DesyncAutoAttempt{Service: "voice", Target: "104.29.1.1:19330", Code: "udp_stalled"})
	report := a.GetDesyncDiagnosticReport()
	if strings.Contains(report, "104.29.") || strings.Contains(report, "private subscription") || !strings.Contains(report, `"route": "profile"`) || !strings.Contains(report, `"route": "desync"`) {
		t.Fatalf("route evidence missing or privacy regression: %s", report)
	}
}

func TestDesyncStreamFailureDoesNotProbeOrCloseWorkingCall(t *testing.T) {
	prefsMu.Lock()
	previous := prefsCurrent
	prefsCurrent.Desync = DesyncSettings{Enabled: true, DiscordUDP: true}
	prefsCurrent.DesyncCache = nil
	prefsMu.Unlock()
	defer func() { prefsMu.Lock(); prefsCurrent = previous; prefsMu.Unlock() }()
	var mutations, reads atomic.Int32
	started := time.Now()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			mutations.Add(1)
			w.WriteHeader(503)
			return
		}
		if r.URL.Path == "/connections" {
			_ = json.NewEncoder(w).Encode(map[string]any{"connections": []ConnectionItem{monitorConnection("104.29.1.1"), monitorConnection("104.29.1.2")}})
			return
		}
		if r.URL.Path == "/proxies/"+desync.DiscordUDPRoute {
			fmt.Fprintf(w, `{"now":%q}`, desync.DiscordUDPRoute+"-f4")
			return
		}
		n := uint64(reads.Add(1))
		now := time.Now().UnixMilli()
		_ = json.NewEncoder(w).Encode(map[string]any{"desyncUDP": map[string]any{"activity": []desyncUDPActivity{
			{Endpoint: "104.29.1.1:19330", FirstMS: started.Add(-time.Minute).UnixMilli(), LastTXMS: now, LastRXMS: started.Add(-30 * time.Second).UnixMilli(), TXPackets: 1200 + n, RXPackets: 23000, TXBytes: 75000, RXBytes: 24000000, MediaRX: 22000},
			{Endpoint: "104.29.1.2:19330", FirstMS: started.Add(-time.Minute).UnixMilli(), LastTXMS: now, LastRXMS: now, TXPackets: 100 + n, RXPackets: 100 + n, TXBytes: 6000, RXBytes: 6000, MediaTX: 50 + n, MediaRX: 50 + n},
		}}})
	}))
	defer server.Close()
	a := &App{coreListen: strings.TrimPrefix(server.URL, "http://")}
	a.desyncCtl.epoch = 1
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	finished := make(chan struct{})
	go func() { defer close(finished); a.runDesyncVoiceService(ctx, 1, desyncNetworkKey(), a.coreListen, "") }()
	ticker := time.NewTicker(20 * time.Millisecond)
	defer ticker.Stop()
	// Four polls allow the 8-second persistence boundary even when the first
	// controller request starts a few milliseconds after the ticker.
	deadline := time.After(18 * time.Second)
	for {
		select {
		case <-deadline:
			cancel()
			<-finished
			t.Fatal("independent stream/call state not observed")
		case <-ticker.C:
			if a.GetDesyncAutoStatus().Voice.Phase == "partial" {
				cancel()
				<-finished
				if mutations.Load() != 0 {
					t.Fatalf("healthy call disturbed by %d controller mutations", mutations.Load())
				}
				return
			}
		}
	}
}

func TestDesyncNativeProbeErrorDoesNotRejectEveryTTL(t *testing.T) {
	posts := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodPost {
			posts++
			w.WriteHeader(503)
			fmt.Fprint(w, `{"message":"UDP candidate socket failed"}`)
			return
		}
		fmt.Fprintf(w, `{"now":%q}`, desync.DiscordUDPRoute+"-f4")
	}))
	defer server.Close()
	a := &App{}
	rejected := map[string]bool{"f4": true}
	_, _, err := a.calibrateDesyncUDP(context.Background(), 0, "test-network", strings.TrimPrefix(server.URL, "http://"), "", "104.29.1.1:19330", rejected)
	if desyncErrorCode(err) != "native_socket" || posts != 1 || len(rejected) != 1 {
		t.Fatalf("socket error caused TTL churn: posts=%d rejected=%v err=%v", posts, rejected, err)
	}
}
