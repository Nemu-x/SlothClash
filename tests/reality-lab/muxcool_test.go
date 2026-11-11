package realitylab

// The mux.cool lab (core patch 0039): a real Xray server, the core under test
// in front of it, one counting TCP relay per proxy between the two. Concurrent
// TCP streams must share one carrier and come back intact, UDP must ride the
// carrier as XUDP, and xudp-proxy-udp443 must reject, carry or bypass UDP/443.
// A proxy without the key, and the stock core with it, must open one VLESS
// connection per stream as before. Same switch as the REALITY lab:
//
//	XRAY_LAB=1 SLOTH_MIHOMO_BIN=<patched core> [SLOTH_MIHOMO_STOCK_BIN=<stock core>] go test -v -count=1 -run MuxCool .
//
// Local only: no internet past the image, Xray reaches the echo servers of
// this process through host.docker.internal.

import (
	"bytes"
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"net"
	"net/netip"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

const (
	muxXrayVersion    = "26.11.9"
	muxStreams        = 8 // the default max-concurrency: one carrier holds them all
	muxStreamBytes    = 1 << 20
	muxListenerBase   = 23000
	muxDatagrams      = 20
	muxUDP443Rejected = "mux.cool rejected UDP/443 traffic"
)

func TestMuxCool(t *testing.T) {
	if os.Getenv("XRAY_LAB") != "1" {
		t.Skip("set XRAY_LAB=1 (needs Docker) to run the mux.cool lab")
	}
	patched := os.Getenv("SLOTH_MIHOMO_BIN")
	stock := os.Getenv("SLOTH_MIHOMO_STOCK_BIN")
	if patched == "" && stock == "" {
		t.Fatal("set SLOTH_MIHOMO_BIN (patched core) and/or SLOTH_MIHOMO_STOCK_BIN")
	}

	echo := startEchoServers(t)
	srv := startMuxXray(t, echo)

	if patched != "" {
		t.Run("patched", func(t *testing.T) {
			lab := startMuxCore(t, patched, srv, 0)
			mux, plain := lab.proxies["mux"], lab.proxies["plain"]

			start := time.Now()
			checkStreams(t, mux, srv.echoTCP)
			t.Logf("mux: %d x %d KiB echoed in %v over %d carrier(s)", muxStreams, muxStreamBytes>>10, time.Since(start).Round(time.Millisecond), mux.relay.count())
			if got := mux.relay.count(); got != 1 {
				t.Errorf("mux: %d concurrent streams opened %d VLESS connections, want 1 carrier", muxStreams, got)
			}
			// UDP joins the idle carrier as an XUDP session (it lives 16 s).
			checkDatagrams(t, mux, srv.echoUDP)
			if got := mux.relay.count(); got != 1 {
				t.Errorf("mux: UDP opened %d VLESS connections in total, want it on the 1 carrier", got)
			}
			// allow: UDP/443 rides the carrier too.
			checkDatagrams(t, mux, srv.udp443)
			if got := mux.relay.count(); got != 1 {
				t.Errorf("mux allow: UDP/443 opened %d VLESS connections in total, want 1", got)
			}

			// reject (the default): no carrier, no reply, the reason in the log.
			reject := lab.proxies["mux-reject"]
			if err := exchangeDatagram(reject, srv.udp443, []byte("udp443"), 2*time.Second); err == nil {
				t.Error("mux reject: UDP/443 got a reply")
			}
			if got := reject.relay.count(); got != 0 {
				t.Errorf("mux reject: UDP/443 opened %d VLESS connections, want 0", got)
			}
			if b, _ := os.ReadFile(lab.log); !strings.Contains(string(b), muxUDP443Rejected) {
				t.Errorf("mux reject: core log lacks %q", muxUDP443Rejected)
			}

			// skip: UDP/443 bypasses the mux on a VLESS UDP connection of its own.
			skip := lab.proxies["mux-skip"]
			checkDatagrams(t, skip, srv.udp443)
			if got := skip.relay.count(); got != 1 {
				t.Errorf("mux skip: UDP/443 opened %d VLESS connections, want 1 plain one", got)
			}

			// Without the key nothing changes: one VLESS connection per stream.
			start = time.Now()
			checkStreams(t, plain, srv.echoTCP)
			t.Logf("plain: %d x %d KiB echoed in %v over %d connection(s)", muxStreams, muxStreamBytes>>10, time.Since(start).Round(time.Millisecond), plain.relay.count())
			if got := plain.relay.count(); got != muxStreams {
				t.Errorf("plain: %d streams opened %d VLESS connections, want %d", muxStreams, got, muxStreams)
			}
		})
	}
	if stock != "" {
		// The stock core ignores the key: the "mux" proxy dials per stream.
		t.Run("stock", func(t *testing.T) {
			lab := startMuxCore(t, stock, srv, 1)
			mux := lab.proxies["mux"]
			checkStreams(t, mux, srv.echoTCP)
			if got := mux.relay.count(); got != muxStreams {
				t.Errorf("stock: %d streams opened %d VLESS connections, want %d", muxStreams, got, muxStreams)
			}
		})
	}
}

// --- servers -------------------------------------------------------------

type echoServers struct{ tcp, udp int }

// startEchoServers runs a TCP and a UDP echo server for Xray to reach. On
// Linux the container comes in from the docker bridge; Docker Desktop
// forwards host.docker.internal to the host's loopback.
func startEchoServers(t *testing.T) echoServers {
	t.Helper()
	host := "127.0.0.1"
	if runtime.GOOS == "linux" {
		host = "0.0.0.0"
	}
	l, err := net.Listen("tcp", net.JoinHostPort(host, "0"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { l.Close() })
	go func() {
		for {
			c, err := l.Accept()
			if err != nil {
				return
			}
			go func() {
				defer c.Close()
				_, _ = io.Copy(c, c)
			}()
		}
	}()
	u, err := net.ListenPacket("udp", net.JoinHostPort(host, "0"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { u.Close() })
	go func() {
		buf := make([]byte, 65535)
		for {
			n, addr, err := u.ReadFrom(buf)
			if err != nil {
				return
			}
			_, _ = u.WriteTo(buf[:n], addr)
		}
	}()
	return echoServers{tcp: l.Addr().(*net.TCPAddr).Port, udp: u.LocalAddr().(*net.UDPAddr).Port}
}

type muxServer struct {
	port    int
	echoTCP netip.AddrPort // as Xray sees the echo servers
	echoUDP netip.AddrPort
	udp443  netip.AddrPort // UDP/443 that Xray redirects to the UDP echo
}

// startMuxXray runs a plain-TCP VLESS inbound (the mux does not care about
// the transport); UDP to port 443 is redirected to the UDP echo server.
func startMuxXray(t *testing.T, echo echoServers) *muxServer {
	t.Helper()
	allowAll := []any{map[string]any{"action": "allow"}}
	cfg := map[string]any{
		"log": map[string]any{"loglevel": "info"},
		"inbounds": []any{map[string]any{
			"listen": "0.0.0.0", "port": innerPort, "protocol": "vless",
			"settings":       map[string]any{"clients": []any{map[string]any{"id": labUUID}}, "decryption": "none"},
			"streamSettings": map[string]any{"network": "tcp"},
		}},
		// From 26.x freedom blocks private targets unless a final rule allows them.
		"outbounds": []any{
			map[string]any{"protocol": "freedom", "tag": "direct", "settings": map[string]any{
				"finalRules": allowAll,
			}},
			map[string]any{"protocol": "freedom", "tag": "udp443", "settings": map[string]any{
				"redirect": fmt.Sprintf("host.docker.internal:%d", echo.udp), "finalRules": allowAll,
			}},
		},
		"routing": map[string]any{"rules": []any{map[string]any{"network": "udp", "port": "443", "outboundTag": "udp443"}}},
	}
	dir := t.TempDir()
	writeJSON(t, filepath.Join(dir, "config.json"), cfg)
	name := "sloth-muxcool-" + strconv.FormatInt(time.Now().UnixNano()%1e8, 36)
	out, err := exec.Command("docker", "run", "-d", "--name", name,
		"--add-host", "host.docker.internal:host-gateway",
		"-p", fmt.Sprintf("127.0.0.1::%d", innerPort),
		"-v", filepath.ToSlash(dir)+":/etc/xray:ro", xrayImage(t, muxXrayVersion)).CombinedOutput()
	if err != nil {
		t.Fatalf("docker run %s: %v\n%s", name, err, out)
	}
	t.Cleanup(func() {
		if t.Failed() {
			if out, err := exec.Command("docker", "logs", "--tail", "80", name).CombinedOutput(); err == nil {
				t.Logf("xray log:\n%s", out)
			}
		}
		_ = exec.Command("docker", "rm", "-f", name).Run()
	})
	out, err = exec.Command("docker", "port", name, fmt.Sprintf("%d/tcp", innerPort)).Output()
	if err != nil {
		t.Fatalf("docker port %s: %v", name, err)
	}
	_, portStr, err := net.SplitHostPort(strings.TrimSpace(strings.SplitN(string(out), "\n", 2)[0]))
	if err != nil {
		t.Fatalf("docker port %s: %q", name, out)
	}
	port, _ := strconv.Atoi(portStr)
	waitTCP(t, port, 30*time.Second)

	// The core sends IPs, so UDP needs no resolver on the host: use the
	// address host.docker.internal has inside the container.
	out, err = exec.Command("docker", "exec", name, "grep", "host.docker.internal", "/etc/hosts").Output()
	if err != nil {
		t.Fatalf("host.docker.internal in %s: %v", name, err)
	}
	hostIP, err := netip.ParseAddr(strings.Fields(string(out))[0])
	if err != nil {
		t.Fatalf("host.docker.internal in %s: %q", name, out)
	}
	return &muxServer{
		port:    port,
		echoTCP: netip.AddrPortFrom(hostIP, uint16(echo.tcp)),
		echoUDP: netip.AddrPortFrom(hostIP, uint16(echo.udp)),
		udp443:  netip.AddrPortFrom(hostIP, 443),
	}
}

// countingRelay forwards every connection to the Xray port and counts them:
// each one is a VLESS connection of the core.
type countingRelay struct {
	port int
	n    atomic.Int32
}

func (r *countingRelay) count() int { return int(r.n.Load()) }

func startRelay(t *testing.T, target int) *countingRelay {
	t.Helper()
	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { l.Close() })
	r := &countingRelay{port: l.Addr().(*net.TCPAddr).Port}
	go func() {
		for {
			c, err := l.Accept()
			if err != nil {
				return
			}
			r.n.Add(1)
			go func() {
				defer c.Close()
				up, err := net.Dial("tcp", fmt.Sprintf("127.0.0.1:%d", target))
				if err != nil {
					return
				}
				defer up.Close()
				go func() { _, _ = io.Copy(up, c); up.(*net.TCPConn).CloseWrite() }()
				_, _ = io.Copy(c, up)
			}()
		}
	}()
	return r
}

type muxProxy struct {
	name   string
	listen int // mixed (SOCKS5) listener of the core
	relay  *countingRelay
}

type muxCore struct {
	proxies map[string]*muxProxy
	log     string
}

var muxProxyKeys = map[string]map[string]any{
	"plain":      nil,
	"mux":        {"enabled": true, "xudp-proxy-udp443": "allow"},
	"mux-reject": {"enabled": true},
	"mux-skip":   {"enabled": true, "xudp-proxy-udp443": "skip"},
}

func startMuxCore(t *testing.T, bin string, srv *muxServer, block int) *muxCore {
	t.Helper()
	home := t.TempDir()
	names := []string{"plain", "mux", "mux-reject", "mux-skip"}
	ports := reservePorts(t, muxListenerBase+block*listenerPortStride, len(names))
	lab := &muxCore{proxies: map[string]*muxProxy{}, log: filepath.Join(home, "core.log")}
	var proxies, listeners []any
	for i, name := range names {
		p := &muxProxy{name: name, listen: ports[i], relay: startRelay(t, srv.port)}
		lab.proxies[name] = p
		proxy := map[string]any{
			"name": name, "type": "vless", "server": "127.0.0.1", "port": p.relay.port,
			"uuid": labUUID, "network": "tcp", "udp": true,
		}
		if keys := muxProxyKeys[name]; keys != nil {
			proxy["mux.cool"] = keys
		}
		proxies = append(proxies, proxy)
		listeners = append(listeners, map[string]any{"name": "l-" + name, "type": "mixed", "listen": "127.0.0.1", "port": p.listen, "udp": true, "proxy": name})
	}
	cfg := map[string]any{
		"mixed-port": 0, "log-level": "debug", "mode": "global", "ipv6": false,
		"proxies": proxies, "listeners": listeners, "rules": []string{"MATCH,DIRECT"},
	}
	writeJSON(t, filepath.Join(home, "config.yaml"), cfg)
	logf, err := os.Create(lab.log)
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cmd := exec.CommandContext(ctx, bin, "-d", home)
	cmd.Stdout, cmd.Stderr = logf, logf
	if err := cmd.Start(); err != nil {
		cancel()
		t.Fatalf("start %s: %v", bin, err)
	}
	t.Cleanup(func() {
		cancel()
		_ = cmd.Wait()
		_ = logf.Close()
		if t.Failed() {
			if b, err := os.ReadFile(lab.log); err == nil {
				t.Logf("core log:\n%s", b)
			}
		}
	})
	for _, p := range ports {
		waitTCP(t, p, 20*time.Second)
	}
	return lab
}

// --- traffic -------------------------------------------------------------

// checkStreams runs muxStreams concurrent TCP streams through the proxy, each
// sending its own random megabyte to the echo server, and compares hashes.
// All streams are connected before any sends, so the carrier must hold them
// at the same time.
func checkStreams(t *testing.T, p *muxProxy, target netip.AddrPort) {
	t.Helper()
	conns := make([]net.Conn, muxStreams)
	for i := range conns {
		c, err := socksConnect(p.listen, target)
		if err != nil {
			t.Fatalf("%s: stream %d: %v", p.name, i, err)
		}
		defer c.Close()
		conns[i] = c
	}
	var wg sync.WaitGroup
	errs := make([]error, muxStreams)
	for i, c := range conns {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_ = c.SetDeadline(time.Now().Add(60 * time.Second))
			data := make([]byte, muxStreamBytes)
			_, _ = rand.Read(data)
			go func() { _, _ = c.Write(data) }()
			got := make([]byte, len(data))
			if _, err := io.ReadFull(c, got); err != nil {
				errs[i] = err
				return
			}
			if sha256.Sum256(got) != sha256.Sum256(data) {
				errs[i] = errors.New("echo differs")
			}
		}()
	}
	wg.Wait()
	for i, err := range errs {
		if err != nil {
			t.Errorf("%s: stream %d: %v", p.name, i, err)
		}
	}
}

// checkDatagrams sends muxDatagrams distinct datagrams on one UDP association
// and wants each echoed unchanged (each is retried up to three times).
func checkDatagrams(t *testing.T, p *muxProxy, target netip.AddrPort) {
	t.Helper()
	assoc, err := socksAssociate(p.listen)
	if err != nil {
		t.Fatalf("%s: UDP associate: %v", p.name, err)
	}
	defer assoc.Close()
	for i := 0; i < muxDatagrams; i++ {
		payload := make([]byte, 64+i*61)
		_, _ = rand.Read(payload)
		var err error
		for try := 0; try < 3; try++ {
			if err = assoc.exchange(target, payload, 2*time.Second); err == nil {
				break
			}
		}
		if err != nil {
			t.Fatalf("%s: datagram %d to %v: %v", p.name, i, target, err)
		}
	}
}

func exchangeDatagram(p *muxProxy, target netip.AddrPort, payload []byte, timeout time.Duration) error {
	assoc, err := socksAssociate(p.listen)
	if err != nil {
		return err
	}
	defer assoc.Close()
	return assoc.exchange(target, payload, timeout)
}

// --- minimal SOCKS5 client (no auth) --------------------------------------

func socksHandshake(port int, cmd byte, addr netip.AddrPort) (net.Conn, netip.AddrPort, error) {
	c, err := net.DialTimeout("tcp", fmt.Sprintf("127.0.0.1:%d", port), 5*time.Second)
	if err != nil {
		return nil, netip.AddrPort{}, err
	}
	_ = c.SetDeadline(time.Now().Add(10 * time.Second))
	fail := func(err error) (net.Conn, netip.AddrPort, error) {
		c.Close()
		return nil, netip.AddrPort{}, err
	}
	if _, err := c.Write([]byte{5, 1, 0}); err != nil {
		return fail(err)
	}
	var method [2]byte
	if _, err := io.ReadFull(c, method[:]); err != nil || method != [2]byte{5, 0} {
		return fail(fmt.Errorf("SOCKS method %v: %v", method, err))
	}
	if _, err := c.Write(append([]byte{5, cmd, 0}, socksAddr(addr)...)); err != nil {
		return fail(err)
	}
	var head [4]byte
	if _, err := io.ReadFull(c, head[:]); err != nil {
		return fail(err)
	}
	if head[1] != 0 {
		return fail(fmt.Errorf("SOCKS reply %d", head[1]))
	}
	var ipLen int
	switch head[3] {
	case 1:
		ipLen = 4
	case 4:
		ipLen = 16
	default:
		return fail(fmt.Errorf("SOCKS reply address type %d", head[3]))
	}
	rest := make([]byte, ipLen+2)
	if _, err := io.ReadFull(c, rest); err != nil {
		return fail(err)
	}
	ip, _ := netip.AddrFromSlice(rest[:ipLen])
	_ = c.SetDeadline(time.Time{})
	return c, netip.AddrPortFrom(ip, binary.BigEndian.Uint16(rest[ipLen:])), nil
}

func socksAddr(addr netip.AddrPort) []byte {
	var b []byte
	if addr.Addr().Is4() {
		ip := addr.Addr().As4()
		b = append([]byte{1}, ip[:]...)
	} else {
		ip := addr.Addr().As16()
		b = append([]byte{4}, ip[:]...)
	}
	return binary.BigEndian.AppendUint16(b, addr.Port())
}

func socksConnect(port int, target netip.AddrPort) (net.Conn, error) {
	c, _, err := socksHandshake(port, 1, target)
	return c, err
}

type socksUDP struct {
	ctrl  net.Conn
	conn  *net.UDPConn
	relay netip.AddrPort
}

func socksAssociate(port int) (*socksUDP, error) {
	ctrl, relay, err := socksHandshake(port, 3, netip.AddrPortFrom(netip.IPv4Unspecified(), 0))
	if err != nil {
		return nil, err
	}
	if relay.Addr().IsUnspecified() {
		relay = netip.AddrPortFrom(netip.MustParseAddr("127.0.0.1"), relay.Port())
	}
	conn, err := net.ListenUDP("udp", net.UDPAddrFromAddrPort(netip.MustParseAddrPort("127.0.0.1:0")))
	if err != nil {
		ctrl.Close()
		return nil, err
	}
	return &socksUDP{ctrl: ctrl, conn: conn, relay: relay}, nil
}

func (s *socksUDP) Close() {
	s.conn.Close()
	s.ctrl.Close()
}

// exchange sends one datagram and waits for its echo from the target.
func (s *socksUDP) exchange(target netip.AddrPort, payload []byte, timeout time.Duration) error {
	packet := append(append([]byte{0, 0, 0}, socksAddr(target)...), payload...)
	if _, err := s.conn.WriteToUDPAddrPort(packet, s.relay); err != nil {
		return err
	}
	_ = s.conn.SetReadDeadline(time.Now().Add(timeout))
	buf := make([]byte, 65535)
	for {
		n, _, err := s.conn.ReadFromUDPAddrPort(buf)
		if err != nil {
			return err
		}
		header := 3 + len(socksAddr(target))
		if n < header {
			continue
		}
		if bytes.Equal(buf[header:n], payload) {
			return nil
		}
		// an echo of an earlier, retried datagram: keep waiting
	}
}
