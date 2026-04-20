//go:build darwin

package main

import (
	"fmt"
	"os/exec"
	"strconv"
	"strings"
)

func (a *App) applyWindowsSystemProxyIfNeededLocked() error {
	if a.state.Traffic != "proxy" {
		return nil
	}
	if a.state.Core.MixedPort <= 0 {
		return nil
	}
	addr := "127.0.0.1"
	port := a.state.Core.MixedPort
	if err := setDarwinSystemProxy(addr, port, true); err != nil {
		return err
	}
	a.systemProxyLeased = true
	return nil
}

func (a *App) applyWindowsSystemProxyFromSnapshot() error {
	a.mu.RLock()
	traffic := a.state.Traffic
	mixed := a.state.Core.MixedPort
	a.mu.RUnlock()
	if traffic != "proxy" || mixed <= 0 {
		return nil
	}
	if err := setDarwinSystemProxy("127.0.0.1", mixed, true); err != nil {
		return err
	}
	a.mu.Lock()
	a.systemProxyLeased = true
	a.mu.Unlock()
	return nil
}

func (a *App) clearWindowsSystemProxyFromSnapshot() error {
	a.mu.RLock()
	traffic := a.state.Traffic
	a.mu.RUnlock()
	if traffic == "proxy" {
		return nil
	}
	a.mu.Lock()
	a.clearWindowsSystemProxyLocked()
	a.mu.Unlock()
	return nil
}

func (a *App) clearWindowsSystemProxyLocked() {
	_ = setDarwinSystemProxy("", 0, false)
	a.systemProxyLeased = false
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
		} else {
			if err := runNetworksetup("-setwebproxystate", svc, "off"); err != nil {
				errs = append(errs, fmt.Sprintf("%s webstate: %v", svc, err))
			}
			if err := runNetworksetup("-setsecurewebproxystate", svc, "off"); err != nil {
				errs = append(errs, fmt.Sprintf("%s securestate: %v", svc, err))
			}
		}
	}
	if len(errs) > 0 {
		return fmt.Errorf(strings.Join(errs, "; "))
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

