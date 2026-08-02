package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"time"
)

// Corp-VPN coexistence lifecycle (corp-vpn-coexistence, task 4b).
//
// The privileged service owns the OpenConnect process; the desktop only
// orchestrates. Connect flow, in strict order so the two VPNs never fight:
//
//	1. ensure the OpenConnect component is present (on-demand download+verify)
//	2. POST /corp/start → service spawns OpenConnect, waits for tunnel-up, and
//	   reports the routes/DNS the gateway pushed (or asks to trust a cert first)
//	3. record the learned split and hot-reload mihomo so its TUN excludes the
//	   corp subnets and splits DNS for the corp domains
//
// Disconnect reverses it: stop the sidecar, clear the split, reload so the
// route-exclude/DNS overlay disappears.

// errCorpVpnUnsupported is returned on platforms where P1 does not ship the
// sidecar yet (everything but macOS).
var errCorpVpnUnsupported = errors.New("corporate VPN is only available on macOS in this release")

// CorpVpnStatus is the Wails-facing view of the sidecar state. camelCase JSON so
// the generated TS model reads naturally in the frontend.
type CorpVpnStatus struct {
	Connected        bool     `json:"connected"`
	NeedsCertTrust   bool     `json:"needsCertTrust"`
	ServercertSHA256 string   `json:"servercertSha256"`
	Routes           []string `json:"routes"`
	DNSServers       []string `json:"dnsServers"`
	DNSDomains       []string `json:"dnsDomains"`
	FullTunnel       bool     `json:"fullTunnel"`
	// Tundev is the corp tunnel interface (e.g. "utun4"); the DNS overlay binds
	// corp resolvers to it so their queries egress the corp tunnel.
	Tundev string `json:"tundev"`
	// GatewayIP is the gateway's public IP; excluded from mihomo's TUN so the
	// tunnel's own traffic to it goes out the physical NIC (not via the proxy).
	GatewayIP string `json:"gatewayIp"`
	// LogTail is recent OpenConnect stderr (most recent last), for the log view.
	LogTail []string `json:"logTail"`
	// Supported is false on platforms where the feature is not available yet, so
	// the UI can hide/disable the tab instead of surfacing errors.
	Supported bool `json:"supported"`
}

// corpVpnEnvelope mirrors the service's Response<CorpVpnStatusData> (snake_case).
type corpVpnEnvelope struct {
	Code    int              `json:"code"`
	Message string           `json:"message"`
	Data    *corpVpnDataWire `json:"data"`
}

type corpVpnDataWire struct {
	Connected        bool     `json:"connected"`
	NeedsCertTrust   bool     `json:"needs_cert_trust"`
	ServercertSHA256 string   `json:"servercert_sha256"`
	Routes           []string `json:"routes"`
	DNSServers       []string `json:"dns_servers"`
	DNSDomains       []string `json:"dns_domains"`
	FullTunnel       bool     `json:"full_tunnel"`
	Tundev           string   `json:"tundev"`
	GatewayIP        string   `json:"gateway_ip"`
	LogTail          []string `json:"log_tail"`
}

// corpStatusFromWire maps the service payload to the Wails-facing status. Pure.
func corpStatusFromWire(d *corpVpnDataWire) CorpVpnStatus {
	if d == nil {
		return CorpVpnStatus{Supported: true}
	}
	return CorpVpnStatus{
		Connected:        d.Connected,
		NeedsCertTrust:   d.NeedsCertTrust,
		ServercertSHA256: d.ServercertSHA256,
		Routes:           append([]string(nil), d.Routes...),
		DNSServers:       append([]string(nil), d.DNSServers...),
		DNSDomains:       append([]string(nil), d.DNSDomains...),
		FullTunnel:       d.FullTunnel,
		Tundev:           d.Tundev,
		GatewayIP:        d.GatewayIP,
		LogTail:          append([]string(nil), d.LogTail...),
		Supported:        true,
	}
}

// corpSplitFromStatus derives the config overlay from a connected status. A
// disconnected, cert-pending, or full-tunnel status yields an empty (inactive)
// split — there is nothing to exclude. Pure.
func corpSplitFromStatus(s CorpVpnStatus) corpVpnSplit {
	if !s.Connected || s.NeedsCertTrust || s.FullTunnel || len(s.Routes) == 0 {
		return corpVpnSplit{}
	}
	return corpVpnSplit{
		Routes:     append([]string(nil), s.Routes...),
		DNSServers: append([]string(nil), s.DNSServers...),
		DNSDomains: append([]string(nil), s.DNSDomains...),
		Tundev:     s.Tundev,
		GatewayIP:  s.GatewayIP,
	}
}

