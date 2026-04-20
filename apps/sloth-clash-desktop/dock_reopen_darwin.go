//go:build darwin && cgo

package main

/*
#cgo CFLAGS: -x objective-c
#cgo LDFLAGS: -framework Cocoa

void sloth_link_dock_reopen(void);
*/
import "C"

// installDockReopenHook registers Dock-click restore after Wails has loaded AppDelegate.
func installDockReopenHook() {
	C.sloth_link_dock_reopen()
}
