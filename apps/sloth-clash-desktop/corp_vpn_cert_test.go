package main

import (
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/sha256"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/base64"
	"encoding/json"
	"math/big"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
	"time"
)

// Real pins from the vpn.skoltech.ru rotation (public fingerprints).
const (
	pinOld = "pin-sha256:MCDJgwuGvXvv80C45Q2mYbX4C334x7+82GyRERoyFCI="
	pinNew = "pin-sha256:zaTdo6UzzBdQ8q9j/dX1wPikc0LbMPI93JrWPkUXHGw="
)

// The exact failure the old (2.5.0) service returned after the gateway renewed
// its certificate while a stale pin was configured.
const rotationFailureBody = `{"code":1,"message":"start corp vpn: openconnect exited before the tunnel came up: None of the 1 fingerprint(s) specified via --servercert match server's certificate: pin-sha256:zaTdo6UzzBdQ8q9j/dX1wPikc0LbMPI93JrWPkUXHGw=\nSSL connection failure: Error in the certificate.\nFailed to open HTTPS connection to vpn.skoltech.ru","data":null}`

// withCorpPrefs isolates prefs (memory + prefs.json) for one test.
func withCorpPrefs(t *testing.T) string {
	t.Helper()
	p, err := prefsStorePath()
	if err != nil {
		t.Fatal(err)
	}
	prevDisk, readErr := os.ReadFile(p)
	prefsMu.Lock()
	previous := prefsCurrent
	prefsMu.Unlock()
	t.Cleanup(func() {
		prefsMu.Lock()
		prefsCurrent = previous
		prefsMu.Unlock()
		if readErr == nil {
			_ = os.WriteFile(p, prevDisk, 0o644)
		} else {
			_ = os.Remove(p)
		}
	})
	return p
}

func writeCorpPrefsFile(t *testing.T, path string, prefs DesktopPrefs) {
	t.Helper()
	b, err := json.Marshal(desktopPrefsFile{DesktopPrefs: prefs})
	if err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(strings.TrimSuffix(path, slothPrefsFile), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, b, 0o644); err != nil {
		t.Fatal(err)
	}
}

func TestNormalizeCorpPin(t *testing.T) {
	hexPin := "sha256:" + strings.Repeat("AB", 32)
	cases := []struct {
		in, want string
		ok       bool
	}{
		{"", "", true},
		{"  " + pinNew + " ", pinNew, true},
		{"PIN-SHA256:" + strings.TrimPrefix(pinNew, "pin-sha256:"), pinNew, true},
		{hexPin, strings.ToLower(hexPin), true},
		{"pin-sha256:MCDJgwuGvXvv80C45Q2mYbX4C334x7+82GyRERoyFC", "", false}, // truncated
		{"pin-sha256:" + base64.StdEncoding.EncodeToString(make([]byte, 20)), "", false},
		{"sha256:" + strings.Repeat("a", 63), "", false},
		{"sha256:" + strings.Repeat("g", 64), "", false},
		{"sha1:" + strings.Repeat("a", 40), "", false},
		{"--no-cert-check", "", false},
		{"MCDJgwuGvXvv80C45Q2mYbX4C334x7+82GyRERoyFCI=", "", false}, // no prefix
	}
	for _, c := range cases {
		got, err := normalizeCorpPin(c.in)
		if (err == nil) != c.ok || got != c.want {
			t.Errorf("normalizeCorpPin(%q) = %q, %v; want %q ok=%v", c.in, got, err, c.want, c.ok)
		}
	}
}

// Regression: the rotation failure must become a CERTIFICATE_CHANGED trust
// prompt (old + new pin), not a generic "HTTP 503" error.
func TestParseCorpEnvelope_StalePinBecomesCertificateChanged(t *testing.T) {
	st, err := parseCorpEnvelope(503, []byte(rotationFailureBody), pinOld)
	if err != nil {
		t.Fatalf("cert rotation must be a trust prompt, got error %v", err)
	}
	if !st.NeedsCertTrust || st.Connected {
		t.Fatalf("want needsCertTrust && !connected, got %#v", st)
	}
	if st.CertError != corpErrCertChanged || st.ServercertSHA256 != pinNew || st.PreviousServercert != pinOld {
		t.Fatalf("bad CHANGED prompt: %#v", st)
	}
}

// The newer service classifies itself; the desktop must keep its fields.
func TestParseCorpEnvelope_StructuredServiceChanged(t *testing.T) {
	body := `{"code":0,"message":"Success","data":{"connected":false,"needs_cert_trust":true,"servercert_sha256":"` + pinNew + `","cert_error":"CERTIFICATE_CHANGED","previous_servercert":"` + pinOld + `"}}`
	st, err := parseCorpEnvelope(200, []byte(body), pinOld)
	if err != nil || st.CertError != corpErrCertChanged || st.PreviousServercert != pinOld || st.ServercertSHA256 != pinNew {
		t.Fatalf("got %#v, %v", st, err)
	}
}

