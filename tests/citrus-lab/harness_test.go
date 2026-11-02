package citruslab

// The local servers, certificates, key generators and SOCKS5 UDP helpers below
// are copied from Citrus' interop harness (getmikan/Citrus, test/interop
// harness_test.go and mihomo_test.go at 62d1f567711c5e009115f471ea89882c196d00e5),
// trimmed to what this lab uses. Process handling, ports and probes are this
// lab's own: fixed port bands, attempt-scoped processes, readiness and bind
// checks.

import (
	"bytes"
	"crypto/ecdh"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/sha256"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/base64"
	"encoding/binary"
	"encoding/hex"
	"encoding/json"
	"encoding/pem"
	"errors"
	"fmt"
	"io"
	"log"
	"math/big"
	"net"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"time"
)

const (
	realitySNI = "interop.test"     // REALITY server name and TLS/ECH inner name
	echPublic  = "ech.interop.test" // ECH outer (public) name
	shortID    = "0123456789abcdef"
)

var (
	echoAddr    string // HTTP echo: GET /ping?t=X -> "pong X", POST /sum -> sha256
	udpEchoAddr string // UDP echo
	targetAddr  string // TLS 1.3 REALITY target

	tlsCertPEM, tlsKeyPEM []byte // self-signed leaf for interop.test
	tlsCertSHA256         string
)

type obj = map[string]any

func merge(maps ...obj) obj {
	out := obj{}
	for _, m := range maps {
		for k, v := range m {
			out[k] = v
		}
	}
	return out
}

func startFixtures() (func(), error) {
	var stops []func()
	stop := func() {
		for _, s := range stops {
			s()
		}
	}
	for _, f := range []func() (func(), error){startEcho, startUDPEcho, startTarget} {
		s, err := f()
		if err != nil {
			stop()
			return nil, err
		}
		stops = append(stops, s)
	}
	var err error
	if tlsCertPEM, tlsKeyPEM, tlsCertSHA256, err = leafCert(realitySNI); err != nil {
		stop()
		return nil, err
	}
	return stop, nil
}

func startEcho() (func(), error) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		return nil, err
	}
	echoAddr = ln.Addr().String()
	mux := http.NewServeMux()
	mux.HandleFunc("/ping", func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprintf(w, "pong %s", r.URL.Query().Get("t"))
	})
	mux.HandleFunc("/sum", func(w http.ResponseWriter, r *http.Request) {
		h := sha256.New()
		if _, err := io.Copy(h, r.Body); err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		io.WriteString(w, hex.EncodeToString(h.Sum(nil)))
	})
	srv := &http.Server{Handler: mux, ReadHeaderTimeout: 10 * time.Second}
	go srv.Serve(ln)
	return func() { srv.Close() }, nil
}

func startUDPEcho() (func(), error) {
	pc, err := net.ListenPacket("udp", "127.0.0.1:0")
	if err != nil {
		return nil, err
	}
	// a burst of 200 datagrams must not be lost in the echo's own buffer
	if uc, ok := pc.(*net.UDPConn); ok {
		uc.SetReadBuffer(4 << 20)
		uc.SetWriteBuffer(4 << 20)
	}
	udpEchoAddr = pc.LocalAddr().String()
	go func() {
		buf := make([]byte, 64*1024)
		for {
			n, addr, err := pc.ReadFrom(buf)
			if err != nil {
				return
			}
			pc.WriteTo(buf[:n], addr)
		}
	}()
	return func() { pc.Close() }, nil
}

// startTarget runs the REALITY target: TLS 1.3, a chain large enough for an
// ML-DSA-65 signature.
func startTarget() (func(), error) {
	chain, key, err := bigChain()
	if err != nil {
		return nil, err
	}
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		return nil, err
	}
	targetAddr = ln.Addr().String()
	srv := &http.Server{
		Handler: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			io.WriteString(w, "reality target\n")
		}),
		TLSConfig: &tls.Config{
			MinVersion:   tls.VersionTLS13,
			Certificates: []tls.Certificate{{Certificate: chain, PrivateKey: key}},
		},
		ReadHeaderTimeout: 10 * time.Second,
		ErrorLog:          log.New(io.Discard, "", 0),
	}
	go srv.ServeTLS(ln, "", "")
	return func() { srv.Close() }, nil
}

