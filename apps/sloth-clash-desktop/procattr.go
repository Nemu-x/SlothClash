package main

import (
	"syscall"

	"SlothClashDesktop/internal/procattr"
)

// hideWindowSysProcAttr is procattr.HideWindow: no console window for spawned
// child processes on Windows, nil elsewhere.
func hideWindowSysProcAttr() *syscall.SysProcAttr {
	return procattr.HideWindow()
}
