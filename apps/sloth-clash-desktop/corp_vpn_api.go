package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"SlothClashDesktop/internal/runtimecfg"

	wailsrt "github.com/wailsapp/wails/v2/pkg/runtime"
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
var errCorpVpnUnsupported = errors.New("corporate VPN is not available on this platform")

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
	// Certificate-trust prompt details (only with NeedsCertTrust). CertError is a
	// corpErrCert* code; CERTIFICATE_CHANGED means a previously trusted pin no
	// longer matches (PreviousServercert) — rotation or interception.
	CertError          string           `json:"certError,omitempty"`
	CertReason         string           `json:"certReason,omitempty"`
	PreviousServercert string           `json:"previousServercert,omitempty"`
	Certificate        *CorpVpnCertInfo `json:"certificate,omitempty"`
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
	// Service >= 2.6.0 classifies certificate failures itself; older services
	// leave these empty and the desktop classifies from the message.
	CertError          string `json:"cert_error"`
	CertReason         string `json:"cert_reason"`
	PreviousServercert string `json:"previous_servercert"`
}

// corpStatusFromWire maps the service payload to the Wails-facing status. Pure.
func corpStatusFromWire(d *corpVpnDataWire) CorpVpnStatus {
	if d == nil {
		return CorpVpnStatus{Supported: true}
	}
	return CorpVpnStatus{
		Connected:          d.Connected,
		NeedsCertTrust:     d.NeedsCertTrust,
		ServercertSHA256:   d.ServercertSHA256,
		Routes:             append([]string(nil), d.Routes...),
		DNSServers:         append([]string(nil), d.DNSServers...),
		DNSDomains:         append([]string(nil), d.DNSDomains...),
		FullTunnel:         d.FullTunnel,
		Tundev:             d.Tundev,
		GatewayIP:          d.GatewayIP,
		LogTail:            append([]string(nil), d.LogTail...),
		Supported:          true,
		CertError:          d.CertError,
		CertReason:         d.CertReason,
		PreviousServercert: d.PreviousServercert,
	}
}

// corpSplitFromStatus derives the config overlay from a connected status. A
// disconnected, cert-pending, or full-tunnel status yields an empty (inactive)
// split — there is nothing to exclude. Pure.
func corpSplitFromStatus(s CorpVpnStatus) runtimecfg.CorpVpnSplit {
	if !s.Connected || s.NeedsCertTrust || s.FullTunnel || len(s.Routes) == 0 {
		return runtimecfg.CorpVpnSplit{}
	}
	return runtimecfg.CorpVpnSplit{
		Routes:     append([]string(nil), s.Routes...),
		DNSServers: append([]string(nil), s.DNSServers...),
		DNSDomains: append([]string(nil), s.DNSDomains...),
		Tundev:     s.Tundev,
		GatewayIP:  s.GatewayIP,
	}
}

// parseCorpEnvelope decodes a service reply and turns transport/protocol errors
// into a structured *corpVpnError, otherwise returns the mapped status. A
// certificate-verification failure is NOT an error — it becomes a trust prompt
// carrying the observed pin (and, after a rotation, the previous one).
// configuredPin is the `--servercert` this request sent ("" for status polls).
// Pure (no I/O).
func parseCorpEnvelope(httpStatus int, body []byte, configuredPin string) (CorpVpnStatus, error) {
	var env corpVpnEnvelope
	if len(body) > 0 {
		_ = json.Unmarshal(body, &env)
	}
	status := corpStatusFromWire(env.Data)
	if status.NeedsCertTrust {
		if status.CertError == "" {
			status.CertError = corpErrCertUntrusted
			if configuredPin != "" {
				status.CertError = corpErrCertChanged
			}
		}
		if status.CertError == corpErrCertChanged && status.PreviousServercert == "" {
			status.PreviousServercert = configuredPin
		}
		return status, nil
	}
	// An old service (< 2.7.0) has no /corp/* routes and answers 404. That is not a
	// connect failure — it means the privileged helper predates the corporate-VPN
	// feature, so give the actionable remedy instead of a raw "HTTP 404". The
	// global reinstall banner (expectedSlothServiceVersion) nudges the same fix.
	if httpStatus == http.StatusNotFound {
		return status, &corpVpnError{Code: corpErrServiceOutdated, Detail: "the Sloth service is outdated and does not support Corporate VPN — reinstall the service (Settings → reinstall) to update it"}
	}
	if httpStatus < 200 || httpStatus >= 300 || env.Code != 0 {
		msg := strings.TrimSpace(env.Message)
		if msg == "" {
			msg = strings.TrimSpace(string(body))
		}
		// The local service's HTTP status describes the IPC call, not the
		// gateway; classify by what OpenConnect reported instead of "HTTP 503".
		if cs, ok := corpCertTrustFromFailure(msg, configuredPin); ok {
			return cs, nil
		}
		return status, &corpVpnError{Code: classifyCorpFailure(msg), Detail: msg}
	}
	return status, nil
}

// corpStartMu serializes connects: a second click while one is in flight is
// refused instead of racing the first (the service would tear it down).
var corpStartMu sync.Mutex

