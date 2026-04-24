//go:build !windows

package main

import "errors"

func setLaunchOnStartup(enabled bool) error {
	_ = enabled
	return errors.New("launch on startup is only supported on Windows")
}

func getLaunchOnStartup() (bool, error) {
	return false, nil
}
