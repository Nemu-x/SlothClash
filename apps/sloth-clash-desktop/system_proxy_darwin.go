//go:build darwin

package main

import (
	"fmt"
	"os/exec"
	"strconv"
	"strings"
	"sync"
)

// darwinProxyReconcileOnce bounds the leaked-loopback-proxy cleanup to one pass
// per process (startup), so the periodic supervisor tick doesn't spawn
// networksetup on every iteration while idle/TUN.
var darwinProxyReconcileOnce sync.Once

func (a *App) applySystemProxyIfNeededLocked() error {
	if a.state.Traffic != "proxy" {
		return nil
	}
	if a.state.Core.MixedPort <= 0 {
		return nil
	}
	addr := "127.0.0.1"
	port := a.state.Core.MixedPort
	if !a.systemProxyLeased {
		services, err := darwinNetworkServices()
		if err == nil {
			a.systemProxySnapshot = captureDarwinSystemProxySnapshot(services)
		}
	}
	if err := setDarwinSystemProxy(addr, port, true); err != nil {
		return err
	}
	a.systemProxyLeased = true
	return nil
}

func (a *App) applySystemProxyFromSnapshot() error {
	a.mu.RLock()
	traffic := a.state.Traffic
	mixed := a.state.Core.MixedPort
	leased := a.systemProxyLeased
	a.mu.RUnlock()
	if traffic != "proxy" || mixed <= 0 {
		return nil
	}
	if !leased {
		services, err := darwinNetworkServices()
		if err == nil {
			snap := captureDarwinSystemProxySnapshot(services)
			a.mu.Lock()
			if !a.systemProxyLeased {
				a.systemProxySnapshot = snap
			}
			a.mu.Unlock()
		}
	}
	if err := setDarwinSystemProxy("127.0.0.1", mixed, true); err != nil {
		return err
	}
	a.mu.Lock()
	a.systemProxyLeased = true
	a.mu.Unlock()
	return nil
}

func (a *App) clearSystemProxyFromSnapshot() error {
	a.mu.RLock()
	traffic := a.state.Traffic
	leased := a.systemProxyLeased
	snapshot := a.systemProxySnapshot
	a.mu.RUnlock()
	if traffic == "proxy" {
		return nil
	}
	if err := clearDarwinSystemProxyWithSnapshot(leased, snapshot); err != nil {
		return err
	}
	a.mu.Lock()
	a.systemProxyLeased = false
	a.systemProxySnapshot = nil
	a.mu.Unlock()
	return nil
}

func (a *App) clearSystemProxyLocked() {
	// Called with a.mu already held in multiple paths (stop/restart).
	// Never re-enter mutex here.
	_ = clearDarwinSystemProxyWithSnapshot(a.systemProxyLeased, a.systemProxySnapshot)
	a.systemProxyLeased = false
	a.systemProxySnapshot = nil
}

func setDarwinSystemProxy(host string, port int, enable bool) error {
	services, err := darwinNetworkServices()
	if err != nil {
		return err
	}
	if len(services) == 0 {
		return fmt.Errorf("no active network services found")
	}
	var errs []string
	successes := 0
	for _, svc := range services {
		if enable {
			if err := runNetworksetup("-setwebproxy", svc, host, strconv.Itoa(port)); err != nil {
				errs = append(errs, fmt.Sprintf("%s web: %v", svc, err))
				continue
			}
			if err := runNetworksetup("-setsecurewebproxy", svc, host, strconv.Itoa(port)); err != nil {
				errs = append(errs, fmt.Sprintf("%s secure: %v", svc, err))
				continue
			}
			_ = runNetworksetup("-setproxybypassdomains", svc, "localhost", "127.0.0.1")
			if err := runNetworksetup("-setwebproxystate", svc, "on"); err != nil {
				errs = append(errs, fmt.Sprintf("%s webstate: %v", svc, err))
				continue
			}
			if err := runNetworksetup("-setsecurewebproxystate", svc, "on"); err != nil {
				errs = append(errs, fmt.Sprintf("%s securestate: %v", svc, err))
				continue
			}
			successes++
		} else {
			offOK := true
			if err := runNetworksetup("-setwebproxystate", svc, "off"); err != nil {
				errs = append(errs, fmt.Sprintf("%s webstate: %v", svc, err))
				offOK = false
			}
			if err := runNetworksetup("-setsecurewebproxystate", svc, "off"); err != nil {
				errs = append(errs, fmt.Sprintf("%s securestate: %v", svc, err))
				offOK = false
			}
			if offOK {
				successes++
			}
		}
	}
	if successes > 0 {
		// Some virtual/VPN services reject networksetup proxy args (exit status 4).
		// Treat per-service failures as non-fatal when at least one primary service succeeded.
		return nil
	}
	if len(errs) > 0 {
		return fmt.Errorf("%s", strings.Join(errs, "; "))
	}
	return nil
}

