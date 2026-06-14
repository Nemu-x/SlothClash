//go:build windows

package main

import (
	"errors"
	"fmt"
	"strconv"
	"strings"
	"syscall"
	"time"

	"golang.org/x/sys/windows/registry"
)

// WinINet proxy-refresh: writing the HKCU Internet Settings registry keys is not
// enough — WinINet (and the system proxy used by most apps) caches the config
// until it's told to reload. clash-verge-rev (and every Windows sysproxy tool)
// calls InternetSetOption after writing. Without it, a proxy change only takes
// effect after a network change / app restart / opening Internet Options.
var (
	procInternetSetOptionW = syscall.NewLazyDLL("wininet.dll").NewProc("InternetSetOptionW")
)

const (
	internetOptionRefresh         = 37 // INTERNET_OPTION_REFRESH
	internetOptionSettingsChanged = 39 // INTERNET_OPTION_SETTINGS_CHANGED
)

// notifyWindowsProxyChanged flushes the WinINet proxy config so an HKCU proxy
// change takes effect immediately. Best-effort: both options take a NULL handle
// and empty buffer; failure is non-fatal (the registry is still correct).
func notifyWindowsProxyChanged() {
	_, _, _ = procInternetSetOptionW.Call(0, uintptr(internetOptionSettingsChanged), 0, 0)
	_, _, _ = procInternetSetOptionW.Call(0, uintptr(internetOptionRefresh), 0, 0)
}

// Mirrors clash-verge-rev Windows default bypass so localhost callbacks and
// local/LAN resources don't get sent into loopback proxy recursion.
const windowsDefaultProxyBypass = "localhost;127.*;192.168.*;10.*;172.16.*;172.17.*;172.18.*;172.19.*;172.20.*;172.21.*;172.22.*;172.23.*;172.24.*;172.25.*;172.26.*;172.27.*;172.28.*;172.29.*;172.30.*;172.31.*;<local>"

func (a *App) applySystemProxyIfNeededLocked() error {
	if a.state.Traffic != "proxy" {
		return nil
	}
	if a.state.Core.MixedPort <= 0 {
		return nil
	}
	addr := "127.0.0.1:" + strconv.Itoa(a.state.Core.MixedPort)
	if err := ensureWindowsUserProxy(addr, true); err != nil {
		return err
	}
	a.appendRuntimeDiag("sysproxy.apply", fmt.Sprintf("127.0.0.1:%d", a.state.Core.MixedPort))
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
	if err := ensureWindowsUserProxy(addr, true); err != nil {
		return err
	}
	a.appendRuntimeDiag("sysproxy.apply", fmt.Sprintf("127.0.0.1:%d", mixed))
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
		a.appendRuntimeDiag("sysproxy.cleanup", "leased")
		_ = ensureWindowsUserProxy("", false)
		a.systemProxyLeased = false
		return
	}
	// Process can restart with systemProxyLeased=false while HKCU proxy still points to old
	// localhost mixed-port from previous Sloth run; clear that stale loopback proxy too.
	_ = clearStaleLoopbackUserProxy()
}

// ensureWindowsUserProxy writes HKCU proxy settings and verifies them, retrying a few times.
// Some Windows builds briefly lag registry reads after writes; clash-verge-rev also toggles
// system proxy when mixed-port changes — we only re-apply the same desired state here.
func ensureWindowsUserProxy(server string, enable bool) error {
	want := strings.TrimSpace(server)
	var lastErr error
	for attempt := 0; attempt < 4; attempt++ {
		if err := setWindowsUserProxy(want, enable); err != nil {
			lastErr = err
		} else if err := verifyWindowsProxyState(want, enable); err != nil {
			lastErr = err
		} else {
			return nil
		}
		time.Sleep(time.Duration(90+attempt*45) * time.Millisecond)
	}
	if lastErr == nil {
		lastErr = fmt.Errorf("unknown proxy apply failure")
	}
	return lastErr
}

