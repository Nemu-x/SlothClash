package main

import (
	"io"
	"net/http"
)

// slothIPCMaxBodyBytes caps a service IPC response. Replies are small JSON
// envelopes (start/stop/status/version); the cap only bounds memory if the
// service misbehaves.
const slothIPCMaxBodyBytes = 512 << 10

// readSlothIPCBody reads a service IPC response. A 2xx body is parsed as JSON
// by every caller, so an oversized one is refused instead of truncated; a
// non-2xx body is only quoted in error messages, where truncation is fine.
func readSlothIPCBody(resp *http.Response, method, path string) ([]byte, error) {
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return io.ReadAll(io.LimitReader(resp.Body, slothIPCMaxBodyBytes))
	}
	return readBounded(resp.Body, "IPC "+method+" "+path, slothIPCMaxBodyBytes)
}
