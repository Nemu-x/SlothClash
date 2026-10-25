package main

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"SlothClashDesktop/internal/boundedio"
)

// padJSON right-pads a JSON document with whitespace to exactly size bytes.
func padJSON(t *testing.T, body []byte, size int) []byte {
	t.Helper()
	if len(body) > size {
		t.Fatalf("fixture of %d bytes cannot be padded to %d", len(body), size)
	}
	return append(append([]byte(nil), body...), bytes.Repeat([]byte(" "), size-len(body))...)
}

func coreJSONServer(t *testing.T, routes map[string][]byte) string {
	t.Helper()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, ok := routes[r.URL.Path]
		if !ok {
			http.NotFound(w, r)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write(body)
	}))
	t.Cleanup(server.Close)
	return strings.TrimPrefix(server.URL, "http://")
}

func TestReadCoreJSONRefusesInsteadOfTruncating(t *testing.T) {
	const limit = 1024
	doc := []byte(`{"ok":true}`)
	for _, size := range []int{len(doc), limit - 1, limit} {
		got, err := readBounded(bytes.NewReader(padJSON(t, doc, size)), "GET /x", limit)
		var v struct{ OK bool }
		if err != nil || len(got) != size || json.Unmarshal(got, &v) != nil || !v.OK {
			t.Fatalf("size %d: %d bytes, %v", size, len(got), err)
		}
	}
	_, err := readBounded(bytes.NewReader(padJSON(t, doc, limit+1)), "GET /x", limit)
	if err == nil || err.Error() != "GET /x response exceeds the 1 KiB limit" {
		t.Fatalf("oversized body: %v", err)
	}
	var v any
	if err := decodeBoundedJSON(strings.NewReader(`{"a":`), "GET /x", limit, &v); err == nil {
		t.Fatal("accepted incomplete JSON")
	}
}

func TestCoreJSONLimits(t *testing.T) {
	for route, want := range map[string]int64{
		"/connections":         coreConnectionsMaxJSONBytes,
		"/connections/":        coreConnectionsMaxJSONBytes,
		"/proxies":             coreProxiesMaxJSONBytes,
		"/proxies?x=1":         coreProxiesMaxJSONBytes,
		"/proxies/GLOBAL":      coreDefaultMaxJSONBytes,
		"/connections/abc-123": coreDefaultMaxJSONBytes,
		"/version":             coreDefaultMaxJSONBytes,
	} {
		if got := coreJSONLimitForRoute(route); got != want {
			t.Errorf("%s: limit %d, want %d", route, got, want)
		}
	}
	for limit, want := range map[int64]string{32 << 20: "32 MiB", 64 << 10: "64 KiB", 1500: "1500 bytes"} {
		if got := boundedio.FormatLimit(limit); got != want {
			t.Errorf("boundedio.FormatLimit(%d) = %q, want %q", limit, got, want)
		}
	}
}

// The caps must cover the sizes their comment promises.
func TestCoreJSONLimitsFitRealisticPayloads(t *testing.T) {
	if n := len(realisticMihomoConnectionsBody(20000)); n > coreConnectionsMaxJSONBytes {
		t.Fatalf("20k connections = %d bytes, over the %d cap", n, coreConnectionsMaxJSONBytes)
	}
	if n := len(realisticMihomoProxiesBody(10000)); n > coreProxiesMaxJSONBytes {
		t.Fatalf("10k proxies = %d bytes, over the %d cap", n, coreProxiesMaxJSONBytes)
	}
	members := make([]string, 50000)
	for i := range members {
		members[i] = realisticMihomoProxyName(i)
	}
	group, _ := json.Marshal(realisticMihomoGroup("🚀 Proxy", "Selector", members))
	if len(group) > coreDefaultMaxJSONBytes {
		t.Fatalf("a 50k-member group detail = %d bytes, over the %d cap", len(group), coreDefaultMaxJSONBytes)
	}
}

func TestConnectionsOverviewParsesLargeCoreResponse(t *testing.T) {
	body := realisticMihomoConnectionsBody(20000)
	if len(body) <= 1<<20 {
		t.Fatal("fixture must exceed the former 1 MiB truncation limit")
	}
	t.Setenv("SLOTH_CLASH_CONTROLLER", coreJSONServer(t, map[string][]byte{"/connections": body}))
	t.Setenv("SLOTH_CLASH_SECRET", "")
	out := (&App{}).FetchConnectionsOverview()
	if !out.Reachable || out.LastError != "" || len(out.Connections) != 500 || out.UploadTotal != 9876543210 {
		t.Fatalf("large /connections: reachable=%t items=%d error=%q", out.Reachable, len(out.Connections), out.LastError)
	}
	if c := out.Connections[0]; c.Metadata.Host == "" || c.Metadata.Process != "chrome.exe" || len(c.Chains) != 2 {
		t.Fatalf("connection fields lost: %+v", c)
	}
}

