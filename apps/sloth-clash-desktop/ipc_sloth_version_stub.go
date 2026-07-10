//go:build !windows && !darwin

package main

import "context"

// Non-Windows/macOS builds have no privileged IPC service.
func ipcSlothServiceVersion(_ context.Context) (string, error) {
	return "", errIPCServiceVersionUnavailable
}