// readWindowsUserProxyHKCU returns the current HKCU Internet Settings proxy snapshot.
func readWindowsUserProxyHKCU() (enabled bool, server string, override string, err error) {
	const keyPath = `Software\Microsoft\Windows\CurrentVersion\Internet Settings`
	k, err := registry.OpenKey(registry.CURRENT_USER, keyPath, registry.QUERY_VALUE)
	if err != nil {
		return false, "", "", err
	}
	defer k.Close()
	en, _, errEn := k.GetIntegerValue("ProxyEnable")
	if errEn != nil {
		return false, "", "", errEn
	}
	enabled = en != 0
	srv, _, errSrv := k.GetStringValue("ProxyServer")
	if errSrv != nil && !errors.Is(errSrv, registry.ErrNotExist) {
		return enabled, "", "", errSrv
	}
	server = srv
	ovr, _, errOv := k.GetStringValue("ProxyOverride")
	if errOv != nil && !errors.Is(errOv, registry.ErrNotExist) {
		return enabled, server, "", errOv
	}
	override = ovr
	return enabled, server, override, nil
}

func windowsSysProxyMatchesIntent(mixedPort int, enabled bool, server string, override string) bool {
	if mixedPort <= 0 || !enabled {
		return false
	}
	want := strings.ToLower(strings.TrimSpace("127.0.0.1:" + strconv.Itoa(mixedPort)))
	if strings.ToLower(strings.TrimSpace(server)) != want {
		return false
	}
	o := strings.ToLower(override)
	return strings.Contains(o, "localhost") && strings.Contains(o, "<local>")
}

// maybeWindowsSysProxyReconcile periodically re-applies HKCU proxy when another app drifted it.
func (a *App) maybeWindowsSysProxyReconcile() {
	gen := a.connectGen.Load()
	a.mu.RLock()
	if a.state.Connection.Status != "connected" {
		a.mu.RUnlock()
		return
	}
	if strings.TrimSpace(a.state.Traffic) != "proxy" || !a.systemProxyLeased {
		a.mu.RUnlock()
		return
	}
	mixed := a.state.Core.MixedPort
	a.mu.RUnlock()
	if mixed <= 0 {
		return
	}
	enabled, server, ovr, err := readWindowsUserProxyHKCU()
	if err != nil {
		a.appendRuntimeDiag("sysproxy.reconcile", "read error: "+strings.TrimSpace(err.Error()))
		return
	}
	if windowsSysProxyMatchesIntent(mixed, enabled, server, ovr) {
		return
	}
	a.appendRuntimeDiag("sysproxy.reconcile", "mismatch")
	want := "127.0.0.1:" + strconv.Itoa(mixed)
	var lastErr error
	for i := 0; i < 3; i++ {
		if a.connectGen.Load() != gen {
			return
		}
		lastErr = ensureWindowsUserProxy(want, true)
		if lastErr == nil {
			a.appendRuntimeDiag("sysproxy.reconcile", "restored")
			a.mu.Lock()
			a.clearWindowsSysProxyDriftWarningLocked()
			a.mu.Unlock()
			a.emitAppStateChanged()
			return
		}
		time.Sleep(time.Duration(200+i*120) * time.Millisecond)
	}
	a.mu.Lock()
	if a.connectGen.Load() == gen &&
		a.state.Connection.Status == "connected" &&
		strings.TrimSpace(a.state.Traffic) == "proxy" &&
		a.systemProxyLeased {
		lw := strings.TrimSpace(a.state.Connection.LastWarning)
		if !strings.HasPrefix(lw, "Windows system proxy drift:") {
			a.state.Connection.LastWarning = "Windows system proxy drift: " + strings.TrimSpace(lastErr.Error())
			a.state.Connection.Health = "degraded"
			a.state.Core.Lifecycle = "degraded"
			a.state.UpdatedAt = time.Now().Unix()
		}
	}
	a.mu.Unlock()
	a.appendRuntimeDiag("sysproxy.reconcile", "failed: "+strings.TrimSpace(lastErr.Error()))
	a.emitAppStateChanged()
}