func TestConnectionsOverviewAtAndOverLimit(t *testing.T) {
	doc := realisticMihomoConnectionsBody(3)
	t.Setenv("SLOTH_CLASH_SECRET", "")
	t.Setenv("SLOTH_CLASH_CONTROLLER", coreJSONServer(t, map[string][]byte{"/connections": padJSON(t, doc, coreConnectionsMaxJSONBytes)}))
	if out := (&App{}).FetchConnectionsOverview(); !out.Reachable || out.LastError != "" || len(out.Connections) != 3 {
		t.Fatalf("body exactly at the cap: reachable=%t items=%d error=%q", out.Reachable, len(out.Connections), out.LastError)
	}
	t.Setenv("SLOTH_CLASH_CONTROLLER", coreJSONServer(t, map[string][]byte{"/connections": padJSON(t, doc, coreConnectionsMaxJSONBytes+1)}))
	out := (&App{}).FetchConnectionsOverview()
	if out.Reachable || out.LastError != "GET /connections response exceeds the 32 MiB limit" {
		t.Fatalf("oversized /connections must surface an explicit error: reachable=%t error=%q", out.Reachable, out.LastError)
	}
}

func TestPullProxyGroupsParsesLargeCoreResponse(t *testing.T) {
	body := realisticMihomoProxiesBody(10000)
	if len(body) <= 4<<20 {
		t.Fatal("fixture must exceed the former 4 MiB truncation limit")
	}
	groups, err := pullProxyGroupsFromCore(coreJSONServer(t, map[string][]byte{"/proxies": body}), "")
	if err != nil {
		t.Fatal(err)
	}
	byName := map[string]ProxyGroup{}
	for _, g := range groups {
		byName[g.Name] = g
	}
	if g := byName["⚡ Auto"]; len(g.Proxies) != 10000 || g.Selected != realisticMihomoProxyName(0) {
		t.Fatalf("groups=%d auto members=%d selected=%q", len(groups), len(g.Proxies), g.Selected)
	}
}

func TestPullProxyGroupsAtAndOverLimit(t *testing.T) {
	doc := realisticMihomoProxiesBody(2)
	groups, err := pullProxyGroupsFromCore(coreJSONServer(t, map[string][]byte{"/proxies": padJSON(t, doc, coreProxiesMaxJSONBytes)}), "")
	if err != nil || len(groups) == 0 {
		t.Fatalf("body exactly at the cap: %d groups, %v", len(groups), err)
	}
	_, err = pullProxyGroupsFromCore(coreJSONServer(t, map[string][]byte{"/proxies": padJSON(t, doc, coreProxiesMaxJSONBytes+1)}), "")
	if err == nil || err.Error() != "GET /proxies response exceeds the 48 MiB limit" {
		t.Fatalf("oversized /proxies: %v", err)
	}
}

// Desync polls /connections through autoCoreJSON; it shares the per-route caps.
func TestAutoCoreJSONUsesRouteLimits(t *testing.T) {
	body := realisticMihomoConnectionsBody(8000)
	if len(body) <= 4<<20 {
		t.Fatal("fixture must exceed the former 4 MiB truncation limit")
	}
	endpoint := coreJSONServer(t, map[string][]byte{
		"/connections":   body,
		"/proxies/group": padJSON(t, []byte(`{"now":"a"}`), coreDefaultMaxJSONBytes+1),
	})
	var overview struct {
		Connections []json.RawMessage `json:"connections"`
	}
	if err := autoCoreJSON(context.Background(), endpoint, "", http.MethodGet, "/connections", nil, &overview); err != nil || len(overview.Connections) != 8000 {
		t.Fatalf("large /connections: %d items, %v", len(overview.Connections), err)
	}
	var group struct{ Now string }
	err := autoCoreJSON(context.Background(), endpoint, "", http.MethodGet, "/proxies/group", nil, &group)
	if err == nil || err.Error() != "GET /proxies/group response exceeds the 4 MiB limit" {
		t.Fatalf("oversized group detail: %v", err)
	}
}

func TestCoreGetJSONRefusesOversizedBody(t *testing.T) {
	a := &App{core: coreProcess{listen: coreJSONServer(t, map[string][]byte{"/version": padJSON(t, []byte(`{"version":"v1"}`), coreDefaultMaxJSONBytes+1)})}}
	var v struct{ Version string }
	if err := a.coreGetJSON(context.Background(), "/version", &v); err == nil || err.Error() != "GET /version response exceeds the 4 MiB limit" {
		t.Fatalf("oversized /version: %v", err)
	}
}