func bigChain() ([][]byte, *ecdsa.PrivateKey, error) {
	now := time.Now()
	mk := func(cn string, parent *x509.Certificate, parentKey *ecdsa.PrivateKey, ca bool, sans []string) (*x509.Certificate, *ecdsa.PrivateKey, error) {
		key, err := ecdsa.GenerateKey(elliptic.P384(), rand.Reader)
		if err != nil {
			return nil, nil, err
		}
		serial, _ := rand.Int(rand.Reader, big.NewInt(1<<62))
		tpl := &x509.Certificate{
			SerialNumber:          serial,
			Subject:               pkix.Name{CommonName: cn, Organization: []string{"Citrus lab"}},
			NotBefore:             now.Add(-time.Hour),
			NotAfter:              now.Add(24 * time.Hour),
			BasicConstraintsValid: true,
			IsCA:                  ca,
			DNSNames:              sans,
		}
		if ca {
			tpl.KeyUsage = x509.KeyUsageCertSign | x509.KeyUsageCRLSign
		} else {
			tpl.KeyUsage = x509.KeyUsageDigitalSignature
			tpl.ExtKeyUsage = []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth}
		}
		if parent == nil {
			parent, parentKey = tpl, key
		}
		der, err := x509.CreateCertificate(rand.Reader, tpl, parent, &key.PublicKey, parentKey)
		if err != nil {
			return nil, nil, err
		}
		c, err := x509.ParseCertificate(der)
		return c, key, err
	}
	root, rootKey, err := mk("Citrus Lab Root", nil, nil, true, nil)
	if err != nil {
		return nil, nil, err
	}
	inter, interKey, err := mk("Citrus Lab Intermediate", root, rootKey, true, nil)
	if err != nil {
		return nil, nil, err
	}
	sans := []string{realitySNI}
	for i := 0; len(sans) < 120; i++ {
		sans = append(sans, fmt.Sprintf("host-%03d.%s", i, realitySNI))
	}
	leaf, leafKey, err := mk(realitySNI, inter, interKey, false, sans)
	if err != nil {
		return nil, nil, err
	}
	return [][]byte{leaf.Raw, inter.Raw, root.Raw}, leafKey, nil
}

func leafCert(name string) (certPEM, keyPEM []byte, sum string, err error) {
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		return
	}
	serial, _ := rand.Int(rand.Reader, big.NewInt(1<<62))
	tpl := &x509.Certificate{
		SerialNumber: serial,
		Subject:      pkix.Name{CommonName: name},
		DNSNames:     []string{name},
		NotBefore:    time.Now().Add(-time.Hour),
		NotAfter:     time.Now().Add(24 * time.Hour),
		KeyUsage:     x509.KeyUsageDigitalSignature,
		ExtKeyUsage:  []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth},
	}
	der, err := x509.CreateCertificate(rand.Reader, tpl, tpl, &key.PublicKey, key)
	if err != nil {
		return
	}
	kder, err := x509.MarshalPKCS8PrivateKey(key)
	if err != nil {
		return
	}
	h := sha256.Sum256(der)
	return pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der}),
		pem.EncodeToMemory(&pem.Block{Type: "PRIVATE KEY", Bytes: kder}),
		hex.EncodeToString(h[:]), nil
}

// --- keys ----------------------------------------------------------------------

type x25519Keys struct{ priv, pub string }

func genX25519() x25519Keys {
	k, err := ecdh.X25519().GenerateKey(rand.Reader)
	if err != nil {
		panic(err)
	}
	enc := base64.RawURLEncoding
	return x25519Keys{priv: enc.EncodeToString(k.Bytes()), pub: enc.EncodeToString(k.PublicKey().Bytes())}
}

