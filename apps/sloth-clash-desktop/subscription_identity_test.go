package main

import (
	"net/http"
	"strings"
	"testing"
)

func TestApplySubscriptionIdentityHeaders(t *testing.T) {
	req, err := http.NewRequest(http.MethodGet, "https://example.com/sub", nil)
	if err != nil {
		t.Fatal(err)
	}
	applySubscriptionIdentityHeaders(req)
	if got := req.Header.Get("x-hwid"); len(got) != 64 || !isHex(got) {
		t.Fatalf("x-hwid: want 64 hex chars, got %q", got)
	}
	if req.Header.Get("x-device-os") == "" {
		t.Fatal("x-device-os empty")
	}
	if req.Header.Get("x-ver-os") == "" {
		t.Fatal("x-ver-os empty")
	}
	if req.Header.Get("x-device-model") == "" {
		t.Fatal("x-device-model empty")
	}
	if req.Header.Get("x-app-version") != AppVersion {
		t.Fatalf("x-app-version: got %q want %q", req.Header.Get("x-app-version"), AppVersion)
	}
}

func TestApplySubscriptionIdentityHeadersHwidDisabled(t *testing.T) {
	prefsMu.Lock()
	prev := prefsCurrent.Privacy
	off := false
	prefsCurrent.Privacy.HwidEnabled = &off
	prefsMu.Unlock()
	t.Cleanup(func() {
		prefsMu.Lock()
		prefsCurrent.Privacy = prev
		prefsMu.Unlock()
	})

	req, err := http.NewRequest(http.MethodGet, "https://example.com/sub", nil)
	if err != nil {
		t.Fatal(err)
	}
	applySubscriptionIdentityHeaders(req)
	if got := req.Header.Get("x-hwid"); got != "" {
		t.Fatalf("x-hwid: want empty, got %q", got)
	}
	// Non-identity headers must still be sent so providers can route per-OS.
	if req.Header.Get("x-device-os") == "" {
		t.Fatal("x-device-os should remain present when HWID is disabled")
	}
	if req.Header.Get("x-app-version") != AppVersion {
		t.Fatalf("x-app-version: got %q want %q", req.Header.Get("x-app-version"), AppVersion)
	}
}

func isHex(s string) bool {
	for _, c := range s {
		switch {
		case c >= '0' && c <= '9', c >= 'a' && c <= 'f':
		default:
			return false
		}
	}
	return strings.TrimSpace(s) != ""
}
