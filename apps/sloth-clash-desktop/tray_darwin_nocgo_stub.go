//go:build darwin && !cgo

package main

func startAppTray(a *App) {
	_ = a
}

func stopAppTray() {}

func trayBackendAvailable() bool { return false }
func trayIsReady() bool { return false }
