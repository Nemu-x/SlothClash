//go:build darwin && slothtray

package main

import (
	"sync"
	"time"

	"github.com/getlantern/systray"
	wailsrt "github.com/wailsapp/wails/v2/pkg/runtime"
)

var (
	trayMu      sync.Mutex
	trayRunning bool
	trayApp     *App
)

func startAppTray(a *App) {
	trayMu.Lock()
	if trayRunning || a == nil {
		trayMu.Unlock()
		return
	}
	trayRunning = true
	trayApp = a
	trayMu.Unlock()

	go systray.Run(func() {
		systray.SetTitle("Sloth Clash")
		systray.SetTooltip("Sloth Clash")
		if b, err := a.bundle.ReadFile("build/appicon.png"); err == nil && len(b) > 0 {
			systray.SetTemplateIcon(b, b)
		}

		itemShow := systray.AddMenuItem("Show Window", "Show Sloth Clash window")
		itemHide := systray.AddMenuItem("Hide Window", "Hide Sloth Clash window")
		systray.AddSeparator()
		itemConn := systray.AddMenuItem("Connect", "Connect/Disconnect")
		systray.AddSeparator()
		itemQuit := systray.AddMenuItem("Quit", "Quit Sloth Clash")

		go func() {
			for {
				select {
				case <-itemShow.ClickedCh:
					trayMu.Lock()
					app := trayApp
					trayMu.Unlock()
					if app != nil && app.ctx != nil {
						wailsrt.WindowShow(app.ctx)
						wailsrt.WindowUnminimise(app.ctx)
					}
				case <-itemHide.ClickedCh:
					trayMu.Lock()
					app := trayApp
					trayMu.Unlock()
					if app != nil && app.ctx != nil {
						wailsrt.WindowHide(app.ctx)
					}
				case <-itemConn.ClickedCh:
					trayMu.Lock()
					app := trayApp
					trayMu.Unlock()
					if app == nil {
						continue
					}
					st := app.GetAppState()
					if st.Connection.Status == "connected" {
						app.Disconnect()
					} else {
						_, _ = app.Connect()
					}
				case <-itemQuit.ClickedCh:
					trayMu.Lock()
					app := trayApp
					trayMu.Unlock()
					if app != nil && app.ctx != nil {
						app.MarkQuitIntent()
						wailsrt.Quit(app.ctx)
					}
					return
				}
			}
		}()

		go func() {
			tick := time.NewTicker(700 * time.Millisecond)
			defer tick.Stop()
			for range tick.C {
				trayMu.Lock()
				if !trayRunning {
					trayMu.Unlock()
					return
				}
				app := trayApp
				trayMu.Unlock()
				if app == nil {
					continue
				}
				st := app.GetAppState()
				if st.Connection.Status == "connected" {
					itemConn.SetTitle("Disconnect")
				} else if st.Connection.Status == "connecting" {
					itemConn.SetTitle("Connecting...")
				} else {
					itemConn.SetTitle("Connect")
				}
			}
		}()
	}, func() {
		trayMu.Lock()
		trayRunning = false
		trayApp = nil
		trayMu.Unlock()
	})
}

func stopAppTray() {
	trayMu.Lock()
	if !trayRunning {
		trayMu.Unlock()
		return
	}
	trayRunning = false
	trayMu.Unlock()
	systray.Quit()
}

func trayBackendAvailable() bool { return true }
func trayIsReady() bool {
	trayMu.Lock()
	defer trayMu.Unlock()
	return trayRunning
}