// handleMixedPortChangeForWindowsSysProxy re-applies HKCU proxy when Mihomo mixed-port changes (Windows).
func (a *App) handleMixedPortChangeForWindowsSysProxy(prevPort, newPort int) {
	if prevPort == newPort || newPort <= 0 {
		return
	}
	gen := a.connectGen.Load()
	a.mu.RLock()
	if a.state.Connection.Status != "connected" ||
		strings.TrimSpace(a.state.Traffic) != "proxy" ||
		!a.systemProxyLeased ||
		a.state.Core.MixedPort != newPort {
		a.mu.RUnlock()
		return
	}
	a.mu.RUnlock()
	addr := "127.0.0.1:" + strconv.Itoa(newPort)
	if err := ensureWindowsUserProxy(addr, true); err != nil {
		a.mu.Lock()
		if a.connectGen.Load() == gen && a.state.Connection.Status == "connected" {
			a.markConnectionDegradedLocked("System proxy could not track mixed-port change: " + strings.TrimSpace(err.Error()))
		}
		a.mu.Unlock()
		a.emitAppStateChanged()
		return
	}
	a.appendRuntimeDiag("sysproxy.port_changed", fmt.Sprintf("%d -> %d", prevPort, newPort))
	a.emitAppStateChanged()
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
		_ = k.DeleteValue("ProxyOverride")
		notifyWindowsProxyChanged()
		return nil
	}
	if err := k.SetStringValue("ProxyServer", server); err != nil {
		return err
	}
	if err := k.SetStringValue("ProxyOverride", windowsDefaultProxyBypass); err != nil {
		return err
	}
	if err := k.SetDWordValue("ProxyEnable", 1); err != nil {
		return err
	}
	notifyWindowsProxyChanged()
	return nil
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
	notifyWindowsProxyChanged()
	return nil
}

// clearWindowsSysProxyDriftWarningLocked removes drift-only warnings after reconcile
// restored HKCU. Caller must hold a.mu.
func (a *App) clearWindowsSysProxyDriftWarningLocked() {
	lw := strings.TrimSpace(a.state.Connection.LastWarning)
	if lw == "" {
		return
	}
	parts := strings.Split(lw, " | ")
	var kept []string
	for _, p := range parts {
		pt := strings.TrimSpace(p)
		if pt == "" {
			continue
		}
		if strings.HasPrefix(pt, "Windows system proxy drift:") {
			continue
		}
		kept = append(kept, pt)
	}
	newLw := strings.TrimSpace(strings.Join(kept, " | "))
	a.state.Connection.LastWarning = newLw
	if newLw == "" {
		if a.state.Connection.Health == "degraded" {
			a.markConnectionReadyLocked()
		}
		return
	}
	a.state.Connection.Health = "degraded"
	a.state.Core.Lifecycle = "degraded"
}

func verifyWindowsProxyState(server string, enabled bool) error {
	const keyPath = `Software\Microsoft\Windows\CurrentVersion\Internet Settings`
	want := strings.ToLower(strings.TrimSpace(server))
	var lastErr error
	for i := 0; i < 3; i++ {
		k, err := registry.OpenKey(registry.CURRENT_USER, keyPath, registry.QUERY_VALUE)
		if err != nil {
			lastErr = err
		} else {
			func() {
				defer k.Close()
				en, _, errEn := k.GetIntegerValue("ProxyEnable")
				if errEn != nil {
					lastErr = errEn
					return
				}
				isEnabled := en != 0
				if isEnabled != enabled {
					lastErr = fmt.Errorf("proxy enable mismatch: want=%v got=%v", enabled, isEnabled)
					return
				}
				if !enabled {
					lastErr = nil
					return
				}
				srv, _, errSrv := k.GetStringValue("ProxyServer")
				if errSrv != nil {
					lastErr = errSrv
					return
				}
				if strings.ToLower(strings.TrimSpace(srv)) != want {
					lastErr = fmt.Errorf("proxy server mismatch: want=%s got=%s", server, srv)
					return
				}
				ovr, _, errOv := k.GetStringValue("ProxyOverride")
				if errOv != nil {
					lastErr = errOv
					return
				}
				if !strings.Contains(strings.ToLower(ovr), "localhost") || !strings.Contains(strings.ToLower(ovr), "<local>") {
					lastErr = fmt.Errorf("proxy override missing localhost/<local>: %s", ovr)
					return
				}
				lastErr = nil
			}()
		}
		if lastErr == nil {
			return nil
		}
		time.Sleep(120 * time.Millisecond)
	}
	return lastErr
}