func genUUID() string {
	b := make([]byte, 16)
	rand.Read(b)
	b[6] = b[6]&0x0f | 0x40
	b[8] = b[8]&0x3f | 0x80
	return fmt.Sprintf("%x-%x-%x-%x-%x", b[0:4], b[4:6], b[6:8], b[8:10], b[10:])
}

func randB64(n int) string {
	b := make([]byte, n)
	rand.Read(b)
	return base64.StdEncoding.EncodeToString(b)
}

func token() string {
	b := make([]byte, 8)
	rand.Read(b)
	return hex.EncodeToString(b)
}

type mldsaKeys struct {
	Seed        string `json:"seed"`
	Verify      string `json:"verify"`
	OtherVerify string `json:"otherVerify"`
}

func readMLDSA() (mldsaKeys, error) {
	var k mldsaKeys
	b, err := os.ReadFile(filepath.Join("testdata", "mldsa65.json"))
	if err == nil {
		err = json.Unmarshal(b, &k)
	}
	return k, err
}

type echKeys struct{ configList, keyPEM string }

// genECH asks the Citrus under test for an ECH key pair (citrus generate
// ech-keypair): a base64 ECHConfigList and an "ECH KEYS" PEM.
func genECH(publicName string) (echKeys, error) {
	out, err := exec.Command(citrusBin, "generate", "ech-keypair", publicName).CombinedOutput()
	if err != nil {
		return echKeys{}, fmt.Errorf("citrus generate ech-keypair: %v\n%s", err, out)
	}
	s := string(out)
	var k echKeys
	for _, line := range strings.Split(s, "\n") {
		if v, ok := strings.CutPrefix(strings.TrimSpace(line), "Config:"); ok {
			k.configList = strings.TrimSpace(v)
		}
	}
	if i := strings.Index(s, "-----BEGIN ECH KEYS-----"); i >= 0 {
		k.keyPEM = strings.TrimSpace(s[i:]) + "\n"
	}
	if k.configList == "" || k.keyPEM == "" {
		return echKeys{}, fmt.Errorf("citrus generate ech-keypair: unexpected output\n%s", s)
	}
	return k, nil
}

// echPEM is the ECHConfigList in the form `xray tls ech --pem` prints.
func echPEM(configList string) string {
	der, err := base64.StdEncoding.DecodeString(configList)
	if err != nil {
		panic(err)
	}
	return string(pem.EncodeToMemory(&pem.Block{Type: "ECH CONFIGS", Bytes: der}))
}

// echWrapped is the base64 list wrapped at 64 columns, as Xray configs and
// panels often carry it.
func echWrapped(configList string) string {
	var b strings.Builder
	for i := 0; i < len(configList); i += 64 {
		end := min(i+64, len(configList))
		b.WriteString(configList[i:end])
		b.WriteString("\n")
	}
	return b.String()
}

// --- ports ---------------------------------------------------------------------

// Every cell takes its ports from a fixed band below every ephemeral range
// (Linux 32768+, Windows 49152+), never from bind-:0-and-close, which hands
// out the same port twice under parallel load (see tests/reality-lab). The
// REALITY lab uses 20000..21400; this lab starts above it.
const (
	portBase   = 22000
	bandSize   = 10
	bandsLimit = 900 // 22000..30999
)

type portBand struct {
	next, end int
}

func newBand(index int) *portBand {
	if index < 0 || index >= bandsLimit {
		panic(fmt.Sprintf("port band %d out of range", index))
	}
	start := portBase + index*bandSize
	return &portBand{next: start, end: start + bandSize}
}

// take returns the next port of the band free for both TCP and UDP right now.
func (b *portBand) take() (int, error) {
	for ; b.next < b.end; b.next++ {
		p := b.next
		ln, err := net.Listen("tcp", fmt.Sprintf("127.0.0.1:%d", p))
		if err != nil {
			continue
		}
		pc, err := net.ListenPacket("udp", fmt.Sprintf("127.0.0.1:%d", p))
		ln.Close()
		if err != nil {
			continue
		}
		pc.Close()
		b.next++
		return p, nil
	}
	return 0, fmt.Errorf("port band exhausted at %d", b.end)
}

