//go:build !windows

package main

func system32Exe(file string) string {
	return file
}
