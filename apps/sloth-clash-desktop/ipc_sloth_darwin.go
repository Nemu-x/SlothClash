//go:build darwin

package main

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"os/exec"
	"strings"
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
	dctx, cancel := context.WithTimeout(ctx, 2*time.Second)
	defer cancel()
	var d net.Dialer
	c, err := d.DialContext(dctx, "unix", slothDarwinServiceSocket)
	if err == nil {
		_ = c.Close()
		return nil
	}

	// Service might be installed but not running yet.
	kickCtx, kickCancel := context.WithTimeout(ctx, 8*time.Second)
	defer kickCancel()
	kickCmd := exec.CommandContext(kickCtx, "launchctl", "kickstart", "-k", "system/"+slothDarwinServiceID)
	_, _ = kickCmd.CombinedOutput()
	startCmd := exec.CommandContext(kickCtx, "launchctl", "start", slothDarwinServiceID)
	_, _ = startCmd.CombinedOutput()

	rctx, rcancel := context.WithTimeout(ctx, 6*time.Second)
	defer rcancel()
	deadline := time.Now().Add(6 * time.Second)
	for time.Now().Before(deadline) {
		c2, err2 := d.DialContext(rctx, "unix", slothDarwinServiceSocket)
		if err2 == nil {
			_ = c2.Close()
			return nil
		}
		time.Sleep(260 * time.Millisecond)
	}
	return fmt.Errorf("Sloth IPC socket unreachable at %s (service id %s): %w", slothDarwinServiceSocket, slothDarwinServiceID, err)
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

