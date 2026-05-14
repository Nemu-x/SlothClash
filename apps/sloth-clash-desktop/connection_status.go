package main

// ConnStatus is the canonical set of values for ConnectionState.Status. It's
// a string type so existing JSON marshaling and frontend boundary stay
// unchanged — the gain is purely compile-time: typos like "connceted" stop
// silently shipping through to runtime.
//
// Callers should compare against the constants below rather than against
// bare string literals. The package keeps the type alias loose enough that
// upstream slices/maps that still hold raw strings interoperate without
// explicit conversion at every site.
type ConnStatus = string

const (
	// ConnDisconnected — no core (or core running but user did not request a
	// connection). UI shows the Connect button.
	ConnDisconnected ConnStatus = "disconnected"
	// ConnConnecting — async connect job is running. Idempotent: a second
	// Connect() while in this state is a no-op (Verge-style UX).
	ConnConnecting ConnStatus = "connecting"
	// ConnConnected — core is running, last reload was applied, post-connect
	// warmup completed.
	ConnConnected ConnStatus = "connected"
	// ConnReconnecting — core died unexpectedly; auto-restart loop is
	// rebuilding it (see core_autorestart.go). UI should show "Reconnecting…"
	// rather than the scary "error" banner.
	ConnReconnecting ConnStatus = "reconnecting"
	// ConnError — core failed and either auto-restart was not attempted or
	// it exhausted its backoffs. UI shows LastError + a Retry call to action.
	ConnError ConnStatus = "error"
)

// IsConnStatusActive reports whether the given status represents an
// in-progress or successful connection. Useful for deciding whether to
// suppress system-proxy reconciles, IP probes, etc. while we're in flight.
func IsConnStatusActive(s string) bool {
	switch s {
	case ConnConnecting, ConnConnected, ConnReconnecting:
		return true
	}
	return false
}
