//go:build !linux

package main

import "errors"

// Non-Linux builds never reach these; they exist so the shared platform
// switches in app.go compile everywhere.
func linuxServiceStagingDir() (string, error) {
	return "", errors.New("linux service staging is not available on this platform")
}

func installServiceElevatedLinux(_, _, _ string) ([]byte, error) {
	return nil, errors.New("linux service install is not available on this platform")
}

func queryLinuxServiceStatus() (installed bool, running bool, lastErr string) {
	return false, false, ""
}
