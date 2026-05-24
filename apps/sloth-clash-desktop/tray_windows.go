//go:build windows

package main

// Native Win32 tray implementation. We talk to Shell_NotifyIconW directly
// and own the message pump ourselves on a single LockOSThread'd goroutine.
//
// This replaces fyne.io/systray which on Windows was prone to wedging after
// long uptime — its NIF_TIP update path leaked GDI handles, and recovering
// from explorer.exe restart (TaskbarCreated) was flaky. Mirroring what we
// already do on macOS (tray_darwin_native.{go,m}) gives us full control over
// the lifecycle.

import (
	_ "embed"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"sync"
	"syscall"
	"time"
	"unsafe"

	wailsrt "github.com/wailsapp/wails/v2/pkg/runtime"
	"golang.org/x/sys/windows"
)

//go:embed build/windows/icon.ico
// Multi-size .ico generated from build/appicon.png (see scripts/generate-windows-icon.mjs).
var windowsTrayIcon []byte

// --- Win32 constants ------------------------------------------------------

const (
	winTrayClassName = "SlothClashTrayWnd"
	winTrayUID       = uint32(1)

	// HWND_MESSAGE = (HWND)-3, cast to uintptr.
	winHwndMessage = ^uintptr(2)

	wmUser          = uintptr(0x0400)
	wmTrayCallback  = uint32(wmUser + 1)
	wmTrayQuit      = uint32(wmUser + 2)
	wmTrayReinit    = uint32(wmUser + 3)
	wmTraySetState  = uint32(wmUser + 4) // wparam = state code (0..2)

	nimAdd        = uintptr(0x00000000)
	nimModify     = uintptr(0x00000001)
	nimDelete     = uintptr(0x00000002)
	nimSetVersion = uintptr(0x00000004)

	nifMessage = uint32(0x00000001)
	nifIcon    = uint32(0x00000002)
	nifTip     = uint32(0x00000004)

	notifyIconVersion4 = uint32(4)

	wmDestroy       = uint32(0x0002)
	wmCommand       = uint32(0x0111)
	wmRButtonUp     = uint32(0x0205)
	wmLButtonDblClk = uint32(0x0203)
	wmContextMenu   = uint32(0x007B)

	mfString    = uintptr(0x00000000)
	mfSeparator = uintptr(0x00000800)

	tpmRightButton = uintptr(0x0002)
	tpmBottomAlign = uintptr(0x0020)
	tpmReturnCmd   = uintptr(0x0100)

	csDblClks = uint32(0x0008)

	imageIcon      = uintptr(1)
	lrLoadFromFile = uintptr(0x00000010)
	lrDefaultSize  = uintptr(0x00000040)

	// menu command IDs (low word of WM_COMMAND wparam)
	cmdShowWindow = uintptr(0x9001)
	cmdToggle     = uintptr(0x9002)
	cmdQuit       = uintptr(0x9003)
)

// connection states surfaced in the tray Connect/Disconnect label.
const (
	trayConnDisc       = 0
	trayConnConnecting = 1
	trayConnConnected  = 2
)

// --- Lazy proc bindings ---------------------------------------------------

var (
	user32   = windows.NewLazySystemDLL("user32.dll")
	kernel32 = windows.NewLazySystemDLL("kernel32.dll")
	shell32  = windows.NewLazySystemDLL("shell32.dll")

	procRegisterClassExW       = user32.NewProc("RegisterClassExW")
	procCreateWindowExW        = user32.NewProc("CreateWindowExW")
	procDefWindowProcW         = user32.NewProc("DefWindowProcW")
	procDestroyWindow          = user32.NewProc("DestroyWindow")
	procGetMessageW            = user32.NewProc("GetMessageW")
	procTranslateMessage       = user32.NewProc("TranslateMessage")
	procDispatchMessageW       = user32.NewProc("DispatchMessageW")
	procPostQuitMessage        = user32.NewProc("PostQuitMessage")
	procPostMessageW           = user32.NewProc("PostMessageW")
	procRegisterWindowMessageW = user32.NewProc("RegisterWindowMessageW")
	procLoadImageW             = user32.NewProc("LoadImageW")
	procDestroyIcon            = user32.NewProc("DestroyIcon")
	procCreatePopupMenu        = user32.NewProc("CreatePopupMenu")
	procDestroyMenu            = user32.NewProc("DestroyMenu")
	procAppendMenuW            = user32.NewProc("AppendMenuW")
	procTrackPopupMenu         = user32.NewProc("TrackPopupMenu")
	procGetCursorPos           = user32.NewProc("GetCursorPos")
	procSetForegroundWindow    = user32.NewProc("SetForegroundWindow")
	procGetModuleHandleW       = kernel32.NewProc("GetModuleHandleW")
	procShellNotifyIconW       = shell32.NewProc("Shell_NotifyIconW")
)