func darwinNetworkServices() ([]string, error) {
	cmd := exec.Command("networksetup", "-listallnetworkservices")
	out, err := cmd.CombinedOutput()
	if err != nil {
		return nil, fmt.Errorf("networksetup -listallnetworkservices failed: %w (%s)", err, strings.TrimSpace(string(out)))
	}
	lines := strings.Split(string(out), "\n")
	var services []string
	for _, line := range lines {
		s := strings.TrimSpace(line)
		if s == "" {
			continue
		}
		if strings.HasPrefix(s, "An asterisk") {
			continue
		}
		if strings.HasPrefix(s, "*") {
			continue
		}
		services = append(services, s)
	}
	return services, nil
}

func runNetworksetup(args ...string) error {
	cmd := exec.Command("networksetup", args...)
	out, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("%w (%s)", err, strings.TrimSpace(string(out)))
	}
	return nil
}

func clearDarwinSystemProxyWithSnapshot(leased bool, snapshot map[string]SystemProxyServiceSnapshot) error {
	if leased {
		if len(snapshot) > 0 {
			return restoreDarwinSystemProxySnapshot(snapshot)
		}
		return setDarwinSystemProxy("", 0, false)
	}
	// Not leased in THIS process — but systemProxyLeased lives only in memory and
	// does NOT survive a force-quit / crash / updater-kill, while the macOS proxy
	// setting persists (even across reboots). A stale "127.0.0.1:<oldport>" proxy we
	// set earlier would then never be cleared and blackholes all traffic once the
	// core stops. So still turn off any proxy that points at loopback (ours); never
	// touch a real upstream proxy the user configured.
	return clearDarwinLoopbackProxy()
}

// isLoopbackProxyHost reports whether a proxy server value is our own loopback.
func isLoopbackProxyHost(h string) bool {
	switch strings.TrimSpace(strings.ToLower(h)) {
	case "127.0.0.1", "::1", "localhost":
		return true
	}
	return false
}

// clearDarwinLoopbackProxy turns off web/secure proxies that currently point at
// loopback across all services — cleaning up a lease we lost track of. Upstream
// proxies (non-loopback) are left untouched.
func clearDarwinLoopbackProxy() error {
	services, err := darwinNetworkServices()
	if err != nil {
		return err
	}
	var errs []string
	for _, svc := range services {
		if web, e := getDarwinProxyState("-getwebproxy", svc); e == nil && web.enabled && isLoopbackProxyHost(web.server) {
			if err := runNetworksetup("-setwebproxystate", svc, "off"); err != nil {
				errs = append(errs, fmt.Sprintf("%s web off: %v", svc, err))
			}
		}
		if sec, e := getDarwinProxyState("-getsecurewebproxy", svc); e == nil && sec.enabled && isLoopbackProxyHost(sec.server) {
			if err := runNetworksetup("-setsecurewebproxystate", svc, "off"); err != nil {
				errs = append(errs, fmt.Sprintf("%s secure off: %v", svc, err))
			}
		}
	}
	if len(errs) > 0 {
		return fmt.Errorf("%s", strings.Join(errs, "; "))
	}
	return nil
}

