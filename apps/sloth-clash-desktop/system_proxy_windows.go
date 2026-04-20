//go:build windows

package main

import (
	"strings"
	"strconv"

	"golang.org/x/sys/windows/registry"
)

func (a *App) applySystemProxyIfNeededLocked() error {
	if a.state.Traffic != "proxy" {
		return nil
	}
	if a.state.Core.MixedPort <= 0 {
		return nil
	}
	addr := "127.0.0.1:" + strconv.Itoa(a.state.Core.MixedPort)
	if err := setWindowsUserProxy(addr, true); err != nil {
		return err
	}
	a.systemProxyLeased = true
	return nil
}

// applySystemProxyFromSnapshot applies HKCU proxy when Traffic is proxy, without holding a.mu
// during registry I/O (caller may use this after connect pipeline).
func (a *App) applySystemProxyFromSnapshot() error {
	a.mu.RLock()
	traffic := a.state.Traffic
	mixed := a.state.Core.MixedPort
	a.mu.RUnlock()
	if traffic != "proxy" || mixed <= 0 {
		return nil
	}
	addr := "127.0.0.1:" + strconv.Itoa(mixed)
	if err := setWindowsUserProxy(addr, true); err != nil {
		return err
	}
	a.mu.Lock()
	a.systemProxyLeased = true
	a.mu.Unlock()
	return nil
}

// clearSystemProxyFromSnapshot clears stale localhost system proxy for non-proxy traffic.
func (a *App) clearSystemProxyFromSnapshot() error {
	a.mu.RLock()
	traffic := a.state.Traffic
	a.mu.RUnlock()
	if traffic == "proxy" {
		return nil
	}
	a.mu.Lock()
	a.clearSystemProxyLocked()
	a.mu.Unlock()
	return nil
}

func (a *App) clearSystemProxyLocked() {
	if a.systemProxyLeased {
		_ = setWindowsUserProxy("", false)
		a.systemProxyLeased = false
		return
	}
	// Process can restart with systemProxyLeased=false while HKCU proxy still points to old
	// localhost mixed-port from previous Sloth run; clear that stale loopback proxy too.
	_ = clearStaleLoopbackUserProxy()
}

func setWindowsUserProxy(server string, enable bool) error {
	const keyPath = `Software\Microsoft\Windows\CurrentVersion\Internet Settings`
	k, _, err := registry.CreateKey(registry.CURRENT_USER, keyPath, registry.SET_VALUE)
	if err != nil {
		return err
	}
	defer k.Close()
	if !enable || server == "" {
		if err := k.SetDWordValue("ProxyEnable", 0); err != nil {
			return err
		}
		_ = k.DeleteValue("ProxyServer")
		return nil
	}
	if err := k.SetStringValue("ProxyServer", server); err != nil {
		return err
	}
	return k.SetDWordValue("ProxyEnable", 1)
}

func clearStaleLoopbackUserProxy() error {
	const keyPath = `Software\Microsoft\Windows\CurrentVersion\Internet Settings`
	k, err := registry.OpenKey(registry.CURRENT_USER, keyPath, registry.QUERY_VALUE|registry.SET_VALUE)
	if err != nil {
		return err
	}
	defer k.Close()

	enabled, _, _ := k.GetIntegerValue("ProxyEnable")
	if enabled == 0 {
		return nil
	}
	server, _, err := k.GetStringValue("ProxyServer")
	if err != nil || strings.TrimSpace(server) == "" {
		return nil
	}
	s := strings.ToLower(strings.TrimSpace(server))
	// Keep user's non-local proxies untouched; only clear stale localhost proxies.
	if !strings.Contains(s, "127.0.0.1:") && !strings.Contains(s, "localhost:") {
		return nil
	}
	if err := k.SetDWordValue("ProxyEnable", 0); err != nil {
		return err
	}
	_ = k.DeleteValue("ProxyServer")
	return nil
}
