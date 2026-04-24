package main

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// Mihomo's /configs accepts PATCH {"tun":{"enable":bool}} as a partial merge.
// We assert the exact wire shape so a regression in the helper doesn't silently
// break Connect/Disconnect.
func TestCoreSetTunEnabled_PatchShape(t *testing.T) {
	var gotMethod string
	var gotPath string
	var gotBody map[string]any
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotMethod = r.Method
		gotPath = r.URL.Path
		b, _ := io.ReadAll(r.Body)
		_ = json.Unmarshal(b, &gotBody)
		w.WriteHeader(http.StatusNoContent)
	}))
	defer srv.Close()

	listen := strings.TrimPrefix(srv.URL, "http://")
	if err := coreSetTunEnabledAt(context.Background(), listen, "", true); err != nil {
		t.Fatalf("coreSetTunEnabledAt: %v", err)
	}

	if gotMethod != http.MethodPatch {
		t.Fatalf("want PATCH, got %s", gotMethod)
	}
	if gotPath != "/configs" {
		t.Fatalf("want /configs, got %s", gotPath)
	}
	tun, _ := gotBody["tun"].(map[string]any)
	if tun == nil {
		t.Fatalf("want tun object in body, got %v", gotBody)
	}
	if v, _ := tun["enable"].(bool); !v {
		t.Fatalf("want tun.enable=true, got %v", tun)
	}

	if err := coreSetTunEnabledAt(context.Background(), listen, "", false); err != nil {
		t.Fatalf("coreSetTunEnabledAt(false): %v", err)
	}
	tun, _ = gotBody["tun"].(map[string]any)
	if v, ok := tun["enable"].(bool); !ok || v {
		t.Fatalf("want tun.enable=false, got %v", tun)
	}
}

func TestCoreSetTunEnabled_PropagatesHTTPError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusBadRequest)
		_, _ = w.Write([]byte(`{"message":"bad config"}`))
	}))
	defer srv.Close()

	listen := strings.TrimPrefix(srv.URL, "http://")
	err := coreSetTunEnabledAt(context.Background(), listen, "", true)
	if err == nil {
		t.Fatal("expected error on 400, got nil")
	}
	if !strings.Contains(err.Error(), "400") || !strings.Contains(err.Error(), "bad config") {
		t.Fatalf("error should include status + body, got %v", err)
	}
}

// PUT /configs?path=...&force=true is Verge's "hot reload everything" endpoint.
// Body is empty JSON — Mihomo ignores it but some versions reject non-empty.
func TestCoreReloadConfigFile_PutShape(t *testing.T) {
	var gotMethod string
	var gotPath string
	var gotQuery string
	var gotBody string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotMethod = r.Method
		gotPath = r.URL.Path
		gotQuery = r.URL.RawQuery
		b, _ := io.ReadAll(r.Body)
		gotBody = string(b)
		w.WriteHeader(http.StatusNoContent)
	}))
	defer srv.Close()

	listen := strings.TrimPrefix(srv.URL, "http://")
	if err := coreReloadConfigFileAt(context.Background(), listen, "", `C:\some\profile\config.yaml`); err != nil {
		t.Fatalf("coreReloadConfigFileAt: %v", err)
	}
	if gotMethod != http.MethodPut {
		t.Fatalf("want PUT, got %s", gotMethod)
	}
	if gotPath != "/configs" {
		t.Fatalf("want /configs, got %s", gotPath)
	}
	if !strings.Contains(gotQuery, "force=true") {
		t.Fatalf("want force=true in query, got %q", gotQuery)
	}
	if !strings.Contains(gotQuery, "path=") {
		t.Fatalf("want path=... in query, got %q", gotQuery)
	}
	if !strings.Contains(gotBody, "{}") {
		t.Fatalf("want empty json body, got %q", gotBody)
	}
}

func TestCoreReloadConfigFile_RequiresPath(t *testing.T) {
	err := coreReloadConfigFileAt(context.Background(), "127.0.0.1:1", "", "")
	if err == nil {
		t.Fatal("expected error on empty path")
	}
	if !strings.Contains(err.Error(), "path is required") {
		t.Fatalf("want 'path is required', got %v", err)
	}
}

// The reload model puts the bearer secret on every /configs request against
// an http endpoint (named-pipe / unix-socket paths are already trust-local).
// A missing Authorization header would make Mihomo reject the PATCH with 401
// and break Connect in the "HTTP controller" deployment we use for dev builds.
func TestCoreSetTunEnabled_SendsBearerAuth(t *testing.T) {
	var gotAuth string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotAuth = r.Header.Get("Authorization")
		w.WriteHeader(http.StatusNoContent)
	}))
	defer srv.Close()

	listen := strings.TrimPrefix(srv.URL, "http://")
	if err := coreSetTunEnabledAt(context.Background(), listen, "top-secret", true); err != nil {
		t.Fatalf("coreSetTunEnabledAt: %v", err)
	}
	if gotAuth != "Bearer top-secret" {
		t.Fatalf("want Bearer top-secret, got %q", gotAuth)
	}
}

func TestCoreSetTunEnabled_RequiresEndpoint(t *testing.T) {
	if err := coreSetTunEnabledAt(context.Background(), "", "", true); err == nil {
		t.Fatal("expected error on empty listen")
	}
}

func TestCoreSetMode_PatchShape(t *testing.T) {
	var gotBody map[string]any
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		b, _ := io.ReadAll(r.Body)
		_ = json.Unmarshal(b, &gotBody)
		w.WriteHeader(http.StatusNoContent)
	}))
	defer srv.Close()
	listen := strings.TrimPrefix(srv.URL, "http://")

	if err := coreSetModeAt(context.Background(), listen, "", "rule"); err != nil {
		t.Fatalf("mode rule: %v", err)
	}
	if gotBody["mode"] != "rule" {
		t.Fatalf("want mode=rule, got %v", gotBody)
	}

	if err := coreSetModeAt(context.Background(), listen, "", "UnKnOwN"); err == nil {
		t.Fatal("expected error for invalid mode")
	}
}