// --- processes -----------------------------------------------------------------

// proc is one Citrus or core process of one attempt of a cell.
type proc struct {
	role    string
	log     string
	cfgPath string
	cmd     *exec.Cmd
	done    chan struct{}
	logf    *os.File
	closeFn func() // not a process: an in-test server
}

// closerProc wraps an in-test server so that a cell stops it with its processes.
func closerProc(role string, closeFn func()) *proc {
	return &proc{role: role, closeFn: closeFn}
}

func (p *proc) stop() {
	if p.closeFn != nil {
		p.closeFn()
		return
	}
	if p.cmd.Process != nil {
		p.cmd.Process.Kill()
	}
	<-p.done
	p.logf.Close()
}

func (p *proc) exited() bool {
	select {
	case <-p.done:
		return true
	default:
		return false
	}
}

func (p *proc) logText() string {
	if p.log == "" {
		return ""
	}
	b, _ := os.ReadFile(p.log)
	return string(b)
}

func (p *proc) has(s string) bool { return strings.Contains(p.logText(), s) }

func (p *proc) tail(n int) string {
	lines := strings.Split(strings.TrimRight(p.logText(), "\n"), "\n")
	if len(lines) > n {
		lines = lines[len(lines)-n:]
	}
	return strings.Join(lines, "\n")
}

// spawn writes cfg (JSON is valid YAML) to dir and runs bin -d dir -f cfg.
func spawn(dir, role, bin string, cfg obj) (*proc, error) {
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return nil, err
	}
	b, err := json.MarshalIndent(cfg, "", "  ")
	if err != nil {
		return nil, err
	}
	p := &proc{role: role, cfgPath: filepath.Join(dir, role+".yaml"), log: filepath.Join(dir, role+".log"), done: make(chan struct{})}
	if err := os.WriteFile(p.cfgPath, b, 0o644); err != nil {
		return nil, err
	}
	if p.logf, err = os.Create(p.log); err != nil {
		return nil, err
	}
	home := filepath.Join(dir, role+"-home")
	if err := os.MkdirAll(home, 0o755); err != nil {
		return nil, err
	}
	p.cmd = exec.Command(bin, "-d", home, "-f", p.cfgPath)
	p.cmd.Stdout, p.cmd.Stderr = p.logf, p.logf
	if err := p.cmd.Start(); err != nil {
		p.logf.Close()
		return nil, err
	}
	go func() { p.cmd.Wait(); close(p.done) }()
	return p, nil
}

var errExited = errors.New("process exited")

// waitReady waits until port answers on TCP and the log says the providers
// are loaded (listeners open before the tunnel runs; connections in between
// are dropped), then checks that no listener failed to bind. A process that
// exits first returns errExited: for the core that is how a refused config
// shows.
func (p *proc) waitReady(port int) error {
	deadline := time.Now().Add(20 * time.Second)
	addr := fmt.Sprintf("127.0.0.1:%d", port)
	up := false
	for time.Now().Before(deadline) {
		if p.exited() {
			return fmt.Errorf("%s: %w:\n%s", p.role, errExited, p.tail(15))
		}
		if !up {
			if c, err := net.DialTimeout("tcp", addr, 300*time.Millisecond); err == nil {
				c.Close()
				up = true
			}
		}
		if up && p.has("Start initial compatible provider") {
			time.Sleep(300 * time.Millisecond)
			low := strings.ToLower(p.logText())
			for _, bad := range []string{"listen err", "address already in use", "only one usage of each socket address"} {
				if strings.Contains(low, bad) {
					return fmt.Errorf("%s: a listener did not bind (%q):\n%s", p.role, bad, p.tail(15))
				}
			}
			if p.exited() {
				return fmt.Errorf("%s: %w:\n%s", p.role, errExited, p.tail(15))
			}
			return nil
		}
		time.Sleep(100 * time.Millisecond)
	}
	return fmt.Errorf("%s: not ready on %s after 20s:\n%s", p.role, addr, p.tail(15))
}