// parseCorpEnvelope decodes a service reply and turns transport/protocol errors
// into a Go error, otherwise returns the mapped status. A non-zero code that is
// specifically the cert-trust prompt is NOT an error — it carries the
// fingerprint for the user to accept. Pure (no I/O).
func parseCorpEnvelope(httpStatus int, body []byte) (CorpVpnStatus, error) {
	var env corpVpnEnvelope
	if len(body) > 0 {
		_ = json.Unmarshal(body, &env)
	}
	status := corpStatusFromWire(env.Data)
	if status.NeedsCertTrust {
		// Cert-trust prompt: surface the fingerprint, not an error.
		return status, nil
	}
	if httpStatus < 200 || httpStatus >= 300 {
		msg := strings.TrimSpace(env.Message)
		if msg == "" {
			msg = strings.TrimSpace(string(body))
		}
		return status, fmt.Errorf("corp vpn service: HTTP %d: %s", httpStatus, msg)
	}
	if env.Code != 0 {
		return status, fmt.Errorf("corp vpn service: %s", strings.TrimSpace(env.Message))
	}
	return status, nil
}

// StartCorpVpn brings up the corporate VPN sidecar. On a first connect against an
// untrusted server certificate it returns needsCertTrust=true with the
// fingerprint; the UI shows it and calls again with servercert set to accept it.
func (a *App) StartCorpVpn(gateway, username, password, servercert string) (CorpVpnStatus, error) {
	if _, ok := openconnectComponentSpec(); !ok {
		return CorpVpnStatus{Supported: false}, errCorpVpnUnsupported
	}
	gateway = strings.TrimSpace(gateway)
	username = strings.TrimSpace(username)
	if gateway == "" || username == "" {
		return CorpVpnStatus{Supported: true}, errors.New("gateway and username are required")
	}

	ctx := a.baseCtx()
	spec, _ := openconnectComponentSpec()
	binPath, err := ensureComponent(ctx, spec, a.componentHTTPClient())
	if err != nil {
		return CorpVpnStatus{Supported: true}, fmt.Errorf("prepare OpenConnect: %w", err)
	}

	payload, err := json.Marshal(map[string]any{
		"bin_path":   binPath,
		"gateway":    gateway,
		"username":   username,
		"password":   password,
		"servercert": strings.TrimSpace(servercert),
	})
	if err != nil {
		return CorpVpnStatus{Supported: true}, err
	}

	startCtx, cancel := context.WithTimeout(ctx, 60*time.Second)
	defer cancel()
	httpStatus, body, err := ipcSlothStartCorpVpn(startCtx, payload)
	if err != nil {
		return CorpVpnStatus{Supported: true}, fmt.Errorf("call corp vpn service: %w", err)
	}
	status, err := parseCorpEnvelope(httpStatus, body)
	if err != nil {
		return status, err
	}

	// Remember server + username (never the password) so the form pre-fills next
	// time. Save once we've gotten far enough that the inputs are known-good
	// (connected or a cert-trust prompt — both mean the gateway/user were valid).
	saveCorpVpnCredentials(gateway, username)

	// Cert-trust round trip: nothing to route yet.
	if status.NeedsCertTrust {
		return status, nil
	}

	// Learned the split → record it and hot-reload mihomo so the exclude/DNS
	// overlay lands, flushing live sockets so existing corp-bound connections
	// re-route immediately (same mechanism as a rules change). Thread the gateway
	// host so the overlay can keep it on public DNS (else the corp DNS policy
	// would capture the gateway and deadlock the next connect).
	split := corpSplitFromStatus(status)
	split.Gateway = gateway
	a.applyCorpSplitAndReload(split)
	return status, nil
}

// StopCorpVpn tears the sidecar down and reloads mihomo without the overlay.
func (a *App) StopCorpVpn() error {
	if _, ok := openconnectComponentSpec(); !ok {
		return errCorpVpnUnsupported
	}
	ctx, cancel := context.WithTimeout(a.baseCtx(), 20*time.Second)
	defer cancel()
	_, _, err := ipcSlothStopCorpVpn(ctx)
	// Clear the split and reload regardless of the stop result: a failed stop
	// still means we should not keep excluding corp routes the sidecar may no
	// longer serve.
	a.applyCorpSplitAndReload(corpVpnSplit{})
	if err != nil {
		return fmt.Errorf("stop corp vpn service: %w", err)
	}
	return nil
}

// GetCorpVpnCredentials returns the remembered corp server + username (never a
// password) so the UI can pre-fill the login form.
func (a *App) GetCorpVpnCredentials() CorpVpnCredentials {
	_ = a
	return currentDesktopPrefs().CorpVpn
}

