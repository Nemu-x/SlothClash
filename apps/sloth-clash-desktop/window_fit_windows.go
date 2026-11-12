//go:build windows

package main

import "unsafe"

// user32 is declared in tray_windows.go.
var (
	procSystemParametersInfo = user32.NewProc("SystemParametersInfoW")
	procGetDpiForSystem      = user32.NewProc("GetDpiForSystem")
)

type winRect struct {
	Left, Top, Right, Bottom int32
}

const spiGetWorkArea = 0x0030

// primaryWorkAreaLogical returns the primary monitor work area (screen minus
// the taskbar) in 96-DPI logical pixels, the unit Wails window sizes use.
// It returns 0, 0 when the values can't be read.
func primaryWorkAreaLogical() (int, int) {
	var r winRect
	ok, _, _ := procSystemParametersInfo.Call(spiGetWorkArea, 0, uintptr(unsafe.Pointer(&r)), 0)
	if ok == 0 {
		return 0, 0
	}
	w := int(r.Right - r.Left)
	h := int(r.Bottom - r.Top)
	if w <= 0 || h <= 0 {
		return 0, 0
	}
	dpi := 96
	if procGetDpiForSystem.Find() == nil { // Windows 10 1607+
		if d, _, _ := procGetDpiForSystem.Call(); d > 0 {
			dpi = int(d)
		}
	}
	return w * 96 / dpi, h * 96 / dpi
}
