package main

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/url"
	"os"
	"strings"
	"time"
)

// TestProxyDelay performs a one-shot delay test for a proxy node/group.
// It calls mihomo GET /proxies/{name}/delay?timeout=4000&url=...
func (a *App) TestProxyDelay(name string) (int, error) {
	name = strings.TrimSpace(name)
	if name == "" {
		return 0, errors.New("proxy name is required")
	}

	a.mu.RLock()
	conn := strings.TrimSpace(a.state.Connection.Status)
	running := a.state.Core.Running
	ep := a.effectiveCoreEndpointLocked()
	secret := a.coreSecret
	a.mu.RUnlock()

	if ep == "" || (conn != "connected" && !running) {
		base := strings.TrimSpace(os.Getenv("SLOTH_CLASH_CONTROLLER"))
		if base == "" {
			return 0, errors.New("connect Sloth or set SLOTH_CLASH_CONTROLLER for external core")
		}
		if !strings.HasPrefix(base, "http://") && !strings.HasPrefix(base, "https://") {
			base = "http://" + base
		}
		base = strings.TrimRight(base, "/")
		ep = strings.TrimPrefix(base, "http://")
		ep = strings.TrimPrefix(ep, "https://")
		secret = strings.TrimSpace(os.Getenv("SLOTH_CLASH_SECRET"))
	}

	q := url.Values{}
	// 10s (verge parity: default_latency_timeout=10000). A hysteria2/QUIC node's
	// first probe is a cold handshake (QUIC + obfs + masquerade + uTLS) that
	// routinely needs 4-8s over a long path — a 4s cap made working nodes show a
	// false 504. Cloudflare's generate_204 (verge default) is a more reliable exit
	// probe than gstatic, which some proxy exits throttle.
	q.Set("timeout", "10000")
	q.Set("url", "http://cp.cloudflare.com/generate_204")
	path := "/proxies/" + url.PathEscape(name) + "/delay?" + q.Encode()

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	resp, err := coreDoWithEndpoint(ctx, ep, secret, http.MethodGet, path, nil)
	if err != nil {
		return 0, err
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return 0, errors.New("delay failed: HTTP " + resp.Status)
	}
	var out struct {
		Delay int `json:"delay"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		return 0, err
	}
	if out.Delay <= 0 {
		return 0, errors.New("no delay value")
	}
	return out.Delay, nil
}