// First connect without a pin: the old service already returns a trust prompt.
func TestParseCorpEnvelope_FirstUseIsUntrustedPrompt(t *testing.T) {
	body := `{"code":0,"message":"Success","data":{"connected":false,"needs_cert_trust":true,"servercert_sha256":"` + pinNew + `"}}`
	st, err := parseCorpEnvelope(200, []byte(body), "")
	if err != nil || st.CertError != corpErrCertUntrusted || st.PreviousServercert != "" {
		t.Fatalf("got %#v, %v", st, err)
	}
}

func TestParseCorpEnvelope_CertReasonsClassified(t *testing.T) {
	cases := map[string]string{
		"signer not found":                    corpErrCertUntrusted,
		"certificate does not match hostname": corpErrCertHostnameMismatch,
		"certificate expired":                 corpErrCertExpired,
		"certificate not yet activated":       corpErrCertExpired,
		"insecure algorithm":                  corpErrCertInvalid,
	}
	for reason, want := range cases {
		msg := "start corp vpn: openconnect exited before the tunnel came up: Certificate from VPN server \"vpn.corp.test\" failed verification.\nReason: " + reason + "\nTo trust this server in future, perhaps add this to your command line:\n    --servercert " + pinNew + "\n"
		body, _ := json.Marshal(map[string]any{"code": 1, "message": msg})
		st, err := parseCorpEnvelope(503, body, "")
		if err != nil || !st.NeedsCertTrust || st.CertError != want || st.CertReason != reason {
			t.Errorf("%q: got %#v, %v; want %s", reason, st, err, want)
		}
	}
}

// No silent trust: non-certificate failures stay errors, with codes, and never
// carry a pin to accept.
func TestParseCorpEnvelope_FailuresAreTypedErrors(t *testing.T) {
	cases := map[string]string{
		"start corp vpn: openconnect exited before the tunnel came up: Login failed.":             corpErrAuthFailed,
		"start corp vpn: openconnect exited before the tunnel came up: Failed to connect to host": corpErrConnectFailed,
		"start corp vpn: corp-vpn: timed out waiting for the tunnel to come up":                   corpErrConnectFailed,
		"start corp vpn: openconnect binary not found at C:\\x":                                   corpErrServiceError,
	}
	for msg, want := range cases {
		body, _ := json.Marshal(map[string]any{"code": 1, "message": msg})
		st, err := parseCorpEnvelope(503, body, pinOld)
		if corpErrorCode(err) != want || st.NeedsCertTrust || st.Connected {
			t.Errorf("%q: got %#v, %v; want %s", msg, st, err, want)
		}
		if strings.Contains(err.Error(), "HTTP 503") {
			t.Errorf("%q: local IPC status leaked into the message: %v", msg, err)
		}
	}
	if _, err := parseCorpEnvelope(404, nil, ""); corpErrorCode(err) != corpErrServiceOutdated {
		t.Fatalf("404 → %v", err)
	}
}

// Regression for the reported bug: the pin was deleted from prefs.json while the
// app kept running (tray), yet the stale in-memory pin kept being sent.
func TestResolveCorpServercert_AbsentOnDiskSendsNoPin(t *testing.T) {
	path := withCorpPrefs(t)
	prefsMu.Lock()
	prefsCurrent.CorpVpn = CorpVpnCredentials{Gateway: "vpn.skoltech.ru", Username: "u", Servercert: pinOld}
	prefsMu.Unlock()
	writeCorpPrefsFile(t, path, DesktopPrefs{CorpVpn: CorpVpnCredentials{Gateway: "vpn.skoltech.ru", Username: "u"}})

	pin, source, err := resolveCorpServercert("")
	if err != nil || pin != "" || source != "none" {
		t.Fatalf("got pin=%q source=%q err=%v; want no pin", pin, source, err)
	}
	if got := currentDesktopPrefs().CorpVpn.Servercert; got != "" {
		t.Fatalf("stale pin survived in memory: %q", got)
	}
	payload, _ := buildCorpStartPayload("oc.exe", "vpn.skoltech.ru", "u", "pw", pin)
	var m map[string]any
	_ = json.Unmarshal(payload, &m)
	if m["servercert"] != "" {
		t.Fatalf("payload servercert = %v, want empty (service then omits --servercert)", m["servercert"])
	}
}