// --- Win32 structs --------------------------------------------------------

type winPoint struct {
	X, Y int32
}

type winMsg struct {
	HWnd    uintptr
	Message uint32
	WParam  uintptr
	LParam  uintptr
	Time    uint32
	Pt      winPoint
	_       uint32 // padding to match Win32 MSG layout on x64
}

type winWndClassExW struct {
	CbSize        uint32
	Style         uint32
	WndProc       uintptr
	ClsExtra      int32
	WndExtra      int32
	Instance      uintptr
	Icon          uintptr
	Cursor        uintptr
	Background    uintptr
	MenuName      *uint16
	ClassName     *uint16
	IconSm        uintptr
}

// NOTIFYICONDATAW V4 (Vista+) — keeps the layout the modern shell expects.
type notifyIconData struct {
	CbSize           uint32
	HWnd             uintptr
	UID              uint32
	UFlags           uint32
	UCallbackMessage uint32
	HIcon            uintptr
	SzTip            [128]uint16
	DwState          uint32
	DwStateMask      uint32
	SzInfo           [256]uint16
	UVersion         uint32
	SzInfoTitle      [64]uint16
	DwInfoFlags      uint32
	GuidItem         windows.GUID
	HBalloonIcon     uintptr
}

// --- Shared state ---------------------------------------------------------

var (
	winTrayMu              sync.Mutex
	winTrayApp             *App
	winTrayUp              bool
	winTrayHWnd            uintptr
	winTrayHIcon           uintptr
	winTrayTaskbarCreated  uint32 // RegisterWindowMessage("TaskbarCreated")
	winTrayStopCh          chan struct{}
	winTrayWndProcCallback uintptr // syscall.NewCallback returns are leaked by design
	winTrayConnState       int     // last known connection state (trayConn*)
)

// --- File logging (matches tray_darwin_native.go) -------------------------

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

// --- Public lifecycle -----------------------------------------------------

func startAppTray(a *App) {
	winTrayMu.Lock()
	if winTrayUp || a == nil {
		winTrayMu.Unlock()
		return
	}
	winTrayApp = a
	stopCh := make(chan struct{})
	winTrayStopCh = stopCh
	winTrayMu.Unlock()

	go runTrayPump(stopCh)
	go runTrayStatePoller(stopCh)
}

func stopAppTray() {
	winTrayMu.Lock()
	hwnd := winTrayHWnd
	ch := winTrayStopCh
	winTrayStopCh = nil
	winTrayMu.Unlock()
	if ch != nil {
		select {
		case <-ch:
		default:
			close(ch)
		}
	}
	if hwnd != 0 {
		procPostMessageW.Call(hwnd, uintptr(wmTrayQuit), 0, 0)
	}
}

func trayBackendAvailable() bool { return true }

func trayIsReady() bool {
	winTrayMu.Lock()
	defer winTrayMu.Unlock()
	return winTrayUp
}

// --- Message pump (pinned to one OS thread) -------------------------------

