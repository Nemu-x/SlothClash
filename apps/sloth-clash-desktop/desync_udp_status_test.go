package main

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"sync"
	"testing"

	"SlothClashDesktop/internal/desync"
)

func TestDiscordUDPStatusAndResetScope(t *testing.T) {
	prefsMu.Lock()
	previous := prefsCurrent
	prefsCurrent.Desync = DesyncSettings{Enabled: true, DiscordUDP: true}
	prefsMu.Unlock()
	defer func() { prefsMu.Lock(); prefsCurrent = previous; prefsMu.Unlock() }()
	connections := []ConnectionItem{
		{ID: "direct-udp", Metadata: ConnectionMeta{Network: "udp", Process: "Discord.exe"}, Chains: []string{desync.DiscordUDPRoute}, Download: 100},
		{ID: "profile-udp", Metadata: ConnectionMeta{Network: "udp", Process: `C:\Apps\Discord.exe`}, Chains: []string{"test-node", "Discord"}, Download: 200},
		{ID: "discord-tcp", Metadata: ConnectionMeta{Network: "tcp", Process: "Discord.exe"}, Chains: []string{desync.DiscordRoute}},
		{ID: "browser-udp", Metadata: ConnectionMeta{Network: "udp", Process: "firefox.exe"}, Chains: []string{"browser-node"}},
	}
	var mu sync.Mutex
	var deleted []string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodGet && r.URL.Path == "/connections":
			_ = json.NewEncoder(w).Encode(map[string]any{"connections": connections})
		case r.Method == http.MethodGet && r.URL.Path == "/proxies/"+desync.DiscordUDPRoute:
			_, _ = w.Write([]byte(`{"desyncUDP":{"counters":{"packets":12,"fakePackets":18,"fakeErrors":0,"peers":1}}}`))
		case r.Method == http.MethodDelete && strings.HasPrefix(r.URL.Path, "/connections/"):
			mu.Lock()
			deleted = append(deleted, strings.TrimPrefix(r.URL.Path, "/connections/"))
			mu.Unlock()
			w.WriteHeader(http.StatusNoContent)
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()
	a := &App{core: coreProcess{listen: strings.TrimPrefix(server.URL, "http://")}}
	a.state.Connection.Status = "connected"
	status := a.GetDesyncUDPStatus()
	if status.LastError != "" || status.DirectFlows != 1 || status.ProfileFlows != 1 || status.Download != 300 || !status.CountersAvailable || status.Counters.FakePackets != 18 || !reflect.DeepEqual(status.ProfileRoutes, []string{"test-node"}) {
		t.Fatalf("wrong route/counter report: %+v", status)
	}
	count, err := a.ReconnectDiscordUDP()
	if err != nil || count != 2 {
		t.Fatalf("UDP reset: %d %v", count, err)
	}
	mu.Lock()
	defer mu.Unlock()
	if !reflect.DeepEqual(deleted, []string{"direct-udp", "profile-udp"}) {
		t.Fatalf("reset affected unrelated TCP/browser traffic: %v", deleted)
	}
}
