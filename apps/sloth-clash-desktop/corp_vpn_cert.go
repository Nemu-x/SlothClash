package main

import (
	"context"
	"crypto/sha256"
	"crypto/tls"
	"crypto/x509"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"net/url"
	"os"
	"regexp"
	"strings"
	"time"
)

// Corp-VPN certificate trust (pinning, rotation, structured errors).
//
// Trust model: OpenConnect does the TLS verification. With no pin it validates
// the gateway against the CA store of its TLS backend; with `--servercert <pin>`
// it accepts ONLY a certificate matching the pin. When verification fails,
// OpenConnect prints the observed pin and exits — the app surfaces it in a
// confirmation dialog and reconnects with the new pin only after the user
// explicitly trusts it. Nothing here ever accepts a certificate on its own.

// Structured error codes shared with the frontend (it localizes them). The Go
// error string is "<CODE>: <detail>" so the code survives the Wails boundary.
const (
	corpErrCertChanged          = "CERTIFICATE_CHANGED"
	corpErrCertUntrusted        = "CERTIFICATE_UNTRUSTED"
	corpErrCertInvalid          = "CERTIFICATE_INVALID"
	corpErrCertHostnameMismatch = "CERTIFICATE_HOSTNAME_MISMATCH"
	corpErrCertExpired          = "CERTIFICATE_EXPIRED"
	corpErrInvalidPin           = "INVALID_PIN"
	corpErrAuthFailed           = "VPN_AUTH_FAILED"
	corpErrConnectFailed        = "VPN_CONNECT_FAILED"
	corpErrBusy                 = "VPN_CONNECT_IN_PROGRESS"
	corpErrServiceUnavailable   = "SERVICE_UNAVAILABLE"
	corpErrServiceOutdated      = "SERVICE_OUTDATED"
	corpErrServiceError         = "SERVICE_ERROR"
)

// corpVpnError carries a structured code across the Wails boundary.
type corpVpnError struct {
	Code   string
	Detail string
}

func (e *corpVpnError) Error() string {
	if e.Detail == "" {
		return e.Code
	}
	return e.Code + ": " + e.Detail
}

// corpErrorCode extracts the structured code from an error ("" when untyped).
func corpErrorCode(err error) string {
	var ce *corpVpnError
	if errors.As(err, &ce) {
		return ce.Code
	}
	return ""
}

// CorpVpnCertInfo describes the certificate the gateway presents, for the trust
// dialog. Informational only: it is gathered by a separate TLS probe and never
// grants trust — OpenConnect's own verification (pin or CA) is authoritative.
type CorpVpnCertInfo struct {
	Host      string `json:"host"`
	Subject   string `json:"subject"`
	Issuer    string `json:"issuer"`
	NotBefore string `json:"notBefore"` // RFC 3339
	NotAfter  string `json:"notAfter"`  // RFC 3339
	// SHA256Fingerprint is the SHA-256 of the full DER certificate (colon hex).
	SHA256Fingerprint string `json:"sha256Fingerprint"`
	// SPKIPin is OpenConnect's `pin-sha256:` form: base64(SHA-256(SubjectPublicKeyInfo)).
	SPKIPin string `json:"spkiPin"`
	// ChainTrusted: the chain verifies against this user's system trust store.
	// Note OpenConnect runs inside the privileged service (SYSTEM on Windows) and
	// uses its own TLS backend, so this is a hint for the user, not a guarantee.
	ChainTrusted    bool   `json:"chainTrusted"`
	ChainError      string `json:"chainError,omitempty"`
	HostnameMatches bool   `json:"hostnameMatches"`
	HostnameError   string `json:"hostnameError,omitempty"`
	Expired         bool   `json:"expired"`
	// MatchesReported: the probe saw the same key OpenConnect reported. False
	// means the two connections were shown DIFFERENT certificates — a red flag.
	MatchesReported bool   `json:"matchesReported"`
	ProbeError      string `json:"probeError,omitempty"`
}

var (
	reCorpPinB64 = regexp.MustCompile(`pin-sha256:([A-Za-z0-9+/]{43}=)`)
	reCorpPinHex = regexp.MustCompile(`(?i)(?:^|[^-a-z])sha256:([0-9a-f]{64})`)
	// OpenConnect (gnutls.c/openssl.c): "None of the %d fingerprint(s) specified
	// via --servercert match server's certificate: %s".
	reCorpPinMismatch = regexp.MustCompile(`(?i)none of the \d+ fingerprint\(s\) specified via --servercert match`)
	// OpenConnect --non-inter: "Certificate from VPN server "%s" failed
	// verification.\nReason: %s\n…--servercert %s".
	reCorpVerifyFailed = regexp.MustCompile(`(?i)failed verification|certificate verify failed`)
	reCorpReason       = regexp.MustCompile(`(?i)reason:\s*([^\n\r]+)`)
)

