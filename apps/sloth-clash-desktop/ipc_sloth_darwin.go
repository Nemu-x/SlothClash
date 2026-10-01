//go:build darwin

package main

// macOS-specific half of the unix-socket IPC: launchd kickstart and the
// privileged socket/launchd heal. The transport itself is in ipc_sloth_unix.go.

import (
	"context"
	"errors"
	"fmt"
	"net"
	"os"
	"os/exec"
	"strings"
	"syscall"
	"time"
)

const slothDarwinServiceID = "dev.slothclash.desktop.ipc.service"

func windowsEnsureSlothIPCReachable(ctx context.Context) error {
	tryDial := func(timeout time.Duration) error {
		return dialSlothServiceSocket(ctx, timeout)
	}

	origErr := tryDial(2 * time.Second)
	if origErr == nil {
		return nil
	}

	// Service might be installed but not running yet.
	kickCtx, kickCancel := context.WithTimeout(ctx, 8*time.Second)
	defer kickCancel()
	kickCmd := exec.CommandContext(kickCtx, "launchctl", "kickstart", "-k", "system/"+slothDarwinServiceID)
	_, _ = kickCmd.CombinedOutput()
	startCmd := exec.CommandContext(kickCtx, "launchctl", "start", slothDarwinServiceID)
	_, _ = startCmd.CombinedOutput()

	deadline := time.Now().Add(6 * time.Second)
	for time.Now().Before(deadline) {
		if err := tryDial(900 * time.Millisecond); err == nil {
			return nil
		}
		time.Sleep(260 * time.Millisecond)
	}

	// Common post-upgrade failure on macOS: stale root-owned socket with restrictive perms.
	// Attempt one privileged launchd/socket heal so users don't need manual service reinstall.
	// This runs `osascript … with administrator privileges` and shows a system password prompt.
	// It should be rare if the IPC service creates a world-writable socket; if it happens on
	// every cold boot, fix permissions in sloth-clash-service-ipc (launchd / umask / chmod).
	if isDarwinSocketAccessIssue(origErr) {
		if healErr := darwinHealServiceIPCWithPrivileges(ctx); healErr == nil {
			deadline2 := time.Now().Add(8 * time.Second)
			for time.Now().Before(deadline2) {
				if err := tryDial(900 * time.Millisecond); err == nil {
					return nil
				}
				time.Sleep(260 * time.Millisecond)
			}
		}
	}

	return fmt.Errorf(
		"Sloth IPC socket unreachable at %s (service id %s): %w [%s]",
		slothServiceSocketPath,
		slothDarwinServiceID,
		origErr,
		describeUnixSocket(slothServiceSocketPath),
	)
}

func isDarwinSocketAccessIssue(err error) bool {
	if err == nil {
		return false
	}
	if errors.Is(err, os.ErrPermission) {
		return true
	}
	var opErr *net.OpError
	if errors.As(err, &opErr) && opErr.Err != nil {
		if errors.Is(opErr.Err, os.ErrPermission) {
			return true
		}
	}
	msg := strings.ToLower(strings.TrimSpace(err.Error()))
	return strings.Contains(msg, "permission denied") || strings.Contains(msg, "operation not permitted")
}

func describeUnixSocket(p string) string {
	st, err := os.Stat(p)
	if err != nil {
		return "socket-stat=" + err.Error()
	}
	mode := st.Mode().String()
	sysPart := ""
	if sys, ok := st.Sys().(*syscall.Stat_t); ok {
		sysPart = fmt.Sprintf(" uid=%d gid=%d", sys.Uid, sys.Gid)
	}
	return fmt.Sprintf("socket-mode=%s%s", mode, sysPart)
}

func darwinHealServiceIPCWithPrivileges(ctx context.Context) error {
	hctx, cancel := context.WithTimeout(ctx, 25*time.Second)
	defer cancel()
	esc := func(s string) string { return strings.ReplaceAll(s, "'", "'\\''") }
	// bootout/bootstrap re-registers launchd job and avoids stale disabled/override state.
	cmdText := strings.Join([]string{
		fmt.Sprintf("/bin/launchctl bootout system/%s >/dev/null 2>&1 || true", esc(slothDarwinServiceID)),
		fmt.Sprintf("/bin/mkdir -p '%s' >/dev/null 2>&1 || true", esc("/tmp/slothclash")),
		fmt.Sprintf("/usr/sbin/chown root:wheel '%s' >/dev/null 2>&1 || true", esc("/tmp/slothclash")),
		fmt.Sprintf("/bin/chmod -N '%s' >/dev/null 2>&1 || true", esc("/tmp/slothclash")),
		fmt.Sprintf("/bin/chmod 1777 '%s' >/dev/null 2>&1 || true", esc("/tmp/slothclash")),
		fmt.Sprintf("/bin/rm -f '%s' >/dev/null 2>&1 || true", esc(slothServiceSocketPath)),
		fmt.Sprintf("/bin/launchctl bootstrap system '/Library/LaunchDaemons/%s.plist' >/dev/null 2>&1 || true", esc(slothDarwinServiceID)),
		fmt.Sprintf("/bin/launchctl kickstart -k system/%s", esc(slothDarwinServiceID)),
		"/bin/sleep 1",
		fmt.Sprintf("/bin/chmod -N '%s' >/dev/null 2>&1 || true", esc("/tmp/slothclash")),
		fmt.Sprintf("/bin/chmod 1777 '%s' >/dev/null 2>&1 || true", esc("/tmp/slothclash")),
		"/usr/bin/env i=0; while [ $i -lt 12 ]; do i=$((i+1)); /bin/chmod -N '/tmp/slothclash' >/dev/null 2>&1 || true; /bin/chmod 1777 '/tmp/slothclash' >/dev/null 2>&1 || true; /bin/chmod 0666 '/tmp/slothclash/sloth-clash-service.sock' >/dev/null 2>&1 || true; /bin/sleep 0.25; done",
	}, "; ")
	appleScript := fmt.Sprintf("do shell script %q with administrator privileges", cmdText)
	cmd := exec.CommandContext(hctx, "osascript", "-e", appleScript)
	out, err := cmd.CombinedOutput()
	if err != nil {
		msg := strings.TrimSpace(string(out))
		if msg == "" {
			msg = err.Error()
		}
		return errors.New(msg)
	}
	return nil
}
