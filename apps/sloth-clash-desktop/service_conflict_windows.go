//go:build windows

package main

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"os/exec"
	"strings"
	"time"
)

var conflictingTunServiceNames = []string{
	"clash_verge_service",
	"FlClashHelperService",
}

// skipTunTakeoverByEnv: set SLOTH_SKIP_TUN_TAKEOVER=1 to experiment without stopping
// other VPN services (Clash Verge, FlClash). TUN may still conflict at the OS level.
func skipTunTakeoverByEnv() bool {
	v := strings.ToLower(strings.TrimSpace(os.Getenv("SLOTH_SKIP_TUN_TAKEOVER")))
	return v == "1" || v == "true" || v == "yes"
}

func scHidden(args ...string) ([]byte, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, system32Exe("sc.exe"), args...)
	if attr := hideWindowSysProcAttr(); attr != nil {
		cmd.SysProcAttr = attr
	}
	return cmd.CombinedOutput()
}

func parseScQueryState(out []byte) (string, bool) {
	s := string(bytes.ToUpper(out))
	if strings.Contains(s, "FAILED") && strings.Contains(s, "1060") {
		// ERROR_SERVICE_DOES_NOT_EXIST
		return "ABSENT", true
	}
	for _, line := range strings.Split(string(out), "\n") {
		line = strings.TrimSpace(line)
		if !strings.HasPrefix(strings.ToUpper(line), "STATE") {
			continue
		}
		_, after, ok := strings.Cut(line, ":")
		if !ok {
			continue
		}
		u := strings.ToUpper(strings.TrimSpace(after))
		switch {
		case strings.Contains(u, "RUNNING"):
			return "RUNNING", true
		case strings.Contains(u, "STOPPED"):
			return "STOPPED", true
		case strings.Contains(u, "STOP_PENDING"):
			return "STOP_PENDING", true
		case strings.Contains(u, "START_PENDING"):
			return "START_PENDING", true
		case strings.Contains(u, "PAUSED"):
			return "PAUSED", true
		case strings.Contains(u, "PAUSE_PENDING"):
			return "PAUSE_PENDING", true
		case strings.Contains(u, "CONTINUE_PENDING"):
			return "CONTINUE_PENDING", true
		}
	}
	return "", false
}

func queryServiceState(name string) (string, bool) {
	out, err := scHidden("query", name)
	if err != nil {
		return "", false
	}
	return parseScQueryState(out)
}

func parseScQcStartTypeLine(out []byte) string {
	for _, line := range strings.Split(string(out), "\n") {
		line = strings.TrimSpace(line)
		if !strings.HasPrefix(strings.ToUpper(line), "START_TYPE") {
			continue
		}
		_, after, ok := strings.Cut(line, ":")
		if !ok {
			return ""
		}
		fields := strings.Fields(strings.TrimSpace(after))
		if len(fields) < 2 {
			return ""
		}
		// e.g. "2  AUTO_START" or "4  DISABLED"
		return strings.ToUpper(strings.TrimSpace(fields[1]))
	}
	return ""
}

func scStartTypeToConfigArg(startType string) string {
	t := strings.ToUpper(strings.TrimSpace(startType))
	t = strings.ReplaceAll(t, "-", "_")
	t = strings.ReplaceAll(t, " ", "_")
	switch t {
	case "AUTO_START", "DELAYED_AUTO_START":
		return "auto"
	case "BOOT_START":
		return "boot"
	case "SYSTEM_START":
		return "system"
	case "DEMAND_START":
		return "demand"
	case "DISABLED":
		return "disabled"
	default:
		return ""
	}
}

func setServiceStartType(name, startArg string) error {
	if strings.TrimSpace(startArg) == "" {
		return nil
	}
	out, err := scHidden("config", name, "start=", startArg)
	if err != nil {
		return fmt.Errorf("sc config %s start=%s: %w (%s)", name, startArg, err, strings.TrimSpace(string(out)))
	}
	return nil
}

func stopServiceRobust(name string, prevStartFromQC string) error {
	const stopWait = 12 * time.Second
	// Give watchdogs less chance to immediately restart while we tear TUN down.
	prevStart := strings.ToUpper(strings.TrimSpace(prevStartFromQC))
	if prevStart == "" {
		if out, err := scHidden("qc", name); err == nil {
			prevStart = parseScQcStartTypeLine(out)
		}
	}
	if prevStart != "" && prevStart != "DEMAND_START" {
		_ = setServiceStartType(name, "demand")
	}

	for attempt := 0; attempt < 2; attempt++ {
		if attempt > 0 {
			time.Sleep(900 * time.Millisecond)
		}
		out, err := scHidden("stop", name)
		_ = out
		_ = err

		deadline := time.Now().Add(stopWait)
		for time.Now().Before(deadline) {
			st, ok := queryServiceState(name)
			if !ok {
				break
			}
			switch st {
			case "STOPPED", "ABSENT":
				if prevStart != "" && prevStart != "DEMAND_START" {
					if arg := scStartTypeToConfigArg(prevStart); arg != "" {
						_ = setServiceStartType(name, arg)
					}
				}
				return nil
			case "STOP_PENDING", "PAUSED", "PAUSE_PENDING", "CONTINUE_PENDING", "START_PENDING":
				time.Sleep(400 * time.Millisecond)
				continue
			case "RUNNING":
				// Still running after a stop attempt; retry outer loop.
				break
			}
			time.Sleep(400 * time.Millisecond)
		}
	}

	st, _ := queryServiceState(name)
	if prevStart != "" && prevStart != "DEMAND_START" {
		if arg := scStartTypeToConfigArg(prevStart); arg != "" {
			_ = setServiceStartType(name, arg)
		}
	}
	if st == "" {
		st = "UNKNOWN"
	}
	return fmt.Errorf("still %s after stop attempts (may need admin rights, or another app is restarting the service)", st)
}

func takeoverConflictingTunServices() ([]tunServiceTakeover, error) {
	if skipTunTakeoverByEnv() {
		return nil, nil
	}
	var running []string
	for _, name := range conflictingTunServiceNames {
		state, ok := queryServiceState(name)
		if ok && state == "RUNNING" {
			running = append(running, name)
		}
	}
	if len(running) == 0 {
		return nil, nil
	}
	var stopped []tunServiceTakeover
	for _, name := range running {
		prevStart := ""
		if out, err := scHidden("qc", name); err == nil {
			prevStart = parseScQcStartTypeLine(out)
		}
		if err := stopServiceRobust(name, prevStart); err != nil {
			return stopped, fmt.Errorf("failed to stop conflicting service %s: %w", name, err)
		}
		stopped = append(stopped, tunServiceTakeover{Name: name, PrevStartScLine: prevStart})
	}
	return stopped, nil
}

func (a *App) restoreTakenOverTunServicesLocked() {
	if len(a.tunTakenOver) == 0 {
		return
	}
	for _, t := range a.tunTakenOver {
		name := strings.TrimSpace(t.Name)
		if name == "" {
			continue
		}
		if arg := scStartTypeToConfigArg(t.PrevStartScLine); arg != "" {
			_ = setServiceStartType(name, arg)
		}
		_, _ = scHidden("start", name)
	}
	a.tunTakenOver = nil
}
