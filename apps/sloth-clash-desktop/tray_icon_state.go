package main

import (
	"runtime"
	"strings"
)

// The tray icon mirrors the live traffic path, the way the other Clash
// desktops do: plain icon while disconnected, a green dot once the system
// proxy is set, a blue ring once the TUN adapter is up. The assets are built by
// scripts/generate-tray-icons.mjs (dot vs ring is a shape difference on
// purpose — macOS template icons drop colour) and embedded per platform:
// build/windows/tray/<style>-<state>.ico and trayicons/state/<style>-<state>.png.

type trayIconKind int

const (
	trayIconIdle trayIconKind = iota
	trayIconProxy
	trayIconTun
)

func (k trayIconKind) String() string {
	switch k {
	case trayIconProxy:
		return "proxy"
	case trayIconTun:
		return "tun"
	default:
		return "idle"
	}
}

// Tray icon styles the user can pick in Settings. "" means platform default.
const (
	trayIconStyleColorful = "colorful"
	trayIconStyleMono     = "mono"
)

// trayIconKindFor maps connection status + traffic mode to the icon to show.
// Only a fully established connection changes the icon: "connecting" keeps the
// idle glyph so a stuck handshake never advertises a tunnel that is not there.
func trayIconKindFor(connectionStatus, traffic string) trayIconKind {
	if strings.TrimSpace(strings.ToLower(connectionStatus)) != "connected" {
		return trayIconIdle
	}
	if strings.TrimSpace(strings.ToLower(traffic)) == "tun" {
		return trayIconTun
	}
	return trayIconProxy
}

// normalizeTrayIconStyle accepts colorful / mono (case-insensitive) and returns
// the canonical value; anything else → "" (platform default).
func normalizeTrayIconStyle(v string) string {
	switch strings.ToLower(strings.TrimSpace(v)) {
	case trayIconStyleColorful:
		return trayIconStyleColorful
	case trayIconStyleMono:
		return trayIconStyleMono
	default:
		return ""
	}
}

// defaultTrayIconStyleFor is the platform default when the user has not
// chosen: macOS menu bars are monochrome template glyphs by convention, the
// Windows notification area is full colour.
func defaultTrayIconStyleFor(goos string) string {
	if goos == "darwin" {
		return trayIconStyleMono
	}
	return trayIconStyleColorful
}

// resolveTrayIconStyle turns the stored preference into the style to render.
func resolveTrayIconStyle(pref, goos string) string {
	if s := normalizeTrayIconStyle(pref); s != "" {
		return s
	}
	return defaultTrayIconStyleFor(goos)
}

// currentTrayIconStyle reads the persisted preference for this platform.
func currentTrayIconStyle() string {
	return resolveTrayIconStyle(currentDesktopPrefs().Tray.IconStyle, runtime.GOOS)
}

// trayIconAssetBase is the file stem shared by the .ico and .png variants.
func trayIconAssetBase(style string, kind trayIconKind) string {
	if normalizeTrayIconStyle(style) == "" {
		style = defaultTrayIconStyleFor(runtime.GOOS)
	}
	return style + "-" + kind.String()
}

// trayTooltipFor is the hover text: app name plus the live mode, so the state
// is readable even where the badge is small (Windows 100% DPI = 16 px).
func trayTooltipFor(kind trayIconKind, s trayStrings) string {
	base := strings.TrimSpace(s.Tooltip)
	if base == "" {
		base = "Sloth Clash"
	}
	var mode string
	switch kind {
	case trayIconProxy:
		mode = s.ModeProxy
	case trayIconTun:
		mode = s.ModeTun
	default:
		mode = s.ModeIdle
	}
	mode = strings.TrimSpace(mode)
	if mode == "" {
		return base
	}
	return base + " · " + mode
}
