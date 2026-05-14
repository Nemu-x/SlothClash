//go:build windows

package main

import (
	_ "embed"
	"sync"
	"time"

	"fyne.io/systray"
	wailsrt "github.com/wailsapp/wails/v2/pkg/runtime"
)

//go:embed build/windows/icon.ico
// Multi-size .ico from build/appicon.png (see scripts/generate-windows-icon.mjs, pnpm icons:windows).
// trayicons/sloth.png is for macOS / future Wails TrayMenu, not read here.
var windowsTrayIcon []byte

var (
	trayMu      sync.Mutex
	trayRunning bool
	trayApp     *App
	trayStopCh  chan struct{}

	// Native double-click detection: Windows sends two WM_LBUTTONUP events
	// for a double-click on the tray icon. We treat them as a double-click
	// when they arrive within the system DCLICK interval (default 500ms).
	trayClickMu        sync.Mutex
	trayLastLeftClick  time.Time
	trayDoubleClickGap = 500 * time.Millisecond

	trayConnToggleMu       sync.Mutex
	trayConnToggleInFlight bool
	trayConnToggleQueued   bool
)

func requestTrayConnectionToggle(a *App) {
	if a == nil {
		return
	}
	trayConnToggleMu.Lock()
	if trayConnToggleInFlight {
		trayConnToggleQueued = true
		trayConnToggleMu.Unlock()
		return
	}
	trayConnToggleInFlight = true
	trayConnToggleMu.Unlock()

	go func(app *App) {
		defer func() {
			trayConnToggleMu.Lock()
			trayConnToggleInFlight = false
			trayConnToggleQueued = false
			trayConnToggleMu.Unlock()
		}()
		for {
			st := app.GetAppState()
			if st.Connection.Status == "connected" {
				app.Disconnect()
			} else {
				_, _ = app.Connect()
			}
			trayConnToggleMu.Lock()
			queued := trayConnToggleQueued
			trayConnToggleQueued = false
			trayConnToggleMu.Unlock()
			if !queued {
				return
			}
			// Coalesce burst clicks into one extra pass.
			time.Sleep(120 * time.Millisecond)
		}
	}(a)
}

// startAppTray launches the Windows tray loop on a dedicated goroutine and
// wires Show / Hide / Connect / Quit menu entries. The icon is the same .ico
// that ships as the main application window icon so the tray matches the app
// chrome across Windows builds.
//
// Click behaviour (Verge-Rev / common Windows apps):
//   - Double-click on the icon → show / restore the main window (native hook).
//   - Right-click → open the options menu.
//   - Single left-click → no-op (waits to see if it becomes a double-click).
//
// This mirrors the Windows convention users already expect from tools like
// Telegram Desktop, Discord and Clash Verge Rev (Tauri tray with on_left_click
// / on_right_click), so existing muscle memory carries over.
func startAppTray(a *App) {
	trayMu.Lock()
	if trayRunning || a == nil {
		trayMu.Unlock()
		return
	}
	trayRunning = true
	trayApp = a
	trayStopCh = make(chan struct{})
	stopCh := trayStopCh
	trayMu.Unlock()

	go systray.Run(func() {
		if len(windowsTrayIcon) > 0 {
			systray.SetIcon(windowsTrayIcon)
		}
		systray.SetTitle("Sloth Clash")
		systray.SetTooltip("Sloth Clash")

		// Native hook: double-click (two WM_LBUTTONUP within ~500ms) opens
		// the main window. SetOnSecondaryTapped is left unset so right-click
		// falls back to systray's default showMenu() path — matching the
		// behaviour the user asked for ("на ПКМ опции, на даблклик окно").
		systray.SetOnTapped(func() {
			trayClickMu.Lock()
			now := time.Now()
			isDouble := !trayLastLeftClick.IsZero() && now.Sub(trayLastLeftClick) <= trayDoubleClickGap
			if isDouble {
				// Reset so triple-click doesn't re-fire the double-click path.
				trayLastLeftClick = time.Time{}
			} else {
				trayLastLeftClick = now
			}
			trayClickMu.Unlock()
			if !isDouble {
				return
			}

			trayMu.Lock()
			app := trayApp
			trayMu.Unlock()
			if app != nil && app.ctx != nil {
				wailsrt.WindowShow(app.ctx)
				wailsrt.WindowUnminimise(app.ctx)
			}
		})

		itemShow := systray.AddMenuItem("Show Window", "Show Sloth Clash window")
		itemHide := systray.AddMenuItem("Hide Window", "Hide Sloth Clash window")
		systray.AddSeparator()
		itemConn := systray.AddMenuItem("Connect", "Connect / Disconnect")
		systray.AddSeparator()
		itemQuit := systray.AddMenuItem("Quit", "Quit Sloth Clash")

		go func() {
			// Click-event dispatcher. Any panic inside a wails runtime call
			// would otherwise kill this goroutine and leave the tray icon
			// alive but inert (no menu reacts) — recover so the loop keeps
			// running.
			defer func() {
				if r := recover(); r != nil {
					// best-effort; cannot rely on logger order with main goroutine
					_ = r
				}
			}()
			for {
				select {
				case <-stopCh:
					return
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
					// Keep systray loop responsive and coalesce rapid clicks.
					requestTrayConnectionToggle(app)
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
			// fyne.io/systray's SetTitle marshals the call through a native
			// Win32 NIF_TIP update on the systray main thread. Repeating it
			// every 700ms with the same payload is a known wedge source —
			// after enough updates the tray window proc has been observed to
			// stop pumping messages, freezing the icon and the whole app.
			// We now keep a cached `last` value and only call SetTitle when
			// the visible label actually changes.
			defer func() {
				if r := recover(); r != nil {
					_ = r
				}
			}()
			tick := time.NewTicker(1500 * time.Millisecond)
			defer tick.Stop()
			lastTitle := "Connect"
			for {
				select {
				case <-stopCh:
					return
				case <-tick.C:
					trayMu.Lock()
					running := trayRunning
					app := trayApp
					trayMu.Unlock()
					if !running || app == nil {
						continue
					}
					st := app.GetAppState()
					var desired string
					switch st.Connection.Status {
					case "connected":
						desired = "Disconnect"
					case "connecting":
						desired = "Connecting..."
					default:
						desired = "Connect"
					}
					if desired == lastTitle {
						continue
					}
					lastTitle = desired
					itemConn.SetTitle(desired)
				}
			}
		}()
	}, func() {
		trayMu.Lock()
		trayRunning = false
		trayApp = nil
		if trayStopCh != nil {
			select {
			case <-trayStopCh:
			default:
				close(trayStopCh)
			}
			trayStopCh = nil
		}
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
	if trayStopCh != nil {
		select {
		case <-trayStopCh:
		default:
			close(trayStopCh)
		}
		trayStopCh = nil
	}
	trayMu.Unlock()
	systray.Quit()
}

func trayBackendAvailable() bool { return true }

func trayIsReady() bool {
	trayMu.Lock()
	defer trayMu.Unlock()
	return trayRunning
}
