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