// --- traffic -------------------------------------------------------------------

func socksClient(port int, timeout time.Duration) *http.Client {
	u := &url.URL{Scheme: "socks5", Host: fmt.Sprintf("127.0.0.1:%d", port)}
	return &http.Client{
		Timeout:   timeout,
		Transport: &http.Transport{Proxy: http.ProxyURL(u), DisableKeepAlives: true},
	}
}

// probeTCP reports whether any of n attempts gets a GET and a 64 KiB POST
// through the socks port intact, and what each attempt saw. Each attempt is a
// fresh proxied connection.
func probeTCP(port, n int, timeout time.Duration) (bool, string) {
	var seen []string
	for i := 0; i < n; i++ {
		start := time.Now()
		err := tcpRound(socksClient(port, timeout))
		took := time.Since(start).Round(10 * time.Millisecond)
		if err == nil {
			seen = append(seen, fmt.Sprintf("#%d %v: ok", i+1, took))
			return true, strings.Join(seen, "; ")
		}
		seen = append(seen, fmt.Sprintf("#%d %v: %v", i+1, took, err))
	}
	return false, strings.Join(seen, "; ")
}

func tcpRound(c *http.Client) error {
	tok := token()
	resp, err := c.Get("http://" + echoAddr + "/ping?t=" + tok)
	if err != nil {
		return err
	}
	body, err := io.ReadAll(resp.Body)
	resp.Body.Close()
	if err != nil || string(body) != "pong "+tok {
		return fmt.Errorf("GET: %q, %v", trunc(body), err)
	}
	blob := make([]byte, 64*1024)
	rand.Read(blob)
	sum := sha256.Sum256(blob)
	resp, err = c.Post("http://"+echoAddr+"/sum", "application/octet-stream", bytes.NewReader(blob))
	if err != nil {
		return fmt.Errorf("POST: %w", err)
	}
	body, err = io.ReadAll(resp.Body)
	resp.Body.Close()
	if err != nil || string(body) != hex.EncodeToString(sum[:]) {
		return fmt.Errorf("POST: %q, %v", trunc(body), err)
	}
	return nil
}

func trunc(b []byte) string {
	if len(b) > 80 {
		return string(b[:80]) + "..."
	}
	return string(b)
}

// --- UDP through a SOCKS5 UDP ASSOCIATE ---------------------------------------

// udpSession is one UDP ASSOCIATE: the control connection (the association
// lives as long as it does) and a socket aimed at the relay.
type udpSession struct {
	ctrl net.Conn
	uc   *net.UDPConn
	hdr  []byte
}

func openUDP(port int) (*udpSession, error) {
	ctrl, relay, err := socksUDPAssociate(port)
	if err != nil {
		return nil, err
	}
	uc, err := net.DialUDP("udp", nil, relay)
	if err != nil {
		ctrl.Close()
		return nil, err
	}
	uc.SetReadBuffer(4 << 20)
	dst, err := net.ResolveUDPAddr("udp", udpEchoAddr)
	if err != nil {
		ctrl.Close()
		uc.Close()
		return nil, err
	}
	hdr := []byte{0, 0, 0, 1}
	hdr = append(hdr, dst.IP.To4()...)
	hdr = binary.BigEndian.AppendUint16(hdr, uint16(dst.Port))
	return &udpSession{ctrl: ctrl, uc: uc, hdr: hdr}, nil
}

func (s *udpSession) close() {
	s.uc.Close()
	s.ctrl.Close()
}

func (s *udpSession) send(payload []byte) error {
	_, err := s.uc.Write(append(append([]byte{}, s.hdr...), payload...))
	return err
}

// recv returns the next echoed payload before deadline.
func (s *udpSession) recv(deadline time.Time) ([]byte, error) {
	buf := make([]byte, 64*1024)
	s.uc.SetReadDeadline(deadline)
	n, err := s.uc.Read(buf)
	if err != nil {
		return nil, err
	}
	return socksUDPBody(buf[:n])
}

