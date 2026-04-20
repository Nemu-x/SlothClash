//go:build windows

package main

import (
	"strings"

	"golang.org/x/sys/windows/registry"
)

// NSIS MUI_LANGDLL stores numeric language id in HKCU.
func detectPreferredLanguage() string {
	const keyPath = `Software\Nemu-x\SlothClashDesktop\Installer`
	k, err := registry.OpenKey(registry.CURRENT_USER, keyPath, registry.QUERY_VALUE)
	if err != nil {
		return ""
	}
	defer k.Close()

	raw, _, err := k.GetStringValue("InstallerLanguage")
	if err != nil {
		return ""
	}
	switch strings.TrimSpace(raw) {
	case "1049":
		return "ru"
	case "2052":
		return "zh"
	case "1033":
		return "en"
	default:
		return ""
	}
}

