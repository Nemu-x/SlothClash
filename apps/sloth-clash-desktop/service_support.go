package main

// privilegedServiceSupported reports whether this build actually drives the
// mihomo core through the privileged helper service. Windows (named pipe) and
// macOS (unix socket) do; every other OS compiles the ipc_sloth_stub.go no-ops
// and runs the core in-process, so TUN has no privilege path there yet.
//
// Keep this in sync with the build tags on ipc_sloth_*.go and with
// useServiceCore in core_manager.go — it is the single place the UI-facing
// install/TUN entry points consult before promising a service-backed mode.
func privilegedServiceSupported(goos string) bool {
	switch goos {
	case "windows", "darwin":
		return true
	default:
		return false
	}
}

// privilegedServiceUnsupportedMessage is what Install service / Enable TUN
// report on builds without a helper path. It names the working alternative so
// the user is not left with a dead button (issue #73: on Linux the installer
// used to run unprivileged, provoke a polkit prompt from systemctl and then
// fail on /usr/local/lib with a permission error).
const privilegedServiceUnsupportedMessage = "The privileged helper service is not wired on this platform yet: the Linux build runs the core in-process and TUN mode needs root. Use Proxy mode (system proxy) for now — privileged TUN on Linux is a planned change."