// normalizeCorpPin validates a `--servercert` value and returns its canonical
// form. Accepted: `pin-sha256:<base64 of a 32-byte digest>` (SPKI pin; what our
// GnuTLS Windows build prints) and `sha256:<64 hex>` (OpenSSL builds). Empty
// stays empty (= no pin, CA validation). Anything else is rejected so garbage
// never reaches OpenConnect's argv.
func normalizeCorpPin(raw string) (string, error) {
	s := strings.TrimSpace(raw)
	if s == "" {
		return "", nil
	}
	lower := strings.ToLower(s)
	switch {
	case strings.HasPrefix(lower, "pin-sha256:"):
		b64 := s[len("pin-sha256:"):]
		sum, err := base64.StdEncoding.Strict().DecodeString(b64)
		if err != nil || len(sum) != sha256.Size {
			return "", fmt.Errorf("malformed pin-sha256 value (want base64 of a 32-byte SHA-256)")
		}
		return "pin-sha256:" + base64.StdEncoding.EncodeToString(sum), nil
	case strings.HasPrefix(lower, "sha256:"):
		h := lower[len("sha256:"):]
		sum, err := hex.DecodeString(h)
		if err != nil || len(sum) != sha256.Size {
			return "", fmt.Errorf("malformed sha256 value (want 64 hex characters)")
		}
		return "sha256:" + h, nil
	default:
		return "", fmt.Errorf("unsupported certificate pin format (want pin-sha256:… or sha256:…)")
	}
}

// extractCorpPin finds the server pin OpenConnect printed, skipping `skip` (the
// pin we configured, so it is never mistaken for the observed one). Base64 form
// first: it contains the substring "sha256:".
func extractCorpPin(text, skip string) string {
	for _, m := range reCorpPinB64.FindAllStringSubmatch(text, -1) {
		if p := "pin-sha256:" + m[1]; p != skip {
			return p
		}
	}
	for _, m := range reCorpPinHex.FindAllStringSubmatch(text, -1) {
		if p := "sha256:" + strings.ToLower(m[1]); p != skip {
			return p
		}
	}
	return ""
}

// classifyCorpCertReason maps OpenConnect's "Reason:" text (GnuTLS/OpenSSL
// wording) to a structured code.
func classifyCorpCertReason(reason string) string {
	r := strings.ToLower(reason)
	switch {
	case r == "":
		return corpErrCertUntrusted
	case strings.Contains(r, "expired"), strings.Contains(r, "not yet activated"), strings.Contains(r, "not yet valid"):
		return corpErrCertExpired
	case strings.Contains(r, "hostname"), strings.Contains(r, "host name"):
		return corpErrCertHostnameMismatch
	case strings.Contains(r, "signer not found"), strings.Contains(r, "not a ca"),
		strings.Contains(r, "issuer"), strings.Contains(r, "self-signed"), strings.Contains(r, "self signed"):
		return corpErrCertUntrusted
	default:
		return corpErrCertInvalid
	}
}

// corpCertTrustFromFailure recognizes a certificate-verification failure in a
// service error message and turns it into a trust prompt. The CHANGED case is
// the cert-rotation bug: with a stale `--servercert`, OpenConnect refuses the
// renewed certificate and the old service reports a plain failure. Pure.
func corpCertTrustFromFailure(msg, configuredPin string) (CorpVpnStatus, bool) {
	observed := extractCorpPin(msg, configuredPin)
	if observed == "" {
		return CorpVpnStatus{}, false
	}
	st := CorpVpnStatus{Supported: true, NeedsCertTrust: true, ServercertSHA256: observed}
	switch {
	case configuredPin != "" && (reCorpPinMismatch.MatchString(msg) || strings.Contains(msg, "--servercert")):
		st.CertError = corpErrCertChanged
		st.PreviousServercert = configuredPin
	case reCorpVerifyFailed.MatchString(msg):
		if m := reCorpReason.FindStringSubmatch(msg); m != nil {
			st.CertReason = strings.TrimSpace(m[1])
		}
		st.CertError = classifyCorpCertReason(st.CertReason)
	default:
		return CorpVpnStatus{}, false
	}
	return st, true
}

