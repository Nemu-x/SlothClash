//go:build !windows

package procattr

import "syscall"

// HideWindow is nil off Windows: there is no console window to hide.
func HideWindow() *syscall.SysProcAttr {
	return nil
}
