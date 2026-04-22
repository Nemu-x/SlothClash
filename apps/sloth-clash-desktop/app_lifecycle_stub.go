//go:build !darwin || !cgo

package main

func registerDarwinLifecycleApp(a *App)   { _ = a }
func unregisterDarwinLifecycleApp(a *App) { _ = a }
