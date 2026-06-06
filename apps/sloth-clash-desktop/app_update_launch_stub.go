//go:build !windows

package main

import "errors"

func launchUpdateInstaller(path, params string) error {
	_, _ = path, params
	return errors.New("in-app installer launch is only supported on Windows — open the release page from Settings")
}

func windowsUpdateInstallerArgs() string { return "" }

func scheduleProcessExitForUpdateHandoff() {}
