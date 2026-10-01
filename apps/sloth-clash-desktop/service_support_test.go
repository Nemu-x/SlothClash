package main

import (
	"runtime"
	"strings"
	"testing"
)

// The helper is only reachable on the platforms that compile a real IPC
// transport; anything else must be reported as unsupported so the UI never
// launches an installer for a service the app would not use.
func TestPrivilegedServiceSupportedMatchesIPCBuildTags(t *testing.T) {
	cases := map[string]bool{
		"windows": true,
		"darwin":  true,
		"linux":   true,
		"freebsd": false,
		"openbsd": false,
		"":        false,
	}
	for goos, want := range cases {
		if got := privilegedServiceSupported(goos); got != want {
			t.Fatalf("privilegedServiceSupported(%q) = %v, want %v", goos, got, want)
		}
	}
}

// On a supported platform the current build must agree with itself: the
// compiled-in IPC transport (ipc_sloth_windows.go / ipc_sloth_unix.go) is
// exactly what privilegedServiceSupported promises for runtime.GOOS.
func TestPrivilegedServiceSupportedForCurrentOS(t *testing.T) {
	want := runtime.GOOS == "windows" || runtime.GOOS == "darwin" || runtime.GOOS == "linux"
	if got := privilegedServiceSupported(runtime.GOOS); got != want {
		t.Fatalf("privilegedServiceSupported(%q) = %v, want %v", runtime.GOOS, got, want)
	}
}

// The unsupported message must point the user at the mode that does work and
// must not tell them to install anything.
func TestPrivilegedServiceUnsupportedMessageNamesProxyMode(t *testing.T) {
	msg := privilegedServiceUnsupportedMessage
	if !strings.Contains(msg, "Proxy mode") {
		t.Fatalf("message must name Proxy mode as the working alternative: %q", msg)
	}
	if strings.Contains(strings.ToLower(msg), "install service") {
		t.Fatalf("message must not tell the user to install the service: %q", msg)
	}
}
