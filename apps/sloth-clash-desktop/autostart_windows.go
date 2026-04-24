//go:build windows

package main

import (
	"errors"
	"os"
	"strings"

	"golang.org/x/sys/windows/registry"
)

// windowsAutostartKey lives in HKCU so we do not need elevated privileges
// and the entry only launches for the current user. This mirrors how
// Clash Verge Rev and most well-behaved desktop apps register autostart
// on Windows.
const windowsAutostartKey = `Software\Microsoft\Windows\CurrentVersion\Run`
const windowsAutostartName = "SlothClash"

// setLaunchOnStartup writes or clears the Run key entry that launches the
// currently running executable at sign-in. When enabled the value includes
// the --minimized flag so the app starts hidden and lives in the tray
// until the user clicks Show Window (matching Start Minimized UX).
func setLaunchOnStartup(enabled bool) error {
	exe, err := os.Executable()
	if err != nil {
		return err
	}
	exe = strings.TrimSpace(exe)
	if exe == "" {
		return errors.New("cannot resolve executable path")
	}
	k, err := registry.OpenKey(registry.CURRENT_USER, windowsAutostartKey, registry.SET_VALUE)
	if err != nil {
		return err
	}
	defer k.Close()
	if !enabled {
		err = k.DeleteValue(windowsAutostartName)
		if err != nil && !errors.Is(err, registry.ErrNotExist) {
			return err
		}
		return nil
	}
	value := `"` + exe + `" --minimized`
	return k.SetStringValue(windowsAutostartName, value)
}

// getLaunchOnStartup reports whether the Run key points at the currently
// running binary. A stale pointer (e.g. after an app move) is reported as
// disabled so the next toggle rewrites the correct path.
func getLaunchOnStartup() (bool, error) {
	k, err := registry.OpenKey(registry.CURRENT_USER, windowsAutostartKey, registry.QUERY_VALUE)
	if err != nil {
		return false, err
	}
	defer k.Close()
	val, _, err := k.GetStringValue(windowsAutostartName)
	if err != nil {
		if errors.Is(err, registry.ErrNotExist) {
			return false, nil
		}
		return false, err
	}
	exe, err := os.Executable()
	if err != nil {
		return false, err
	}
	return strings.Contains(strings.ToLower(val), strings.ToLower(exe)), nil
}