// StartCorpVpn brings up the corporate VPN sidecar. When the gateway certificate
// fails verification it returns needsCertTrust=true with the observed pin and
// certificate details (certError says why: untrusted, or CHANGED vs the pin we
// trusted before); the UI asks the user and calls again with servercert set to
// the pin they explicitly trusted. Never trusts a certificate on its own.
func (a *App) StartCorpVpn(gateway, username, password, servercert string) (CorpVpnStatus, error) {
	if _, ok := openconnectComponentSpec(); !ok {
		return CorpVpnStatus{Supported: false}, errCorpVpnUnsupported
	}
	gateway = strings.TrimSpace(gateway)
	username = strings.TrimSpace(username)
	if gateway == "" || username == "" {
		return CorpVpnStatus{Supported: true}, errors.New("gateway and username are required")
	}
	if !corpStartMu.TryLock() {
		return CorpVpnStatus{Supported: true}, &corpVpnError{Code: corpErrBusy, Detail: "a corporate VPN connect is already in progress"}
	}
	defer corpStartMu.Unlock()

	// Trust-on-first-use: an explicit pin (just approved by the user) wins, else
	// the remembered one — re-read from prefs.json, see resolveCorpServercert.
	pin, pinSource, err := resolveCorpServercert(servercert)
	if err != nil {
		return CorpVpnStatus{Supported: true}, err
	}

	ctx := a.baseCtx()
	a.emitCorpPhase("preparing")
	spec, _ := openconnectComponentSpec()
	binPath, err := a.ensureCorpComponent(ctx, spec)
	if err != nil {
		return CorpVpnStatus{Supported: true}, fmt.Errorf("prepare OpenConnect: %w", err)
	}

	// On Windows OpenConnect runs on the TAP-Windows driver (mihomo owns wintun,
	// and two wintun users collide), so install it before the first connect.
	// No-op on macOS/Linux (native tun). Auto — no extra step for the user.
	a.emitCorpPhase("driver")
	if err := a.ensureCorpDriver(ctx); err != nil {
		return CorpVpnStatus{Supported: true}, fmt.Errorf("prepare corp driver: %w", err)
	}
	a.emitCorpPhase("connecting")

	payload, err := buildCorpStartPayload(binPath, gateway, username, password, pin)
	if err != nil {
		return CorpVpnStatus{Supported: true}, err
	}
	// Diagnostics: pins are public fingerprints; never log password/cookies.
	debugLog("corp", "CERT", "corp_vpn_api.go", "corp start",
		corpDiagFields("connect", gateway, pinSource, pin, "", ""))

	startCtx, cancel := context.WithTimeout(ctx, 60*time.Second)
	defer cancel()
	httpStatus, body, err := ipcSlothStartCorpVpn(startCtx, payload)
	if err != nil {
		return CorpVpnStatus{Supported: true}, &corpVpnError{Code: corpErrServiceUnavailable, Detail: fmt.Sprintf("call corp vpn service: %v", err)}
	}
	status, err := parseCorpEnvelope(httpStatus, body, pin)
	if err != nil {
		code := corpErrorCode(err)
		// An auth failure means TLS already accepted the pin the user just
		// approved — keep it, so a password retry doesn't re-prompt the cert.
		if code == corpErrAuthFailed && pinSource == "explicit" {
			saveCorpVpnServercert(pin)
		}
		debugLog("corp", "CERT", "corp_vpn_api.go", "corp start failed",
			corpDiagFields("result", gateway, pinSource, pin, "", code))
		return status, err
	}

	// Remember server + username (never the password) so the form pre-fills next
	// time. Save once we've gotten far enough that the inputs are known-good
	// (connected or a cert-trust prompt — both mean the gateway/user were valid).
	saveCorpVpnCredentials(gateway, username)

	// Cert-trust round trip: nothing to route yet. Attach what the certificate
	// looks like so the user can verify it before trusting.
	if status.NeedsCertTrust {
		info := inspectCorpCert(ctx, gateway, status.ServercertSHA256)
		status.Certificate = &info
		status.CertError = refineCorpCertError(status.CertError, info)
		debugLog("corp", "CERT", "corp_vpn_api.go", "corp cert trust required",
			corpDiagFields("tls", gateway, pinSource, pin, status.ServercertSHA256, status.CertError))
		return status, nil
	}

	// Connected against a pinned certificate → remember the pin so future
	// connects skip the trust prompt entirely (trust-on-first-use).
	if pin != "" {
		saveCorpVpnServercert(pin)
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

// corpDiagFields is the debug-log payload for a corp connect step. It takes only
// non-secret inputs by construction (there is no password/cookie parameter).
func corpDiagFields(stage, gateway, pinSource, configuredPin, observedPin, code string) map[string]any {
	return map[string]any{
		"stage":         stage,
		"gateway":       gateway,
		"pinSource":     pinSource,
		"configuredPin": configuredPin,
		"observedPin":   observedPin,
		"code":          code,
	}
}

// ForgetCorpVpnServercert drops the remembered certificate pin, so the next
// connect validates against the CA store (or asks to trust the certificate).
func (a *App) ForgetCorpVpnServercert() CorpVpnCredentials {
	_ = a
	forgetCorpVpnServercert()
	return currentDesktopPrefs().CorpVpn
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
	a.applyCorpSplitAndReload(runtimecfg.CorpVpnSplit{})
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

// saveCorpVpnCredentials persists the corp server + username (never the
// password). Updates only those fields so a previously remembered cert pin
// survives.
func saveCorpVpnCredentials(gateway, username string) {
	gateway = strings.TrimSpace(gateway)
	username = strings.TrimSpace(username)
	if gateway == "" && username == "" {
		return
	}
	prefsMu.Lock()
	prefsCurrent.CorpVpn.Gateway = gateway
	prefsCurrent.CorpVpn.Username = username
	snapshot := prefsCurrent
	savePrefsBestEffort(snapshot)
	prefsMu.Unlock()
}

// saveCorpVpnServercert remembers the trusted certificate pin from a successful
// connect so trust-on-first-use only prompts once.
func saveCorpVpnServercert(pin string) {
	pin = strings.TrimSpace(pin)
	if pin == "" {
		return
	}
	prefsMu.Lock()
	prefsCurrent.CorpVpn.Servercert = pin
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
	status, perr := parseCorpEnvelope(httpStatus, body, "")
	// Self-heal: if the sidecar is down but we still hold a corp overlay (e.g.
	// OpenConnect dropped on its own, not via Disconnect), clear it and reload so
	// the corp DNS policy / route-exclude don't linger pointing at a dead tunnel —
	// which would otherwise make the whole corp domain (incl. the gateway)
	// unresolvable and block the next connect. Fires once per drop: after the
	// clear, currentCorpVpnSplit is inactive.
	if perr == nil && !status.Connected && !status.NeedsCertTrust && currentCorpVpnSplit().Active() {
		a.applyCorpSplitAndReload(runtimecfg.CorpVpnSplit{})
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
func (a *App) applyCorpSplitAndReload(split runtimecfg.CorpVpnSplit) {
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

// emitCorpPhase tells the UI which step of the connect is running ("preparing" →
// "driver" → "connecting"), so the user sees live progress instead of a frozen
// "Connecting…" and doesn't spam the button. Best-effort; no-op before the Wails
// runtime has attached a.ctx.
func (a *App) emitCorpPhase(phase string) {
	if a.ctx == nil {
		return
	}
	wailsrt.EventsEmit(a.ctx, "corp:phase", phase)
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

// ensureCorpDriver installs the corp tunnel driver on platforms that need one
// (Windows: TAP-Windows, so OpenConnect runs on TAP instead of colliding with
// mihomo's wintun). Fetches the SHA-verified driver component on demand, then
// asks the privileged service to install it (idempotent). No-op on macOS/Linux
// (native tun → tapWindowsComponentSpec ok=false).
func (a *App) ensureCorpDriver(ctx context.Context) error {
	spec, ok := tapWindowsComponentSpec()
	if !ok {
		return nil
	}
	driverPath, err := a.ensureCorpComponent(ctx, spec)
	if err != nil {
		return fmt.Errorf("fetch tap-windows driver: %w", err)
	}
	return ipcSlothEnsureCorpDriver(ctx, filepath.Dir(driverPath))
}

// ensureCorpComponent fetches a corp component, trying the proxy-aware client
// first (works on networks that can't resolve github.com directly) and falling
// back to a direct client if the proxy path fails — e.g. mihomo is down or its
// TUN is stuck, so the local mixed-port proxy refuses. Without the fallback a
// flaky core would block the corp download entirely (the "proxyconnect refused"
// error users hit while the TUN adapter was wedged).
func (a *App) ensureCorpComponent(ctx context.Context, spec componentSpec) (string, error) {
	proxyCli := a.componentHTTPClient()
	path, err := ensureComponent(ctx, spec, proxyCli)
	if err == nil {
		return path, nil
	}
	// Only worth retrying if the first attempt actually went through the proxy.
	if proxyCli != http.DefaultClient {
		if p2, e2 := ensureComponent(ctx, spec, http.DefaultClient); e2 == nil {
			return p2, nil
		}
	}
	return "", err
}

var (
	corpVpnStateMu sync.RWMutex
	corpVpnState   runtimecfg.CorpVpnSplit
)

// setCorpVpnSplit records the split the running corp sidecar reported (called by
// the lifecycle orchestration on connect; cleared on disconnect). The next
// config reload picks it up via currentCorpVpnSplit.
func setCorpVpnSplit(s runtimecfg.CorpVpnSplit) {
	corpVpnStateMu.Lock()
	corpVpnState = s
	corpVpnStateMu.Unlock()
}

// currentCorpVpnSplit returns the active corp split (empty when the sidecar is
// down), captured into the generation env on every reload
// (currentRuntimeConfigEnv).
func currentCorpVpnSplit() runtimecfg.CorpVpnSplit {
	corpVpnStateMu.RLock()
	defer corpVpnStateMu.RUnlock()
	return corpVpnState
}
