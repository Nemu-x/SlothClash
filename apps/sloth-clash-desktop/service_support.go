package main

// privilegedServiceSupported reports whether this build actually drives the
// mihomo core through the privileged helper service. Windows (named pipe),
// macOS and Linux (unix socket, ipc_sloth_unix.go) do; every other OS compiles
// the ipc_sloth_stub.go no-ops and runs the core in-process, so TUN has no
// privilege path there.
//
// Keep this in sync with the build tags on ipc_sloth_*.go and with
// useServiceCore in core_manager.go — it is the single place the UI-facing
// install/TUN entry points consult before promising a service-backed mode.
func privilegedServiceSupported(goos string) bool {
	switch goos {
	case "windows", "darwin", "linux":
		return true
	default:
		return false
	}
}

// privilegedServiceUnsupportedMessage is what Install service / Enable TUN
// report on builds without a helper path (BSDs and other non-Linux unixes).
// It names the working alternative so the user is not left with a dead button.
const privilegedServiceUnsupportedMessage = "The privileged helper service is not available on this platform: the core runs in-process and TUN mode needs root. Use Proxy mode for now."
