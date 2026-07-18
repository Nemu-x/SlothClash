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
		{
			"unreachable is not a pin mismatch",
			errors.New("sloth IPC pipe still unreachable after attempting to start `sloth_clash_service`: ... cannot find the file specified"),
			false,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := isCorePinMismatchError(tc.err); got != tc.want {
				t.Fatalf("isCorePinMismatchError(%v) = %v, want %v", tc.err, got, tc.want)
			}
		})
	}
}

func TestIsServiceUnreachableError(t *testing.T) {
	cases := []struct {
		name string
		err  error
		want bool
	}{
		{"nil", nil, false},
		{
			"pipe unreachable after start (the real error)",
			errors.New("sloth IPC pipe still unreachable after attempting to start `sloth_clash_service`: open \\\\.\\pipe\\sloth-clash-service: The system cannot find the file specified. (original dial: ...)"),
			true,
		},
		{
			"sc query failed / not running",
			errors.New("sloth IPC service pipe not reachable (...); is `sloth_clash_service` installed and running? (sc query: ...)"),
			true,
		},
		{"pin mismatch is not unreachable", errors.New("does not match any pinned hash"), false},
		{"generic 500", errors.New("POST /clash/start: HTTP 500 — internal error"), false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := isServiceUnreachableError(tc.err); got != tc.want {
				t.Fatalf("isServiceUnreachableError(%v) = %v, want %v", tc.err, got, tc.want)
			}
		})
	}
}
