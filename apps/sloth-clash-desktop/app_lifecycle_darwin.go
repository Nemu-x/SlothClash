//go:build darwin && cgo

package main

/*
void slothOnTerminateRequest(void);
*/
import "C"

import "sync"

var (
	darwinLifecycleMu  sync.Mutex
	darwinLifecycleApp *App
)

func registerDarwinLifecycleApp(a *App) {
	darwinLifecycleMu.Lock()
	darwinLifecycleApp = a
	darwinLifecycleMu.Unlock()
}

func unregisterDarwinLifecycleApp(a *App) {
	darwinLifecycleMu.Lock()
	if darwinLifecycleApp == a {
		darwinLifecycleApp = nil
	}
	darwinLifecycleMu.Unlock()
}

//export slothOnTerminateRequest
func slothOnTerminateRequest() {
	darwinLifecycleMu.Lock()
	app := darwinLifecycleApp
	darwinLifecycleMu.Unlock()
	if app != nil {
		app.MarkQuitIntent()
	}
}
