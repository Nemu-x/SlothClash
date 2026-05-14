package main

import "strings"

// trayStrings holds localized labels for the native tray menu. We keep this
// table in Go (rather than reading the frontend's i18n JSON at runtime)
// because the tray is built before the webview attaches, and the table is
// tiny — 5 strings × 3 locales.
//
// The active language comes from DesktopPrefs.Lang, which the frontend sets
// via SetUiLanguage(...) at i18n init and on every language change.
type trayStrings struct {
	ShowWindow string
	Connect    string
	Disconnect string
	Connecting string
	Settings   string
	Quit       string
	Tooltip    string
}

var trayStringsEN = trayStrings{
	ShowWindow: "Show Window",
	Connect:    "Connect",
	Disconnect: "Disconnect",
	Connecting: "Connecting...",
	Settings:   "Settings…",
	Quit:       "Quit Sloth Clash",
	Tooltip:    "Sloth Clash",
}

var trayStringsRU = trayStrings{
	ShowWindow: "Показать окно",
	Connect:    "Подключить",
	Disconnect: "Отключить",
	Connecting: "Подключение...",
	Settings:   "Настройки…",
	Quit:       "Выйти из Sloth Clash",
	Tooltip:    "Sloth Clash",
}

var trayStringsZH = trayStrings{
	ShowWindow: "显示窗口",
	Connect:    "连接",
	Disconnect: "断开",
	Connecting: "连接中…",
	Settings:   "设置…",
	Quit:       "退出 Sloth Clash",
	Tooltip:    "Sloth Clash",
}

// currentTrayStrings returns the active translation table. Order of
// resolution: persisted DesktopPrefs.Lang → installer-time language hint →
// English fallback. Unknown values fall back to English.
func currentTrayStrings() trayStrings {
	lang := strings.TrimSpace(currentDesktopPrefs().Lang)
	if lang == "" {
		lang = strings.TrimSpace(detectPreferredLanguage())
	}
	switch lang {
	case "ru":
		return trayStringsRU
	case "zh":
		return trayStringsZH
	}
	return trayStringsEN
}
