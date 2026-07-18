package main

import (
	"errors"
	"testing"
)

func TestIsCorePinMismatchError(t *testing.T) {
	cases := []struct {
		name string
		err  error
		want bool
	}{
		{"nil", nil, false},
		{
			"real service message",
			errors.New("POST /clash/start: HTTP 503 — Failed to start core: core binary SHA-256 98986b57 does not match any pinned hash (\\\\?\\C:\\...\\sloth-mihomo.exe)"),
			true,
		},
		{"short form", errors.New("start core via service: pinned hash mismatch"), true},
		{"unrelated connect error", errors.New("POST /clash/start: HTTP 500 — internal error"), false},
		{"network error", errors.New("dial tcp: connection refused"), false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := isCorePinMismatchError(tc.err); got != tc.want {
				t.Fatalf("isCorePinMismatchError(%v) = %v, want %v", tc.err, got, tc.want)
			}
		})
	}
}