func captureDarwinSystemProxySnapshot(services []string) map[string]SystemProxyServiceSnapshot {
	out := make(map[string]SystemProxyServiceSnapshot, len(services))
	for _, svc := range services {
		web, werr := getDarwinProxyState("-getwebproxy", svc)
		sec, serr := getDarwinProxyState("-getsecurewebproxy", svc)
		if werr != nil && serr != nil {
			continue
		}
		out[svc] = SystemProxyServiceSnapshot{
			WebEnabled:    web.enabled,
			WebServer:     web.server,
			WebPort:       web.port,
			SecureEnabled: sec.enabled,
			SecureServer:  sec.server,
			SecurePort:    sec.port,
		}
	}
	return out
}

func restoreDarwinSystemProxySnapshot(snapshot map[string]SystemProxyServiceSnapshot) error {
	var errs []string
	for svc, st := range snapshot {
		if st.WebEnabled && st.WebServer != "" && st.WebPort > 0 {
			if err := runNetworksetup("-setwebproxy", svc, st.WebServer, strconv.Itoa(st.WebPort)); err != nil {
				errs = append(errs, fmt.Sprintf("%s restore web: %v", svc, err))
			}
			if err := runNetworksetup("-setwebproxystate", svc, "on"); err != nil {
				errs = append(errs, fmt.Sprintf("%s restore webstate: %v", svc, err))
			}
		} else if err := runNetworksetup("-setwebproxystate", svc, "off"); err != nil {
			errs = append(errs, fmt.Sprintf("%s restore webstate off: %v", svc, err))
		}
		if st.SecureEnabled && st.SecureServer != "" && st.SecurePort > 0 {
			if err := runNetworksetup("-setsecurewebproxy", svc, st.SecureServer, strconv.Itoa(st.SecurePort)); err != nil {
				errs = append(errs, fmt.Sprintf("%s restore secure: %v", svc, err))
			}
			if err := runNetworksetup("-setsecurewebproxystate", svc, "on"); err != nil {
				errs = append(errs, fmt.Sprintf("%s restore securestate: %v", svc, err))
			}
		} else if err := runNetworksetup("-setsecurewebproxystate", svc, "off"); err != nil {
			errs = append(errs, fmt.Sprintf("%s restore securestate off: %v", svc, err))
		}
	}
	if len(errs) > 0 {
		return fmt.Errorf("%s", strings.Join(errs, "; "))
	}
	return nil
}

type darwinProxyState struct {
	enabled bool
	server  string
	port    int
}

func getDarwinProxyState(flag string, service string) (darwinProxyState, error) {
	cmd := exec.Command("networksetup", flag, service)
	out, err := cmd.CombinedOutput()
	if err != nil {
		return darwinProxyState{}, fmt.Errorf("%w (%s)", err, strings.TrimSpace(string(out)))
	}
	lines := strings.Split(string(out), "\n")
	state := darwinProxyState{}
	for _, line := range lines {
		s := strings.TrimSpace(line)
		if strings.HasPrefix(s, "Enabled:") {
			v := strings.TrimSpace(strings.TrimPrefix(s, "Enabled:"))
			state.enabled = strings.EqualFold(v, "yes")
			continue
		}
		if strings.HasPrefix(s, "Server:") {
			state.server = strings.TrimSpace(strings.TrimPrefix(s, "Server:"))
			continue
		}
		if strings.HasPrefix(s, "Port:") {
			p := strings.TrimSpace(strings.TrimPrefix(s, "Port:"))
			if n, convErr := strconv.Atoi(p); convErr == nil {
				state.port = n
			}
		}
	}
	return state, nil
}

// maybeWindowsSysProxyReconcile (darwin) self-heals a leaked loopback proxy.
// The supervisor calls this periodically and on resume/startup: if we're not in
// proxy mode yet a 127.0.0.1 proxy is still enabled (a previous session was
// force-quit / crashed before teardown), turn it off so direct traffic isn't
// blackholed. A legitimate proxy-mode lease is left alone.
func (a *App) maybeWindowsSysProxyReconcile() {
	a.mu.RLock()
	inProxy := a.state.Traffic == "proxy" && a.systemProxyLeased
	a.mu.RUnlock()
	if inProxy {
		return
	}
	darwinProxyReconcileOnce.Do(func() { _ = clearDarwinLoopbackProxy() })
}

func (a *App) handleMixedPortChangeForWindowsSysProxy(prevPort, newPort int) {
	_, _ = prevPort, newPort
}