// echo sends payload up to attempts times, waiting wait for it to come back.
// A first datagram may be lost while the tunnel is set up, hence the retries.
func (s *udpSession) echo(payload []byte, attempts int, wait time.Duration) (int, error) {
	for a := 1; a <= attempts; a++ {
		if err := s.send(payload); err != nil {
			return a, err
		}
		deadline := time.Now().Add(wait)
		for {
			body, err := s.recv(deadline)
			if err != nil {
				break
			}
			if bytes.Equal(body, payload) {
				return a, nil
			}
		}
	}
	return attempts, fmt.Errorf("no echo of %d bytes after %d attempts", len(payload), attempts)
}

func socksUDPAssociate(port int) (net.Conn, *net.UDPAddr, error) {
	c, err := net.DialTimeout("tcp", fmt.Sprintf("127.0.0.1:%d", port), 5*time.Second)
	if err != nil {
		return nil, nil, err
	}
	c.SetDeadline(time.Now().Add(10 * time.Second))
	fail := func(format string, args ...any) (net.Conn, *net.UDPAddr, error) {
		c.Close()
		return nil, nil, fmt.Errorf(format, args...)
	}
	if _, err := c.Write([]byte{5, 1, 0}); err != nil {
		return fail("socks greeting: %v", err)
	}
	r := make([]byte, 2)
	if _, err := io.ReadFull(c, r); err != nil || r[1] != 0 {
		return fail("socks greeting reply %v: %v", r, err)
	}
	if _, err := c.Write([]byte{5, 3, 0, 1, 0, 0, 0, 0, 0, 0}); err != nil {
		return fail("socks UDP ASSOCIATE: %v", err)
	}
	head := make([]byte, 4)
	if _, err := io.ReadFull(c, head); err != nil || head[1] != 0 {
		return fail("socks UDP ASSOCIATE reply %v: %v", head, err)
	}
	var ip net.IP
	switch head[3] {
	case 1:
		ip = make(net.IP, 4)
	case 4:
		ip = make(net.IP, 16)
	default:
		return fail("socks UDP ASSOCIATE reply address type %d", head[3])
	}
	if _, err := io.ReadFull(c, ip); err != nil {
		return fail("socks UDP ASSOCIATE reply: %v", err)
	}
	pb := make([]byte, 2)
	if _, err := io.ReadFull(c, pb); err != nil {
		return fail("socks UDP ASSOCIATE reply: %v", err)
	}
	c.SetDeadline(time.Time{})
	if ip.IsUnspecified() {
		ip = net.IPv4(127, 0, 0, 1)
	}
	return c, &net.UDPAddr{IP: ip, Port: int(binary.BigEndian.Uint16(pb))}, nil
}

func socksUDPBody(p []byte) ([]byte, error) {
	if len(p) < 4 {
		return nil, io.ErrUnexpectedEOF
	}
	n := 4
	switch p[3] {
	case 1:
		n += 4
	case 4:
		n += 16
	case 3:
		if len(p) < 5 {
			return nil, io.ErrUnexpectedEOF
		}
		n += 1 + int(p[4])
	default:
		return nil, fmt.Errorf("address type %d", p[3])
	}
	n += 2
	if len(p) < n {
		return nil, io.ErrUnexpectedEOF
	}
	return p[n:], nil
}

// --- small helpers -------------------------------------------------------------

var logNameRE = strings.NewReplacer("/", "__", " ", "_", "?", "_", "=", "_", ":", "_")

func safeName(s string) string { return logNameRE.Replace(s) }

// once-per-run lazily built fixtures shared by the cases
type lazy[T any] struct {
	once sync.Once
	v    T
	err  error
	f    func() (T, error)
}

func (l *lazy[T]) get() (T, error) {
	l.once.Do(func() { l.v, l.err = l.f() })
	return l.v, l.err
}
