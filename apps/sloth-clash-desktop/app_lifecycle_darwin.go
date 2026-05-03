//go:build darwin && cgo

package main

/*
void slothOnTerminateRequest(void);
*/
import "C"

import (
	"strings"
	"sync"
	"time"

	wailsrt "github.com/wailsapp/wails/v2/pkg/runtime"
)

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

//export slothOnOpenURL
func slothOnOpenURL(raw *C.char) {
	u := strings.TrimSpace(C.GoString(raw))
	if u == "" || !strings.HasPrefix(strings.ToLower(u), "slothclash:") {
		return
	}
	darwinLifecycleMu.Lock()
	app := darwinLifecycleApp
	darwinLifecycleMu.Unlock()
	if app == nil {
		return
	}
	go func() {
		// Keep parity with argv deep-link startup path: frontend listeners may not be ready yet.
		time.Sleep(450 * time.Millisecond)
		app.tryInstallConfigFromArgs([]string{u})
		if app.ctx != nil {
			wailsrt.WindowShow(app.ctx)
			wailsrt.WindowUnminimise(app.ctx)
		}
	}()
}
