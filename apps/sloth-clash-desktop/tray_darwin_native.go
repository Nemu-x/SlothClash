//go:build darwin && cgo

package main

/*
#cgo CFLAGS: -x objective-c
#cgo LDFLAGS: -framework Cocoa

void SlothTrayRegisterMonoPNG(const unsigned char *p, int n);
void SlothTrayStart(void);
void SlothTrayStop(void);
void slothTrayDispatch(int op);
*/
import "C"

import (
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"sync"
	"time"
	"unsafe"

	wailsrt "github.com/wailsapp/wails/v2/pkg/runtime"
)

var (
	trayNativeMu  sync.Mutex
	trayNativeApp *App
	trayNativeUp  bool
)

func writeTrayLog(msg string) {
	root, err := slothDataRoot()
	if err != nil {
		return
	}
	_ = os.MkdirAll(root, 0o755)
	p := filepath.Join(root, "tray.log")
	f, err := os.OpenFile(p, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o644)
	if err != nil {
		return
	}
	defer f.Close()
	_, _ = f.WriteString(fmt.Sprintf("%s %s\n", time.Now().Format(time.RFC3339), msg))
}

func startAppTray(a *App) {
	trayNativeMu.Lock()
	trayNativeApp = a
	trayNativeUp = false
	trayNativeMu.Unlock()
	if a != nil && a.ctx != nil {
		wailsrt.LogInfo(a.ctx, "[tray] start requested")
	}
	writeTrayLog("[tray] start requested")
	if len(darwinTrayMonoPNG) > 0 {
		C.SlothTrayRegisterMonoPNG(
			(*C.uchar)(unsafe.Pointer(&darwinTrayMonoPNG[0])),
			C.int(len(darwinTrayMonoPNG)),
		)
		runtime.KeepAlive(darwinTrayMonoPNG)
	} else {
		writeTrayLog("[tray] warn: trayicons/mono.png missing at compile time (embed empty)")
	}
	C.SlothTrayStart()
}

func stopAppTray() {
	trayNativeMu.Lock()
	app := trayNativeApp
	trayNativeMu.Unlock()
	if app != nil && app.ctx != nil {
		wailsrt.LogInfo(app.ctx, "[tray] stop requested")
	}
	writeTrayLog("[tray] stop requested")
	C.SlothTrayStop()
	trayNativeMu.Lock()
	trayNativeUp = false
	trayNativeApp = nil
	trayNativeMu.Unlock()
}

func trayBackendAvailable() bool { return true }
func trayIsReady() bool {
	trayNativeMu.Lock()
	defer trayNativeMu.Unlock()
	return trayNativeUp
}

//export slothTrayDispatch
func slothTrayDispatch(op C.int) {
	trayNativeMu.Lock()
	app := trayNativeApp
	trayNativeMu.Unlock()
	if app == nil {
		return
	}
	switch int(op) {
	case 1:
		app.NavigateUIScreen("home")
	case 2:
		app.NavigateUIScreen("profiles")
	case 3:
		app.NavigateUIScreen("proxies")
	case 4:
		app.NavigateUIScreen("rules")
	case 5:
		app.NavigateUIScreen("advanced")
	case 6:
		app.NavigateUIScreen("settings")
	case 10:
		_, _ = app.SetMode("rule")
	case 11:
		_, _ = app.SetMode("global")
	case 12:
		_, _ = app.SetMode("direct")
	case 20:
		_, _ = app.SetTrafficMode("proxy")
	case 21:
		_, _ = app.SetTrafficMode("tun")
	default:
		return
	}
}

//export slothTrayOnReady
func slothTrayOnReady() {
	trayNativeMu.Lock()
	app := trayNativeApp
	trayNativeUp = true
	trayNativeMu.Unlock()
	if app != nil && app.ctx != nil {
		wailsrt.LogInfo(app.ctx, "[tray] ready")
	}
	writeTrayLog("[tray] ready")
}

//export slothTrayOnStopped
func slothTrayOnStopped() {
	trayNativeMu.Lock()
	app := trayNativeApp
	trayNativeUp = false
	trayNativeMu.Unlock()
	if app != nil && app.ctx != nil {
		wailsrt.LogInfo(app.ctx, "[tray] stopped")
	}
	writeTrayLog("[tray] stopped")
}

//export slothTrayOnShow
func slothTrayOnShow() {
	trayNativeMu.Lock()
	app := trayNativeApp
	trayNativeMu.Unlock()
	if app == nil || app.ctx == nil {
		return
	}
	wailsrt.WindowShow(app.ctx)
	wailsrt.WindowUnminimise(app.ctx)
}

//export slothTrayOnHide
func slothTrayOnHide() {
	trayNativeMu.Lock()
	app := trayNativeApp
	trayNativeMu.Unlock()
	if app == nil || app.ctx == nil {
		return
	}
	wailsrt.WindowHide(app.ctx)
}

//export slothTrayOnToggleConnect
func slothTrayOnToggleConnect() {
	trayNativeMu.Lock()
	app := trayNativeApp
	trayNativeMu.Unlock()
	if app == nil {
		return
	}
	st := app.GetAppState()
	if st.Connection.Status == "connected" {
		app.Disconnect()
	} else {
		_, _ = app.Connect()
	}
}

//export slothTrayOnQuit
func slothTrayOnQuit() {
	trayNativeMu.Lock()
	app := trayNativeApp
	trayNativeMu.Unlock()
	if app == nil || app.ctx == nil {
		return
	}
	app.MarkQuitIntent()
	wailsrt.Quit(app.ctx)
}
