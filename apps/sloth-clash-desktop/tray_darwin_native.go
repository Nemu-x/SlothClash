//go:build darwin && cgo

package main

/*
#cgo CFLAGS: -x objective-c
#cgo LDFLAGS: -framework Cocoa

#include <stdlib.h>

void SlothTrayRegisterMonoPNG(const unsigned char *p, int n);
void SlothTrayConfigureLabels(const char *showWindow, const char *settings, const char *quit, const char *connect);
void SlothTrayStart(void);
void SlothTrayStop(void);
void SlothTraySetConnectTitle(const char *title);
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
	trayNativeMu     sync.Mutex
	trayNativeApp    *App
	trayNativeUp     bool
	trayNativeStopCh chan struct{}
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
	if trayNativeStopCh != nil {
		select {
		case <-trayNativeStopCh:
		default:
			close(trayNativeStopCh)
		}
	}
	trayNativeStopCh = make(chan struct{})
	stopCh := trayNativeStopCh
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

	// Push localized labels into the Objective-C side before the menu is
	// built. The labels live in retained NSStrings until the next call so it
	// is safe to free our C strings immediately after the call returns.
	labels := currentTrayStrings()
	cShow := C.CString(labels.ShowWindow)
	cSettings := C.CString(labels.Settings)
	cQuit := C.CString(labels.Quit)
	cConnect := C.CString(labels.Connect)
	C.SlothTrayConfigureLabels(cShow, cSettings, cQuit, cConnect)
	C.free(unsafe.Pointer(cShow))
	C.free(unsafe.Pointer(cSettings))
	C.free(unsafe.Pointer(cQuit))
	C.free(unsafe.Pointer(cConnect))

	C.SlothTrayStart()
	// Poll the app state on a low cadence and rewrite the Connect/Disconnect
	// menu item title when it actually changed. The native menu is created
	// once with title "Connect"; without this poller the user always saw
	// "Connect" even after the app was already connected.
	go trayConnectTitlePoll(stopCh)
}

func trayConnectTitlePoll(stopCh <-chan struct{}) {
	defer func() {
		if r := recover(); r != nil {
			writeTrayLog(fmt.Sprintf("[tray] title poller panic recovered: %v", r))
		}
	}()
	tick := time.NewTicker(1500 * time.Millisecond)
	defer tick.Stop()
	last := ""
	for {
		select {
		case <-stopCh:
			return
		case <-tick.C:
			trayNativeMu.Lock()
			up := trayNativeUp
			app := trayNativeApp
			trayNativeMu.Unlock()
			if !up || app == nil {
				continue
			}
			st := app.GetAppState()
			labels := currentTrayStrings()
			var desired string
			switch st.Connection.Status {
			case ConnConnected:
				desired = labels.Disconnect
			case ConnConnecting:
				desired = labels.Connecting
			default:
				desired = labels.Connect
			}
			if desired == last {
				continue
			}
			last = desired
			ctitle := C.CString(desired)
			C.SlothTraySetConnectTitle(ctitle)
			C.free(unsafe.Pointer(ctitle))
		}
	}
}

func stopAppTray() {
	trayNativeMu.Lock()
	app := trayNativeApp
	if trayNativeStopCh != nil {
		select {
		case <-trayNativeStopCh:
		default:
			close(trayNativeStopCh)
		}
		trayNativeStopCh = nil
	}
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