func runTrayPump(stopCh chan struct{}) {
	defer func() {
		if r := recover(); r != nil {
			writeTrayLog(fmt.Sprintf("[tray] pump panic recovered: %v", r))
		}
		winTrayMu.Lock()
		winTrayUp = false
		winTrayApp = nil
		winTrayHWnd = 0
		winTrayMu.Unlock()
		writeTrayLog("[tray] stopped")
	}()

	runtime.LockOSThread()
	defer runtime.UnlockOSThread()

	hInstance, _, _ := procGetModuleHandleW.Call(0)
	className, err := windows.UTF16PtrFromString(winTrayClassName)
	if err != nil {
		writeTrayLog(fmt.Sprintf("[tray] UTF16PtrFromString class: %v", err))
		return
	}

	// One callback for the lifetime of the process — syscall.NewCallback
	// allocations are bounded (Go runtime caps at 2048) so we make exactly one.
	winTrayMu.Lock()
	if winTrayWndProcCallback == 0 {
		winTrayWndProcCallback = syscall.NewCallback(trayWndProc)
	}
	wndProc := winTrayWndProcCallback
	winTrayMu.Unlock()

	wc := winWndClassExW{
		CbSize:    uint32(unsafe.Sizeof(winWndClassExW{})),
		Style:     csDblClks,
		WndProc:   wndProc,
		Instance:  hInstance,
		ClassName: className,
	}
	atom, _, e := procRegisterClassExW.Call(uintptr(unsafe.Pointer(&wc)))
	// atom == 0 is a real failure; the duplicate-class-atom path returns the
	// existing class so we accept that silently (matters only across restarts
	// within the same process — never happens in practice).
	if atom == 0 {
		writeTrayLog(fmt.Sprintf("[tray] RegisterClassExW failed: %v", e))
		return
	}

	hwnd, _, e := procCreateWindowExW.Call(
		0,                                  // dwExStyle
		uintptr(unsafe.Pointer(className)), // lpClassName
		uintptr(unsafe.Pointer(className)), // lpWindowName
		0,                                  // dwStyle
		0, 0, 0, 0,
		winHwndMessage, // hWndParent = HWND_MESSAGE
		0,              // hMenu
		hInstance,      // hInstance
		0,              // lpParam
	)
	if hwnd == 0 {
		writeTrayLog(fmt.Sprintf("[tray] CreateWindowExW failed: %v", e))
		return
	}

	hIcon, err := loadTrayIcon()
	if err != nil {
		writeTrayLog(fmt.Sprintf("[tray] loadTrayIcon: %v", err))
		// Continue without icon — the menu still works.
	}

	tcMsgName, _ := windows.UTF16PtrFromString("TaskbarCreated")
	tcMsg, _, _ := procRegisterWindowMessageW.Call(uintptr(unsafe.Pointer(tcMsgName)))

	winTrayMu.Lock()
	winTrayHWnd = hwnd
	winTrayHIcon = hIcon
	winTrayTaskbarCreated = uint32(tcMsg)
	winTrayUp = true
	winTrayMu.Unlock()

	if !addNotifyIcon(hwnd, hIcon) {
		writeTrayLog("[tray] Shell_NotifyIconW NIM_ADD failed")
	}
	writeTrayLog("[tray] ready")

	// Standard Win32 message loop.
	var msg winMsg
	for {
		ret, _, _ := procGetMessageW.Call(
			uintptr(unsafe.Pointer(&msg)),
			0, 0, 0,
		)
		// GetMessage returns BOOL (uintptr here); int32 cast handles -1 error.
		switch int32(ret) {
		case -1:
			writeTrayLog("[tray] GetMessageW error")
			goto cleanup
		case 0:
			// WM_QUIT
			goto cleanup
		}
		procTranslateMessage.Call(uintptr(unsafe.Pointer(&msg)))
		procDispatchMessageW.Call(uintptr(unsafe.Pointer(&msg)))
	}

cleanup:
	removeNotifyIcon(hwnd)
	if hIcon != 0 {
		procDestroyIcon.Call(hIcon)
	}
	procDestroyWindow.Call(hwnd)
}

