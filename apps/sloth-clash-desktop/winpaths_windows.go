//go:build windows

package main

import (
	"os"
	"path/filepath"
)

// system32Exe returns an absolute path to an executable under System32 (e.g. sc.exe, net.exe).
// Using short names like "sc" relies on PATH and can behave inconsistently; full paths avoid extra shells.
func system32Exe(file string) string {
	root := os.Getenv("SystemRoot")
	if root == "" {
		root = `C:\Windows`
	}
	return filepath.Join(root, "System32", file)
}