func TestResolveCorpServercert_Sources(t *testing.T) {
	path := withCorpPrefs(t)
	writeCorpPrefsFile(t, path, DesktopPrefs{CorpVpn: CorpVpnCredentials{Servercert: pinNew}})
	if pin, src, err := resolveCorpServercert(""); err != nil || pin != pinNew || src != "saved" {
		t.Fatalf("saved: %q %q %v", pin, src, err)
	}
	if pin, src, err := resolveCorpServercert(pinOld); err != nil || pin != pinOld || src != "explicit" {
		t.Fatalf("explicit: %q %q %v", pin, src, err)
	}
	if _, _, err := resolveCorpServercert("pin-sha256:garbage"); corpErrorCode(err) != corpErrInvalidPin {
		t.Fatalf("malformed explicit: %v", err)
	}
	// A malformed remembered pin is dropped (→ CA validation / prompt), not sent.
	writeCorpPrefsFile(t, path, DesktopPrefs{CorpVpn: CorpVpnCredentials{Servercert: "sha256:nothex"}})
	if pin, src, err := resolveCorpServercert(""); err != nil || pin != "" || src != "none" {
		t.Fatalf("malformed saved: %q %q %v", pin, src, err)
	}
	if b, _ := os.ReadFile(path); strings.Contains(string(b), "nothex") {
		t.Fatal("malformed pin not removed from disk")
	}
}

// Trust decision survives an app restart and is what the next connect sends;
// the old pin is never restored.
func TestCorpServercert_ApprovedPinPersistsAcrossRestart(t *testing.T) {
	path := withCorpPrefs(t)
	writeCorpPrefsFile(t, path, DesktopPrefs{CorpVpn: CorpVpnCredentials{Gateway: "vpn.corp.test", Servercert: pinOld}})
	loadDesktopPrefs()
	saveCorpVpnServercert(pinNew) // user trusted the rotated cert and connected
	saveCorpVpnCredentials("vpn.corp.test", "u")

	prefsMu.Lock()
	prefsCurrent = DesktopPrefs{} // process restart
	prefsMu.Unlock()
	loadDesktopPrefs()
	if pin, _, _ := resolveCorpServercert(""); pin != pinNew {
		t.Fatalf("after restart pin = %q, want %q", pin, pinNew)
	}
	forgetCorpVpnServercert()
	loadDesktopPrefs()
	if pin, _, _ := resolveCorpServercert(""); pin != "" {
		t.Fatalf("forget did not stick: %q", pin)
	}
	if got := currentDesktopPrefs().CorpVpn.Gateway; got != "vpn.corp.test" {
		t.Fatalf("forgetting the pin must keep the gateway, got %q", got)
	}
}

func TestCorpDiagFields_NoSecrets(t *testing.T) {
	f := corpDiagFields("tls", "vpn.corp.test", "saved", pinOld, pinNew, corpErrCertChanged)
	b, _ := json.Marshal(f)
	for _, k := range []string{"password", "passwd", "cookie", "token"} {
		if strings.Contains(strings.ToLower(string(b)), k) {
			t.Fatalf("diagnostics contain %q: %s", k, b)
		}
	}
}

func TestCorpGatewayAddr(t *testing.T) {
	cases := map[string]string{
		"vpn.skoltech.ru":                 "vpn.skoltech.ru:443",
		"https://vpn.corp.test/employees": "vpn.corp.test:443",
		"vpn.corp.test:8443":              "vpn.corp.test:8443",
	}
	for in, want := range cases {
		if _, addr, err := corpGatewayAddr(in); err != nil || addr != want {
			t.Errorf("%q → %q, %v; want %q", in, addr, err, want)
		}
	}
}

// --- certificate probe against a local corporate-CA fixture ---

type corpCA struct {
	cert *x509.Certificate
	key  *ecdsa.PrivateKey
	pool *x509.CertPool
}

func newCorpCA(t *testing.T) corpCA {
	t.Helper()
	key, _ := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	tmpl := &x509.Certificate{
		SerialNumber:          big.NewInt(1),
		Subject:               pkix.Name{CommonName: "corp-Root CA"},
		NotBefore:             time.Now().Add(-time.Hour),
		NotAfter:              time.Now().Add(24 * time.Hour),
		IsCA:                  true,
		KeyUsage:              x509.KeyUsageCertSign,
		BasicConstraintsValid: true,
	}
	der, err := x509.CreateCertificate(rand.Reader, tmpl, tmpl, &key.PublicKey, key)
	if err != nil {
		t.Fatal(err)
	}
	c, _ := x509.ParseCertificate(der)
	pool := x509.NewCertPool()
	pool.AddCert(c)
	return corpCA{cert: c, key: key, pool: pool}
}

