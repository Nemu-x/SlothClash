//go:build darwin || linux

package main

// Unix-socket transport to the privileged helper service, shared by macOS
// (launchd) and Linux (systemd). The service listens on the same path on both
// (sloth-clash-service-ipc `IPC_PATH`); only reachability/heal differs per OS
// and lives in ipc_sloth_darwin.go / ipc_sloth_linux.go.

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"strings"
	"time"
)

const (
	slothServiceSocketPath = "/tmp/slothclash/sloth-clash-service.sock"
	slothIPCHeaderMagic    = "X-IPC-Magic"
	slothIPCAuthExpect     = `Like as the waves make towards the pebbl'd shore, So do our minutes hasten to their end;`
)

type ipcEnvelope struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
}

func ipcSlothServiceClient() *http.Client {
	return ipcSlothServiceClientTimeout(30 * time.Second)
}

// ipcSlothServiceClientTimeout builds a service client with an explicit overall
// timeout. Corp-VPN connects run up to ~35 s (OpenConnect handshake) and the
// service gives its handler 45 s, so the corp-start call needs a longer client
// timeout than the 30 s default or it would give up first and the connect would
// spuriously fail.
func ipcSlothServiceClientTimeout(d time.Duration) *http.Client {
	return &http.Client{
		Transport: &http.Transport{
			DialContext: func(ctx context.Context, _, _ string) (net.Conn, error) {
				var dl net.Dialer
				return dl.DialContext(ctx, "unix", slothServiceSocketPath)
			},
			DisableKeepAlives: true,
		},
		Timeout: d,
	}
}

func ipcSlothDo(ctx context.Context, method, path string, body []byte) (status int, bodyOut []byte, err error) {
	return ipcSlothDoWith(ipcSlothServiceClient(), ctx, method, path, body)
}

func ipcSlothDoWith(cli *http.Client, ctx context.Context, method, path string, body []byte) (status int, bodyOut []byte, err error) {
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

// dialSlothServiceSocket is the shared "is the helper up" probe.
func dialSlothServiceSocket(ctx context.Context, timeout time.Duration) error {
	var d net.Dialer
	dctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	c, err := d.DialContext(dctx, "unix", slothServiceSocketPath)
	if err == nil {
		_ = c.Close()
		return nil
	}
	return err
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

// ipcSlothRemoveTun is a no-op on unix: there is no wintun and mihomo tears its
// own utun/tun device down on stop. Present so cross-platform recovery code
// compiles.
func ipcSlothRemoveTun(ctx context.Context) (int, error) {
	_ = ctx
	return 0, nil
}

// Corp-VPN sidecar transport (macOS-only in P1; the Linux component spec
// reports unsupported so these are never reached there). They just relay to
// the privileged service, which owns the OpenConnect process.
func ipcSlothStartCorpVpn(ctx context.Context, payload []byte) (int, []byte, error) {
	// 55 s > the service's 45 s handler ceiling > OpenConnect's ~35 s connect, so
	// the client never gives up before the service resolves the connect.
	cli := ipcSlothServiceClientTimeout(55 * time.Second)
	return ipcSlothDoWith(cli, ctx, http.MethodPost, "/corp/start", payload)
}

func ipcSlothStopCorpVpn(ctx context.Context) (int, []byte, error) {
	return ipcSlothDo(ctx, http.MethodDelete, "/corp/stop", nil)
}

// ipcSlothEnsureCorpDriver is a no-op on unix (native tun needs no driver). The
// cross-platform caller gates on tapWindowsComponentSpec (Windows-only), so this
// is never reached; it exists only so the shared corp code compiles here.
func ipcSlothEnsureCorpDriver(_ context.Context, _ string) error { return nil }

func ipcSlothCorpVpnStatus(ctx context.Context) (int, []byte, error) {
	return ipcSlothDo(ctx, http.MethodGet, "/corp/status", nil)
}
