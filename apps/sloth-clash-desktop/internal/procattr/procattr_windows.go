//go:build windows

// Package procattr holds the per-OS process attributes for child processes the
// app spawns (the core, its preflight, service tools).
package procattr

import (
	"syscall"

	"golang.org/x/sys/windows"
)

// HideWindow keeps a spawned child from opening a console window.
func HideWindow() *syscall.SysProcAttr {
	// HideWindow alone is not always enough for console subsystem tools (sc.exe, net.exe, etc.).
	// CREATE_NO_WINDOW avoids allocating a visible console when the child is a console process.
	return &syscall.SysProcAttr{
		HideWindow:    true,
		CreationFlags: windows.CREATE_NO_WINDOW,
	}
}
