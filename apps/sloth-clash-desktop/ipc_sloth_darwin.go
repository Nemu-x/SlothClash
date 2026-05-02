//go:build darwin

package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"os/exec"
	"strings"
	"syscall"
	"time"
)

const (
	slothDarwinServiceSocket = "/tmp/slothclash/sloth-clash-service.sock"
	slothDarwinServiceID     = "dev.slothclash.desktop.ipc.service"
	slothIPCHeaderMagic      = "X-IPC-Magic"
	slothIPCAuthExpect       = `Like as the waves make towards the pebbl'd shore, So do our minutes hasten to their end;`
)

type ipcEnvelope struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
}

func ipcSlothServiceClient() *http.Client {
	return &http.Client{
		Transport: &http.Transport{
			DialContext: func(ctx context.Context, _, _ string) (net.Conn, error) {
				var d net.Dialer
				return d.DialContext(ctx, "unix", slothDarwinServiceSocket)
			},
			DisableKeepAlives: true,
		},
		Timeout: 30 * time.Second,
	}
}

func ipcSlothDo(ctx context.Context, method, path string, body []byte) (status int, bodyOut []byte, err error) {
	cli := ipcSlothServiceClient()
	var rdr io.Reader
	if len(body) > 0 {
		rdr = bytes.NewReader(body)
	}
	req, err := http.NewRequestWithContext(ctx, method, "http://sloth"+path, rdr)
	if err != nil {
		return 0, nil, err
	}
	req.Header.Set(slothIPCHeaderMagic, slothIPCAuthExpect)
	if len(body) > 0 {
		req.Header.Set("Content-Type", "application/json")
	}
	resp, err := cli.Do(req)
	if err != nil {
		return 0, nil, err
	}
	defer resp.Body.Close()
	b, err := io.ReadAll(io.LimitReader(resp.Body, 512*1024))
	return resp.StatusCode, b, err
}

func windowsEnsureSlothIPCReachable(ctx context.Context) error {
	var d net.Dialer
	tryDial := func(timeout time.Duration) error {
		dctx, cancel := context.WithTimeout(ctx, timeout)
		defer cancel()
		c, err := d.DialContext(dctx, "unix", slothDarwinServiceSocket)
		if err == nil {
			_ = c.Close()
			return nil
		}
		return err
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
		slothDarwinServiceSocket,
		slothDarwinServiceID,
		origErr,
		describeDarwinSocket(slothDarwinServiceSocket),
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

func describeDarwinSocket(p string) string {
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
		fmt.Sprintf("/bin/rm -f '%s' >/dev/null 2>&1 || true", esc(slothDarwinServiceSocket)),
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

func ipcSlothStartClash(ctx context.Context, p slothIPCStartParams) error {
	payload := map[string]any{
		"core_config": map[string]string{
			"core_path":     p.CorePath,
			"core_ipc_path": p.CoreIpcPath,
			"config_path":   p.ConfigPath,
			"config_dir":    p.ConfigDir,
		},
		"log_config": map[string]any{
			"directory":     p.LogDirectory,
			"max_log_size":  10 * 1024 * 1024,
			"max_log_files": 8,
		},
	}
	raw, err := json.Marshal(payload)
	if err != nil {
		return err
	}
	st, b, err := ipcSlothDo(ctx, http.MethodPost, "/clash/start", raw)
	if err != nil {
		return err
	}
	var env ipcEnvelope
	_ = json.Unmarshal(b, &env)
	if st < 200 || st >= 300 {
		if env.Message != "" {
			return fmt.Errorf("POST /clash/start: HTTP %d - %s", st, env.Message)
		}
		return fmt.Errorf("POST /clash/start: HTTP %d - %s", st, strings.TrimSpace(string(b)))
	}
	if env.Code != 0 {
		if env.Message != "" {
			return fmt.Errorf("start core via service: %s", env.Message)
		}
		return fmt.Errorf("start core via service: code %d", env.Code)
	}
	return nil
}

func ipcSlothStopCore(ctx context.Context) error {
	st, b, err := ipcSlothDo(ctx, http.MethodDelete, "/clash/stop", nil)
	if err != nil {
		return err
	}
	var env ipcEnvelope
	_ = json.Unmarshal(b, &env)
	if st < 200 || st >= 300 {
		if env.Message != "" {
			return fmt.Errorf("DELETE /clash/stop: HTTP %d - %s", st, env.Message)
		}
		return fmt.Errorf("DELETE /clash/stop: HTTP %d - %s", st, strings.TrimSpace(string(b)))
	}
	if env.Code != 0 {
		if env.Message != "" {
			return fmt.Errorf("stop core via service: %s", env.Message)
		}
		return fmt.Errorf("stop core via service: code %d", env.Code)
	}
	return nil
}