// trayWndProc is the WndProc registered for our message-only window. It runs
// on the pump goroutine's locked OS thread.
func trayWndProc(hwnd uintptr, msg uint32, wparam, lparam uintptr) uintptr {
	// TaskbarCreated arrives whenever explorer.exe restarts (Windows Update,
	// Edge crash, etc). Re-register our icon so the user does not lose access.
	winTrayMu.Lock()
	tcMsg := winTrayTaskbarCreated
	winTrayMu.Unlock()
	if tcMsg != 0 && msg == tcMsg {
		winTrayMu.Lock()
		hIcon := winTrayHIcon
		winTrayMu.Unlock()
		addNotifyIcon(hwnd, hIcon)
		writeTrayLog("[tray] re-added after TaskbarCreated")
		return 0
	}

	switch msg {
	case wmTrayCallback:
		// lparam low word = mouse message
		event := uint32(lparam & 0xFFFF)
		switch event {
		case wmLButtonDblClk:
			showAppWindow()
		case wmRButtonUp, wmContextMenu:
			showTrayContextMenu(hwnd)
		}
		return 0
	case wmCommand:
		cmd := wparam & 0xFFFF
		handleTrayCommand(cmd)
		return 0
	case wmTrayQuit:
		procDestroyWindow.Call(hwnd)
		return 0
	case wmDestroy:
		procPostQuitMessage.Call(0)
		return 0
	}

	ret, _, _ := procDefWindowProcW.Call(hwnd, uintptr(msg), wparam, lparam)
	return ret
}

// showTrayContextMenu builds the popup menu fresh each time so the Connect /
// Disconnect label always reflects current state without us mutating a
// long-lived HMENU.
func showTrayContextMenu(hwnd uintptr) {
	hMenu, _, _ := procCreatePopupMenu.Call()
	if hMenu == 0 {
		return
	}
	defer procDestroyMenu.Call(hMenu)

	winTrayMu.Lock()
	state := winTrayConnState
	winTrayMu.Unlock()
	labels := currentTrayStrings()
	connectLabel := labels.Connect
	switch state {
	case trayConnConnected:
		connectLabel = labels.Disconnect
	case trayConnConnecting:
		connectLabel = labels.Connecting
	}

	appendMenuStr(hMenu, cmdShowWindow, labels.ShowWindow)
	appendMenuSeparator(hMenu)
	appendMenuStr(hMenu, cmdToggle, connectLabel)
	appendMenuSeparator(hMenu)
	appendMenuStr(hMenu, cmdQuit, labels.Quit)

	// Bringing our hidden window to the foreground is required by Win32 to
	// keep the popup interactive (otherwise the user clicks elsewhere and the
	// menu dismisses without firing WM_COMMAND).
	procSetForegroundWindow.Call(hwnd)

	var pt winPoint
	procGetCursorPos.Call(uintptr(unsafe.Pointer(&pt)))

	cmd, _, _ := procTrackPopupMenu.Call(
		hMenu,
		tpmRightButton|tpmBottomAlign|tpmReturnCmd,
		uintptr(pt.X), uintptr(pt.Y),
		0,
		hwnd,
		0,
	)
	if cmd != 0 {
		handleTrayCommand(cmd)
	}

	// Per MSDN: post a no-op message to unstick the popup from the foreground
	// queue so the next right-click is not eaten.
	procPostMessageW.Call(hwnd, 0, 0, 0)
}

func appendMenuStr(hMenu, id uintptr, label string) {
	w, err := windows.UTF16PtrFromString(label)
	if err != nil {
		return
	}
	procAppendMenuW.Call(hMenu, mfString, id, uintptr(unsafe.Pointer(w)))
}

func appendMenuSeparator(hMenu uintptr) {
	procAppendMenuW.Call(hMenu, mfSeparator, 0, 0)
}

func handleTrayCommand(cmd uintptr) {
	winTrayMu.Lock()
	app := winTrayApp
	winTrayMu.Unlock()
	if app == nil {
		return
	}
	switch cmd {
	case cmdShowWindow:
		showAppWindow()
	case cmdToggle:
		// Connect/Disconnect can take seconds — never block the message pump.
		go func(app *App) {
			defer func() {
				if r := recover(); r != nil {
					writeTrayLog(fmt.Sprintf("[tray] toggle panic: %v", r))
				}
			}()
			if app.GetAppState().Connection.Status == "connected" {
				app.Disconnect()
			} else {
				_, _ = app.Connect()
			}
		}(app)
	case cmdQuit:
		if app.ctx != nil {
			app.MarkQuitIntent()
			wailsrt.Quit(app.ctx)
		}
	}
}

