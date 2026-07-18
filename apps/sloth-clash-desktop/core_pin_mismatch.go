package main

import "strings"

// The privileged service content-verifies the core it is asked to spawn against
// the SHA-256(s) pinned in its own environment (SLOTH_CLASH_CORE_SHA256, written
// by the elevated installer). After a core bump on app upgrade the embedded core
// hash changes, but an already-installed service keeps the old pin, so
// POST /clash/start fails with an error like:
//
//	"core binary SHA-256 <hex> does not match any pinned hash (<path>)"
//
// This is not a config or network fault — the fix is to re-pin, i.e. reinstall
// the service (the installer records the fresh hashes). We detect the condition
// so the UI can surface an actionable "reinstall service" banner instead of a
// raw 503 the user cannot act on. Cross-platform: the same pin mechanism (and
// message) is used on Windows / macOS / Linux service installs.
func isCorePinMismatchError(err error) bool {
	if err == nil {
		return false
	}
	msg := strings.ToLower(err.Error())
	return strings.Contains(msg, "pinned hash") ||
		strings.Contains(msg, "does not match any pinned")
}

// isServiceUnreachableError reports whether a connect failed because the
// privileged service, though (supposedly) installed, could not be reached even
// after the client tried to start it — e.g. its temp-dir binary was reaped, or
// it is stopped/crashed after an upgrade. The remedy is the same as for a stale
// pin: reinstall the service (re-extracts a fresh binary and re-registers it),
// so both conditions drive the same "reinstall service" banner. Matches only
// the terminal, post-start-attempt errors (not a transient mid-startup dial)
// so we do not nag on a blip that the next connect resolves.
func isServiceUnreachableError(err error) bool {
	if err == nil {
		return false
	}
	msg := strings.ToLower(err.Error())
	return strings.Contains(msg, "pipe still unreachable after attempting to start") ||
		strings.Contains(msg, "pipe not reachable") ||
		strings.Contains(msg, "is `sloth_clash_service` installed and running")
}