// ForgetCorpVpnCredentials clears the remembered corp server + username (used
// when the user unticks "remember").
func (a *App) ForgetCorpVpnCredentials() {
	_ = a
	prefsMu.Lock()
	prefsCurrent.CorpVpn = CorpVpnCredentials{}
	snapshot := prefsCurrent
	savePrefsBestEffort(snapshot)
	prefsMu.Unlock()
}

// saveCorpVpnCredentials persists the corp server + username (never the password).
func saveCorpVpnCredentials(gateway, username string) {
	gateway = strings.TrimSpace(gateway)
	username = strings.TrimSpace(username)
	if gateway == "" && username == "" {
		return
	}
	prefsMu.Lock()
	prefsCurrent.CorpVpn = CorpVpnCredentials{Gateway: gateway, Username: username}
	snapshot := prefsCurrent
	savePrefsBestEffort(snapshot)
	prefsMu.Unlock()
}

// GetCorpVpnStatus reports the current sidecar state (for the UI to poll/refresh).
func (a *App) GetCorpVpnStatus() (CorpVpnStatus, error) {
	if _, ok := openconnectComponentSpec(); !ok {
		return CorpVpnStatus{Supported: false}, nil
	}
	ctx, cancel := context.WithTimeout(a.baseCtx(), 10*time.Second)
	defer cancel()
	httpStatus, body, err := ipcSlothCorpVpnStatus(ctx)
	if err != nil {
		return CorpVpnStatus{Supported: true}, fmt.Errorf("query corp vpn service: %w", err)
	}
	status, perr := parseCorpEnvelope(httpStatus, body)
	// Self-heal: if the sidecar is down but we still hold a corp overlay (e.g.
	// OpenConnect dropped on its own, not via Disconnect), clear it and reload so
	// the corp DNS policy / route-exclude don't linger pointing at a dead tunnel —
	// which would otherwise make the whole corp domain (incl. the gateway)
	// unresolvable and block the next connect. Fires once per drop: after the
	// clear, currentCorpVpnSplit is inactive.
	if perr == nil && !status.Connected && !status.NeedsCertTrust && currentCorpVpnSplit().active() {
		a.applyCorpSplitAndReload(corpVpnSplit{})
	}
	return status, perr
}

// applyCorpSplitAndReload records the split for the config overlay and triggers a
// hot-reload so it takes effect. The reload is a no-op when mihomo is not
// connected in TUN mode; the split is still stored so the next Connect applies it.
//
// Deliberately does NOT flush live connections. New routes/DNS apply to new
// connections, and existing mihomo-proxied sessions (the browser's keep-alives,
// HTTP/2, etc.) keep working. Flushing here was actively harmful: on a flaky corp
// tunnel the self-heal path fired repeatedly and nuked all browser connections
// each time, so the terminal (fresh sockets) worked while the browser (reused
// sockets) appeared dead. A corp connect/disconnect is not a routing-policy
// change that needs live traffic torn down.
func (a *App) applyCorpSplitAndReload(split corpVpnSplit) {
	setCorpVpnSplit(split)
	a.mu.RLock()
	activeID := strings.TrimSpace(a.state.Profile.ActiveProfileID)
	connected := a.state.Connection.Status == "connected"
	traffic := a.state.Traffic
	a.mu.RUnlock()
	if activeID == "" || !connected || traffic != "tun" {
		return
	}
	go a.reconnectActiveProfile()
}

// baseCtx returns the app context when available, falling back to Background so
// the corp calls work even before the Wails runtime has attached a.ctx.
func (a *App) baseCtx() context.Context {
	if a.ctx != nil {
		return a.ctx
	}
	return context.Background()
}

// componentHTTPClient returns the client used to download the OpenConnect
// component. When mihomo is connected it routes through the local mixed-port
// proxy so the fetch resolves and reaches github.com even on networks that
// can't do it directly (the exact "lookup github.com: no such host" a user hit)
// — the proxy does the DNS remotely. When the core is down it falls back to a
// direct client.
func (a *App) componentHTTPClient() *http.Client {
	a.mu.RLock()
	connected := a.state.Connection.Status == "connected"
	port := a.state.Core.MixedPort
	a.mu.RUnlock()
	if connected && port > 0 {
		if u, err := url.Parse(fmt.Sprintf("http://127.0.0.1:%d", port)); err == nil {
			return &http.Client{
				Transport: &http.Transport{Proxy: http.ProxyURL(u)},
			}
		}
	}
	return http.DefaultClient
}