func showAppWindow() {
	winTrayMu.Lock()
	app := winTrayApp
	winTrayMu.Unlock()
	if app == nil || app.ctx == nil {
		return
	}
	wailsrt.WindowShow(app.ctx)
	wailsrt.WindowUnminimise(app.ctx)
}

// --- Connection state polling --------------------------------------------

// runTrayStatePoller mirrors what the macOS tray does: it polls app state on
// a low cadence and caches the result. Menu rebuild reads the cache, so the
// pump thread never has to lock app.mu (which serializes against reconnects).
func runTrayStatePoller(stopCh <-chan struct{}) {
	defer func() {
		if r := recover(); r != nil {
			writeTrayLog(fmt.Sprintf("[tray] state poller panic: %v", r))
		}
	}()
	tick := time.NewTicker(1500 * time.Millisecond)
	defer tick.Stop()
	for {
		select {
		case <-stopCh:
			return
		case <-tick.C:
			winTrayMu.Lock()
			app := winTrayApp
			up := winTrayUp
			winTrayMu.Unlock()
			if !up || app == nil {
				continue
			}
			st := app.GetAppState()
			var s int
			switch st.Connection.Status {
			case "connected":
				s = trayConnConnected
			case "connecting":
				s = trayConnConnecting
			default:
				s = trayConnDisc
			}
			winTrayMu.Lock()
			winTrayConnState = s
			winTrayMu.Unlock()
		}
	}
}

// --- Shell_NotifyIcon helpers --------------------------------------------

func addNotifyIcon(hwnd, hIcon uintptr) bool {
	nid := makeNotifyIconData(hwnd, hIcon)
	ret, _, _ := procShellNotifyIconW.Call(nimAdd, uintptr(unsafe.Pointer(&nid)))
	return ret != 0
}

func removeNotifyIcon(hwnd uintptr) {
	nid := notifyIconData{
		CbSize: uint32(unsafe.Sizeof(notifyIconData{})),
		HWnd:   hwnd,
		UID:    winTrayUID,
	}
	procShellNotifyIconW.Call(nimDelete, uintptr(unsafe.Pointer(&nid)))
}

func makeNotifyIconData(hwnd, hIcon uintptr) notifyIconData {
	nid := notifyIconData{
		CbSize:           uint32(unsafe.Sizeof(notifyIconData{})),
		HWnd:             hwnd,
		UID:              winTrayUID,
		UFlags:           nifMessage | nifIcon | nifTip,
		UCallbackMessage: wmTrayCallback,
		HIcon:            hIcon,
	}
	tip, _ := windows.UTF16FromString(currentTrayStrings().Tooltip)
	copy(nid.SzTip[:], tip)
	return nid
}

func loadTrayIcon() (uintptr, error) {
	if len(windowsTrayIcon) == 0 {
		return 0, errors.New("embedded windows tray icon is empty")
	}
	dir, err := slothDataRoot()
	if err != nil {
		dir = os.TempDir()
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		dir = os.TempDir()
	}
	p := filepath.Join(dir, "tray-icon.ico")
	if err := os.WriteFile(p, windowsTrayIcon, 0o644); err != nil {
		return 0, fmt.Errorf("write icon file: %w", err)
	}
	u16, err := windows.UTF16PtrFromString(p)
	if err != nil {
		return 0, err
	}
	h, _, ce := procLoadImageW.Call(
		0,
		uintptr(unsafe.Pointer(u16)),
		imageIcon,
		0, 0, // cxDesired, cyDesired — 0 means use default tray icon size
		lrLoadFromFile|lrDefaultSize,
	)
	if h == 0 {
		return 0, fmt.Errorf("LoadImageW: %v", ce)
	}
	return h, nil
}
