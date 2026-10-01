//go:build linux

package main

// Linux-specific half of the unix-socket IPC: systemd unit status and the
// reachability probe. The transport itself is in ipc_sloth_unix.go.
//
// Access model (set up by the elevated installer, see service_install_linux.go):
// the unit runs as root with `Group=<installing user's primary group>`; the
// service chowns /tmp/slothclash to that gid and binds the socket 0660, so the
// unprivileged GUI connects through group membership, nobody else can.

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"syscall"
	"time"
)

const slothLinuxServiceUnit = "sloth-clash-service"

func windowsEnsureSlothIPCReachable(ctx context.Context) error {
	origErr := dialSlothServiceSocket(ctx, 2*time.Second)
	if origErr == nil {
		return nil
	}

	// The unit is `Restart=always` + `enable --now`, so "not running" is the
	// rare case (first boot after install, or the user stopped it). A plain
	// `systemctl start` from an unprivileged process goes through polkit —
	// fine when an agent is present (prompt), fast failure when not — so one
	// attempt is cheap and never hangs.
	loaded, active := queryLinuxUnitState(ctx)
	if loaded && !active {
		kctx, cancel := context.WithTimeout(ctx, 8*time.Second)
		_, _ = exec.CommandContext(kctx, "systemctl", "start", slothLinuxServiceUnit+".service").CombinedOutput()
		cancel()
		deadline := time.Now().Add(6 * time.Second)
		for time.Now().Before(deadline) {
			if err := dialSlothServiceSocket(ctx, 900*time.Millisecond); err == nil {
				return nil
			}
			time.Sleep(260 * time.Millisecond)
		}
	}

	// Wording matters: isServiceUnreachableError keys the reinstall banner on
	// "socket unreachable".
	return fmt.Errorf(
		"Sloth service socket unreachable at %s (unit %s loaded=%v active=%v): %w [%s]",
		slothServiceSocketPath,
		slothLinuxServiceUnit,
		loaded,
		active,
		origErr,
		describeUnixSocket(slothServiceSocketPath),
	)
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

// queryLinuxUnitState asks systemd whether the unit file exists (loaded) and
// whether it is currently active. Works unprivileged; `systemctl show` never
// prompts.
func queryLinuxUnitState(ctx context.Context) (loaded, active bool) {
	qctx, cancel := context.WithTimeout(ctx, 4*time.Second)
	defer cancel()
	out, err := exec.CommandContext(qctx, "systemctl", "show", "-p", "LoadState,ActiveState", "--no-pager", slothLinuxServiceUnit+".service").CombinedOutput()
	if err != nil && len(out) == 0 {
		return false, false
	}
	return parseSystemctlShowState(string(out))
}

// queryLinuxServiceStatus is the refreshServiceStatus backend for Linux:
// installed = unit file present (systemd knows it, or the file is on disk),
// running = ActiveState active.
func queryLinuxServiceStatus() (installed bool, running bool, lastErr string) {
	loaded, active := queryLinuxUnitState(context.Background())
	if !loaded {
		if _, err := os.Stat("/etc/systemd/system/" + slothLinuxServiceUnit + ".service"); err == nil {
			loaded = true
		}
	}
	return loaded, loaded && active, ""
}