// classifyCorpFailure maps a non-certificate service failure to a code. The
// service wraps OpenConnect failures ("openconnect exited…", "timed out…") — those
// came from the gateway side; anything else is a local service problem.
func classifyCorpFailure(msg string) string {
	m := strings.ToLower(msg)
	switch {
	case strings.Contains(m, "login failed"), strings.Contains(m, "authentication failed"),
		strings.Contains(m, "auth-failed"), strings.Contains(m, "invalid username or password"),
		strings.Contains(m, "failed to obtain webvpn cookie"), strings.Contains(m, "user input required in non-interactive mode"):
		return corpErrAuthFailed
	case strings.Contains(m, "openconnect exited"), strings.Contains(m, "timed out waiting for the tunnel"):
		return corpErrConnectFailed
	default:
		return corpErrServiceError
	}
}

// resolveCorpServercert picks the pin for this connect. An explicit argument
// (the user just trusted a certificate) wins; otherwise the remembered pin is
// re-read from prefs.json so the file on disk is the source of truth. The app
// lives in the tray for weeks, and a pin held only in memory once made a
// renewed gateway certificate unfixable: deleting/replacing the pin in
// prefs.json had no effect until the process exited. A malformed explicit pin
// is an error; a malformed remembered pin is dropped (→ CA validation / trust
// prompt, never silent trust).
func resolveCorpServercert(arg string) (pin, source string, err error) {
	if strings.TrimSpace(arg) != "" {
		p, err := normalizeCorpPin(arg)
		if err != nil {
			return "", "", &corpVpnError{Code: corpErrInvalidPin, Detail: err.Error()}
		}
		return p, "explicit", nil
	}
	saved := reloadCorpVpnServercertFromDisk()
	if saved == "" {
		return "", "none", nil
	}
	p, err := normalizeCorpPin(saved)
	if err != nil {
		debugLog("corp", "CERT", "corp_vpn_cert.go", "dropping malformed saved pin",
			map[string]any{"error": err.Error()})
		forgetCorpVpnServercert()
		return "", "none", nil
	}
	return p, "saved", nil
}

// reloadCorpVpnServercertFromDisk syncs the in-memory pin with prefs.json and
// returns it. Falls back to memory only when the file can't be read/parsed.
func reloadCorpVpnServercertFromDisk() string {
	p, err := prefsStorePath()
	if err != nil {
		return strings.TrimSpace(currentDesktopPrefs().CorpVpn.Servercert)
	}
	prefsMu.Lock()
	defer prefsMu.Unlock()
	b, err := os.ReadFile(p)
	if err != nil || len(b) == 0 {
		return strings.TrimSpace(prefsCurrent.CorpVpn.Servercert)
	}
	var disk desktopPrefsFile
	if err := json.Unmarshal(b, &disk); err != nil {
		return strings.TrimSpace(prefsCurrent.CorpVpn.Servercert)
	}
	prefsCurrent.CorpVpn.Servercert = strings.TrimSpace(disk.CorpVpn.Servercert)
	return prefsCurrent.CorpVpn.Servercert
}

// forgetCorpVpnServercert drops the remembered pin (memory + disk).
func forgetCorpVpnServercert() {
	prefsMu.Lock()
	prefsCurrent.CorpVpn.Servercert = ""
	snapshot := prefsCurrent
	savePrefsBestEffort(snapshot)
	prefsMu.Unlock()
}

// buildCorpStartPayload is the /corp/start request body. An empty pin is sent
// as "" — the service then omits `--servercert` entirely (CA validation).
func buildCorpStartPayload(binPath, gateway, username, password, pin string) ([]byte, error) {
	return json.Marshal(map[string]any{
		"bin_path":   binPath,
		"gateway":    gateway,
		"username":   username,
		"password":   password,
		"servercert": pin,
	})
}

// corpGatewayAddr turns the gateway the user typed (host, host:port, or URL with
// a path/usergroup) into the TLS dial target and SNI host.
func corpGatewayAddr(gateway string) (host, addr string, err error) {
	g := strings.TrimSpace(gateway)
	if !strings.Contains(g, "://") {
		g = "https://" + g
	}
	u, err := url.Parse(g)
	if err != nil || u.Hostname() == "" {
		return "", "", fmt.Errorf("invalid gateway %q", gateway)
	}
	port := u.Port()
	if port == "" {
		port = "443"
	}
	return u.Hostname(), net.JoinHostPort(u.Hostname(), port), nil
}

