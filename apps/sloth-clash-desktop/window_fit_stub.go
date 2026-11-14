//go:build !windows

package main

// primaryWorkAreaLogical is only needed on Windows, where Wails centres the
// window without clamping it to the work area.
func primaryWorkAreaLogical() (int, int) {
	return 0, 0
}
