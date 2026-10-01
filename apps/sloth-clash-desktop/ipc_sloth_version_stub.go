//go:build !windows && !darwin && !linux

package main

import "context"

// Builds without a privileged IPC transport (not Windows/macOS/Linux).
func ipcSlothServiceVersion(_ context.Context) (string, error) {
	return "", errIPCServiceVersionUnavailable
}
