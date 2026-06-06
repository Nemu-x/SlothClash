//go:build !windows

package main

import "errors"

func launchUpdateInstaller(path string) error {
	_ = path
	return errors.New("in-app installer launch is only supported on Windows — open the release page from Settings")
}
