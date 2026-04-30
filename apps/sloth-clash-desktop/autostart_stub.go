//go:build !windows

package main

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"
)

func setLaunchOnStartup(enabled bool) error {
	exe, err := os.Executable()
	if err != nil {
		return err
	}
	exe = strings.TrimSpace(exe)
	if exe == "" {
		return errors.New("cannot resolve executable path")
	}
	switch runtime.GOOS {
	case "darwin":
		return setLaunchOnStartupDarwin(exe, enabled)
	case "linux":
		return setLaunchOnStartupLinux(exe, enabled)
	default:
		return errors.New("launch on startup is unsupported on this platform")
	}
}

func getLaunchOnStartup() (bool, error) {
	exe, err := os.Executable()
	if err != nil {
		return false, err
	}
	exe = strings.TrimSpace(exe)
	switch runtime.GOOS {
	case "darwin":
		return getLaunchOnStartupDarwin(exe)
	case "linux":
		return getLaunchOnStartupLinux(exe)
	default:
		return false, nil
	}
}

func setLaunchOnStartupDarwin(exe string, enabled bool) error {
	home, err := os.UserHomeDir()
	if err != nil {
		return err
	}
	plist := filepath.Join(home, "Library", "LaunchAgents", "dev.slothclash.desktop.autostart.plist")
	if !enabled {
		if err := os.Remove(plist); err != nil && !errors.Is(err, os.ErrNotExist) {
			return err
		}
		return nil
	}
	if err := os.MkdirAll(filepath.Dir(plist), 0o755); err != nil {
		return err
	}
	content := fmt.Sprintf(`<?xml version="1.0" encoding="UTF-8"?>
<!DOCTYPE plist PUBLIC "-//Apple//DTD PLIST 1.0//EN" "http://www.apple.com/DTDs/PropertyList-1.0.dtd">
<plist version="1.0">
<dict>
  <key>Label</key>
  <string>dev.slothclash.desktop.autostart</string>
  <key>ProgramArguments</key>
  <array>
    <string>%s</string>
    <string>--minimized</string>
  </array>
  <key>RunAtLoad</key>
  <true/>
  <key>KeepAlive</key>
  <false/>
</dict>
</plist>
`, xmlEscape(exe))
	return os.WriteFile(plist, []byte(content), 0o644)
}

func getLaunchOnStartupDarwin(exe string) (bool, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return false, err
	}
	plist := filepath.Join(home, "Library", "LaunchAgents", "dev.slothclash.desktop.autostart.plist")
	b, err := os.ReadFile(plist)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return false, nil
		}
		return false, err
	}
	return strings.Contains(strings.ToLower(string(b)), strings.ToLower(exe)), nil
}

func setLaunchOnStartupLinux(exe string, enabled bool) error {
	home, err := os.UserHomeDir()
	if err != nil {
		return err
	}
	desktop := filepath.Join(home, ".config", "autostart", "sloth-clash.desktop")
	if !enabled {
		if err := os.Remove(desktop); err != nil && !errors.Is(err, os.ErrNotExist) {
			return err
		}
		return nil
	}
	if err := os.MkdirAll(filepath.Dir(desktop), 0o755); err != nil {
		return err
	}
	content := fmt.Sprintf(`[Desktop Entry]
Type=Application
Version=1.0
Name=Sloth Clash
Comment=Sloth Clash desktop client
Exec="%s" --minimized
Terminal=false
X-GNOME-Autostart-enabled=true
`, strings.ReplaceAll(exe, `"`, `\"`))
	return os.WriteFile(desktop, []byte(content), 0o644)
}

func getLaunchOnStartupLinux(exe string) (bool, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return false, err
	}
	desktop := filepath.Join(home, ".config", "autostart", "sloth-clash.desktop")
	b, err := os.ReadFile(desktop)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return false, nil
		}
		return false, err
	}
	return strings.Contains(strings.ToLower(string(b)), strings.ToLower(exe)), nil
}

func xmlEscape(s string) string {
	s = strings.ReplaceAll(s, "&", "&amp;")
	s = strings.ReplaceAll(s, "<", "&lt;")
	s = strings.ReplaceAll(s, ">", "&gt;")
	s = strings.ReplaceAll(s, `"`, "&quot;")
	s = strings.ReplaceAll(s, "'", "&apos;")
	return s
}
