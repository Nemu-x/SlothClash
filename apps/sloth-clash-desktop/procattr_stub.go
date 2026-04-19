//go:build !windows

package main

import "syscall"

func hideWindowSysProcAttr() *syscall.SysProcAttr {
	return nil
}