// Test seams for the certificate probe.
var (
	corpCertRoots *x509.CertPool // nil = system trust store
	corpCertNow   = time.Now
	corpCertDial  = func(ctx context.Context, addr string, cfg *tls.Config) (*tls.Conn, error) {
		d := &tls.Dialer{NetDialer: &net.Dialer{Timeout: 8 * time.Second}, Config: cfg}
		c, err := d.DialContext(ctx, "tcp", addr)
		if err != nil {
			return nil, err
		}
		return c.(*tls.Conn), nil
	}
)

// inspectCorpCert fetches the gateway's certificate for DISPLAY and evaluates it
// the standard way (system roots + hostname + validity). The handshake skips Go's
// verification only so an untrusted certificate can still be shown; the result
// never feeds a trust decision and no data is sent over the connection.
func inspectCorpCert(ctx context.Context, gateway, reportedPin string) CorpVpnCertInfo {
	host, addr, err := corpGatewayAddr(gateway)
	info := CorpVpnCertInfo{Host: host}
	if err != nil {
		info.ProbeError = err.Error()
		return info
	}
	ctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	conn, err := corpCertDial(ctx, addr, &tls.Config{
		ServerName:         host,
		InsecureSkipVerify: true, //nolint:gosec // inspection only: the cert is displayed, never trusted; verified manually below
		MinVersion:         tls.VersionTLS12,
	})
	if err != nil {
		info.ProbeError = err.Error()
		return info
	}
	certs := conn.ConnectionState().PeerCertificates
	_ = conn.Close()
	if len(certs) == 0 {
		info.ProbeError = "server presented no certificate"
		return info
	}
	return describeCorpCert(host, certs, reportedPin)
}

// describeCorpCert evaluates a presented chain. Pure (given the seams).
func describeCorpCert(host string, certs []*x509.Certificate, reportedPin string) CorpVpnCertInfo {
	leaf := certs[0]
	der := sha256.Sum256(leaf.Raw)
	spki := sha256.Sum256(leaf.RawSubjectPublicKeyInfo)
	now := corpCertNow()
	info := CorpVpnCertInfo{
		Host:              host,
		Subject:           leaf.Subject.String(),
		Issuer:            leaf.Issuer.String(),
		NotBefore:         leaf.NotBefore.UTC().Format(time.RFC3339),
		NotAfter:          leaf.NotAfter.UTC().Format(time.RFC3339),
		SHA256Fingerprint: colonHex(der[:]),
		SPKIPin:           "pin-sha256:" + base64.StdEncoding.EncodeToString(spki[:]),
		Expired:           now.Before(leaf.NotBefore) || now.After(leaf.NotAfter),
	}
	inter := x509.NewCertPool()
	for _, c := range certs[1:] {
		inter.AddCert(c)
	}
	if _, err := leaf.Verify(x509.VerifyOptions{Roots: corpCertRoots, Intermediates: inter, CurrentTime: now}); err != nil {
		info.ChainError = err.Error()
	} else {
		info.ChainTrusted = true
	}
	if err := leaf.VerifyHostname(host); err != nil {
		info.HostnameError = err.Error()
	} else {
		info.HostnameMatches = true
	}
	switch rp := strings.ToLower(reportedPin); {
	case strings.HasPrefix(rp, "pin-sha256:"):
		info.MatchesReported = reportedPin == info.SPKIPin
	case strings.HasPrefix(rp, "sha256:"):
		h := rp[len("sha256:"):]
		info.MatchesReported = h == hex.EncodeToString(der[:]) || h == hex.EncodeToString(spki[:])
	}
	return info
}

func colonHex(b []byte) string {
	h := strings.ToUpper(hex.EncodeToString(b))
	var sb strings.Builder
	for i := 0; i < len(h); i += 2 {
		if i > 0 {
			sb.WriteByte(':')
		}
		sb.WriteString(h[i : i+2])
	}
	return sb.String()
}

// refineCorpCertError sharpens an UNTRUSTED code with what the probe saw
// (expired / wrong hostname), when OpenConnect gave no specific reason. A
// CHANGED code is kept: rotation is the headline the user must decide on.
func refineCorpCertError(code string, info CorpVpnCertInfo) string {
	if code != corpErrCertUntrusted || info.ProbeError != "" {
		return code
	}
	switch {
	case info.Expired:
		return corpErrCertExpired
	case info.Subject != "" && !info.HostnameMatches:
		return corpErrCertHostnameMismatch
	}
	return code
}
