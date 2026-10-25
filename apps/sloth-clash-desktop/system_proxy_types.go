package main

import "os"

type SystemProxyServiceSnapshot struct {
	WebEnabled    bool
	WebServer     string
	WebPort       int
	SecureEnabled bool
	SecureServer  string
	SecurePort    int
}

// osSystemProxyWritesAllowed gates every write to the OS proxy settings (HKCU
// Internet Settings on Windows, networksetup on macOS). It is always true in
// the shipped app; it is false only inside a `go test` binary, unless
// SLOTH_ALLOW_REAL_SYSTEM_PROXY=1.
//
// Same reasoning as the slothDataRoot guard: the lifecycle tests drive the real
// Connect / stop paths, and stopCoreLocked clears any loopback system proxy it
// finds — which would switch off the developer's own running SlothClash
// session, while a proxy-mode Connect would point the whole machine at a fake
// core's dead mixed-port.
func osSystemProxyWritesAllowed() bool {
	return !runningUnderGoTest() || os.Getenv("SLOTH_ALLOW_REAL_SYSTEM_PROXY") == "1"
}
