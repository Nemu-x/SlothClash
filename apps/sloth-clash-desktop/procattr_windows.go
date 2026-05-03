//go:build windows

package main

import (
	"syscall"

	"golang.org/x/sys/windows"
)

func hideWindowSysProcAttr() *syscall.SysProcAttr {
	// HideWindow alone is not always enough for console subsystem tools (sc.exe, net.exe, etc.).
	// CREATE_NO_WINDOW avoids allocating a visible console when the child is a console process.
	return &syscall.SysProcAttr{
		HideWindow:    true,
		CreationFlags: windows.CREATE_NO_WINDOW,
	}
}