func (ca corpCA) leaf(t *testing.T, host string) tls.Certificate {
	t.Helper()
	key, _ := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	tmpl := &x509.Certificate{
		SerialNumber: big.NewInt(2),
		Subject:      pkix.Name{CommonName: host},
		DNSNames:     []string{host},
		NotBefore:    time.Now().Add(-time.Hour),
		NotAfter:     time.Now().Add(12 * time.Hour),
		KeyUsage:     x509.KeyUsageDigitalSignature,
		ExtKeyUsage:  []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth},
	}
	der, err := x509.CreateCertificate(rand.Reader, tmpl, ca.cert, &key.PublicKey, ca.key)
	if err != nil {
		t.Fatal(err)
	}
	leaf, _ := x509.ParseCertificate(der)
	return tls.Certificate{Certificate: [][]byte{der}, PrivateKey: key, Leaf: leaf}
}

func spkiPinOf(c *x509.Certificate) string {
	sum := sha256.Sum256(c.RawSubjectPublicKeyInfo)
	return "pin-sha256:" + base64.StdEncoding.EncodeToString(sum[:])
}

// startCorpGateway serves the leaf over TLS and points the probe's dialer at it.
func startCorpGateway(t *testing.T, cert tls.Certificate, roots *x509.CertPool) {
	t.Helper()
	srv := httptest.NewUnstartedServer(http.NotFoundHandler())
	srv.TLS = &tls.Config{Certificates: []tls.Certificate{cert}}
	srv.StartTLS()
	prevDial, prevRoots, prevNow := corpCertDial, corpCertRoots, corpCertNow
	t.Cleanup(func() { srv.Close(); corpCertDial, corpCertRoots, corpCertNow = prevDial, prevRoots, prevNow })
	addr := srv.Listener.Addr().String()
	corpCertRoots = roots
	corpCertDial = func(ctx context.Context, _ string, cfg *tls.Config) (*tls.Conn, error) {
		raw, err := (&net.Dialer{}).DialContext(ctx, "tcp", addr)
		if err != nil {
			return nil, err
		}
		c := tls.Client(raw, cfg)
		if err := c.HandshakeContext(ctx); err != nil {
			return nil, err
		}
		return c, nil
	}
}

func TestInspectCorpCert_InternalCATrusted(t *testing.T) {
	ca := newCorpCA(t)
	cert := ca.leaf(t, "vpn.corp.test")
	startCorpGateway(t, cert, ca.pool)
	pin := spkiPinOf(cert.Leaf)
	info := inspectCorpCert(context.Background(), "vpn.corp.test", pin)
	if info.ProbeError != "" || !info.ChainTrusted || !info.HostnameMatches || info.Expired {
		t.Fatalf("got %#v", info)
	}
	if info.SPKIPin != pin || !info.MatchesReported || !strings.Contains(info.Issuer, "corp-Root CA") {
		t.Fatalf("pin/issuer: %#v", info)
	}
	if len(strings.Split(info.SHA256Fingerprint, ":")) != 32 {
		t.Fatalf("fingerprint %q", info.SHA256Fingerprint)
	}
}

func TestInspectCorpCert_UntrustedCA(t *testing.T) {
	ca := newCorpCA(t)
	cert := ca.leaf(t, "vpn.corp.test")
	startCorpGateway(t, cert, x509.NewCertPool()) // CA not installed
	info := inspectCorpCert(context.Background(), "vpn.corp.test", pinOld)
	if info.ChainTrusted || info.ChainError == "" || !info.HostnameMatches {
		t.Fatalf("got %#v", info)
	}
	if info.MatchesReported {
		t.Fatal("a different reported pin must not match")
	}
	if refineCorpCertError(corpErrCertUntrusted, info) != corpErrCertUntrusted {
		t.Fatal("untrusted must stay untrusted")
	}
}

func TestInspectCorpCert_HostnameMismatchAndExpiry(t *testing.T) {
	ca := newCorpCA(t)
	cert := ca.leaf(t, "other.corp.test")
	startCorpGateway(t, cert, ca.pool)
	info := inspectCorpCert(context.Background(), "vpn.corp.test", "")
	if info.HostnameMatches || !info.ChainTrusted || info.HostnameError == "" {
		t.Fatalf("hostname mismatch not detected: %#v", info)
	}
	if refineCorpCertError(corpErrCertUntrusted, info) != corpErrCertHostnameMismatch {
		t.Fatal("refine → hostname mismatch")
	}
	if refineCorpCertError(corpErrCertChanged, info) != corpErrCertChanged {
		t.Fatal("CHANGED must stay the headline")
	}

	corpCertNow = func() time.Time { return time.Now().Add(48 * time.Hour) }
	info = inspectCorpCert(context.Background(), "other.corp.test", "")
	if !info.Expired || info.ChainTrusted || refineCorpCertError(corpErrCertUntrusted, info) != corpErrCertExpired {
		t.Fatalf("expiry not detected: %#v", info)
	}
}
