//go:build windows

package main

import (
	"fmt"
	"path/filepath"
	"syscall"
	"unsafe"
)

var (
	shell32Update           = syscall.NewLazyDLL("shell32.dll")
	procShellExecuteWUpdate = shell32Update.NewProc("ShellExecuteW")
)

// launchUpdateInstaller starts the verified NSIS installer with UAC elevation.
// exec.Command (CreateProcess) cannot trigger elevation; ShellExecute with
// verb "runas" shows the standard Windows consent prompt.
func launchUpdateInstaller(path string) error {
	verb, err := syscall.UTF16PtrFromString("runas")
	if err != nil {
		return fmt.Errorf("prepare elevation verb: %w", err)
	}
	exe, err := syscall.UTF16PtrFromString(path)
	if err != nil {
		return fmt.Errorf("prepare installer path: %w", err)
	}
	dir, err := syscall.UTF16PtrFromString(filepath.Dir(path))
	if err != nil {
		return fmt.Errorf("prepare installer directory: %w", err)
	}

	// SW_SHOWNORMAL — the installer UI should be visible after UAC.
	const showNormal = 1
	ret, _, callErr := procShellExecuteWUpdate.Call(
		0,
		uintptr(unsafe.Pointer(verb)),
		uintptr(unsafe.Pointer(exe)),
		0,
		uintptr(unsafe.Pointer(dir)),
		showNormal,
	)
	if ret <= 32 {
		if msg := shellExecuteFailureMessage(ret); msg != "" {
			return fmt.Errorf("%s", msg)
		}
		if callErr != nil && callErr != syscall.Errno(0) {
			return fmt.Errorf("could not start installer: %w", callErr)
		}
		return fmt.Errorf("could not start installer (ShellExecute code %d)", ret)
	}
	return nil
}

func shellExecuteFailureMessage(code uintptr) string {
	switch code {
	case 2:
		return "installer file not found"
	case 3:
		return "installer path not found"
	case 5:
		return "administrator permission required — accept the UAC prompt to install the update"
	case 8:
		return "not enough memory to start the installer"
	case 32:
		return "could not start the installer (missing dependency)"
	default:
		return ""
	}
}
