// Package realitylab is the opt-in REALITY compatibility matrix for the
// patched mihomo core (core/patches/mihomo): real Xray servers of every
// generation in Docker, the core under test in front of them, one HTTPS probe
// per proxy. See docs/core-patches.md.
//
//	XRAY_LAB=1 SLOTH_MIHOMO_BIN=<patched core> [SLOTH_MIHOMO_STOCK_BIN=<stock core>] go test -v -count=1 .
//
// Needs Docker and internet (Xray falls back to www.cloudflare.com; the probe
// fetches https://www.gstatic.com/generate_204 through the tunnel).
package realitylab

import (
	"context"
	"encoding/json"
	"fmt"
	"net"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"
)

// Lab-only key pair and ids (never used outside this test).
const (
	labPrivateKey = "KMrJVES1iBgdUD96IaYtAQTlv2BJonjum3X83DXtOGQ"
	labPublicKey  = "WB52-may9oKJkdKL4cbLSLg_t41INOVet9-1ey1RLnk"
	labUUID       = "3f1c9a3e-5c6a-4d1e-9d8b-2a7e4f0b1c55"
	labShortID    = "0123456789abcdef"
	labSNI        = "www.cloudflare.com"
	probeURL      = "https://www.gstatic.com/generate_204"
	innerPort     = 10000
)

// Xray generations: before 2025 a hybrid ClientHello is dropped, from 26.9.8
// a classic one is rejected, in between both are accepted.
var xrayVersions = []string{"24.12.31", "25.7.25", "26.4.13", "26.9.9"}

var variants = map[string]map[string]any{
	"plain":  {},
	"minver": {"minClientVer": "25.1.1"},
	"maxver": {"maxClientVer": "2.0.0"},
}

type server struct {
	version, variant string
	port             int
}

type row struct {
	srv         *server
	fingerprint string
	forced      bool // per-proxy support-x25519mlkem768: true
}

func (r row) name() string {
	n := fmt.Sprintf("%s-%s-%s", r.srv.version, r.srv.variant, r.fingerprint)
	if r.forced {
		n += "-forced"
	}
	return n
}

// acceptsShare reports whether an Xray server of this version completes a
// REALITY handshake with (mlkem=true) or without the X25519MLKEM768 share.
func acceptsShare(version string, mlkem bool) bool {
	switch {
	case cmpVersion(version, "25.0.0") < 0:
		return !mlkem
	case cmpVersion(version, "26.9.8") >= 0:
		return mlkem
	}
	return true
}

func versionGate(r row, clientVersion string) bool {
	switch r.srv.variant {
	case "minver":
		return cmpVersion(clientVersion, "25.1.1") >= 0
	case "maxver":
		return cmpVersion(clientVersion, "2.0.0") <= 0
	}
	return true
}

// expectPatched models the patched core: policy auto|on|off, client version.
func expectPatched(policy, clientVersion string) func(row) bool {
	return func(r row) bool {
		if !versionGate(r, clientVersion) {
			return false
		}
		switch {
		case r.forced || policy == "on":
			return acceptsShare(r.srv.version, true) // non-chrome prints are swapped to chrome
		case policy == "off":
			return acceptsShare(r.srv.version, false)
		}
		return true // auto: one of the two shares works on every generation
	}
}

// expectStock models upstream mihomo: client version 1.8.2, hybrid share only
// when forced and only from chrome, unknown top-level keys ignored.
func expectStock(r row) bool {
	if !versionGate(r, "1.8.2") {
		return false
	}
	return acceptsShare(r.srv.version, r.forced && r.fingerprint == "chrome")
}

func TestRealityMatrix(t *testing.T) {
	if os.Getenv("XRAY_LAB") != "1" {
		t.Skip("set XRAY_LAB=1 (needs Docker + internet) to run the REALITY lab")
	}
	patched := os.Getenv("SLOTH_MIHOMO_BIN")
	stock := os.Getenv("SLOTH_MIHOMO_STOCK_BIN")
	if patched == "" && stock == "" {
		t.Fatal("set SLOTH_MIHOMO_BIN (patched core) and/or SLOTH_MIHOMO_STOCK_BIN")
	}
	if _, err := exec.LookPath("docker"); err != nil {
		t.Fatal("docker not found")
	}

	servers := startServers(t)
	var rows []row
	for _, s := range servers {
		rows = append(rows, row{srv: s, fingerprint: "chrome"}, row{srv: s, fingerprint: "chrome", forced: true})
		if s.variant == "plain" && (s.version == "24.12.31" || s.version == "26.9.9") {
			rows = append(rows, row{srv: s, fingerprint: "firefox"}, row{srv: s, fingerprint: "safari"})
		}
	}

	type scenario struct {
		name   string
		bin    string
		keys   map[string]any
		expect func(row) bool
	}
	var scenarios []scenario
	if patched != "" {
		scenarios = append(scenarios,
			scenario{"patched/no-keys", patched, nil, expectPatched("auto", "26.9.9")},
			scenario{"patched/auto", patched, map[string]any{"reality-mlkem": "auto"}, expectPatched("auto", "26.9.9")},
			scenario{"patched/off", patched, map[string]any{"reality-mlkem": "off"}, expectPatched("off", "26.9.9")},
			scenario{"patched/on", patched, map[string]any{"reality-mlkem": "on"}, expectPatched("on", "26.9.9")},
			scenario{"patched/version-1.9.0", patched, map[string]any{"reality-client-version": "1.9.0"}, expectPatched("auto", "1.9.0")},
		)
	}
	if stock != "" {
		// Same keys as the app writes: an unpatched core must ignore them.
		scenarios = append(scenarios, scenario{"stock/with-keys", stock,
			map[string]any{"reality-mlkem": "on", "reality-client-version": "26.9.9"}, expectStock})
	}

	for _, sc := range scenarios {
		t.Run(sc.name, func(t *testing.T) {
			ports := startCore(t, sc.bin, sc.keys, rows)
			got := make([]bool, len(rows))
			var wg sync.WaitGroup
			for i := range rows {
				wg.Add(1)
				go func() {
					defer wg.Done()
					got[i] = probe(ports[i], 3)
				}()
			}
			wg.Wait()
			ok := 0
			for i, r := range rows {
				want := sc.expect(r)
				mark := "  "
				if got[i] != want {
					mark = "!!"
					t.Errorf("%s: got ok=%v, want ok=%v", r.name(), got[i], want)
				}
				if got[i] {
					ok++
				}
				t.Logf("%s %-34s got=%-5v want=%v", mark, r.name(), got[i], want)
			}
			t.Logf("%s: %d/%d OK", sc.name, ok, len(rows))
		})
	}
}

func startServers(t *testing.T) []*server {
	t.Helper()
	dir := t.TempDir()
	run := strconv.FormatInt(time.Now().UnixNano()%1e8, 36)
	var servers []*server
	for _, version := range xrayVersions {
		for _, variant := range []string{"plain", "minver", "maxver"} {
			reality := map[string]any{
				"show": false, "dest": labSNI + ":443", "xver": 0, "serverNames": []string{labSNI},
				"privateKey": labPrivateKey, "shortIds": []string{labShortID},
			}
			for k, v := range variants[variant] {
				reality[k] = v
			}
			cfg := map[string]any{
				"log": map[string]any{"loglevel": "warning"},
				// DoH by IP: a host-side Clash may answer plain DNS from containers with fake-ip.
				"dns": map[string]any{"servers": []string{"https://1.1.1.1/dns-query", "https://8.8.8.8/dns-query"}, "queryStrategy": "UseIPv4"},
				"inbounds": []any{map[string]any{
					"listen": "0.0.0.0", "port": innerPort, "protocol": "vless",
					"settings":       map[string]any{"clients": []any{map[string]any{"id": labUUID, "flow": "xtls-rprx-vision"}}, "decryption": "none"},
					"streamSettings": map[string]any{"network": "tcp", "security": "reality", "realitySettings": reality},
				}},
				"outbounds": []any{map[string]any{"protocol": "freedom", "settings": map[string]any{"domainStrategy": "UseIPv4"}}},
			}
			cdir := filepath.Join(dir, version+"-"+variant)
			writeJSON(t, filepath.Join(cdir, "config.json"), cfg)
			name := fmt.Sprintf("sloth-xlab-%s-%s-%s", run, version, variant)
			out, err := exec.Command("docker", "run", "-d", "--name", name,
				"-p", fmt.Sprintf("127.0.0.1::%d", innerPort),
				"-v", filepath.ToSlash(cdir)+":/etc/xray:ro", "teddysun/xray:"+version).CombinedOutput()
			if err != nil {
				t.Fatalf("docker run %s: %v\n%s", name, err, out)
			}
			t.Cleanup(func() { _ = exec.Command("docker", "rm", "-f", name).Run() })
			out, err = exec.Command("docker", "port", name, fmt.Sprintf("%d/tcp", innerPort)).Output()
			if err != nil {
				t.Fatalf("docker port %s: %v", name, err)
			}
			_, portStr, err := net.SplitHostPort(strings.TrimSpace(strings.SplitN(string(out), "\n", 2)[0]))
			if err != nil {
				t.Fatalf("docker port %s: %q", name, out)
			}
			port, _ := strconv.Atoi(portStr)
			servers = append(servers, &server{version: version, variant: variant, port: port})
		}
	}
	for _, s := range servers {
		waitTCP(t, s.port, 30*time.Second)
	}
	return servers
}

// startCore runs the core with one mixed listener per row and returns the ports.
func startCore(t *testing.T, bin string, keys map[string]any, rows []row) []int {
	t.Helper()
	home := t.TempDir()
	var proxies, listeners []any
	ports := make([]int, len(rows))
	for i, r := range rows {
		opts := map[string]any{"public-key": labPublicKey, "short-id": labShortID}
		if r.forced {
			opts["support-x25519mlkem768"] = true
		}
		name := fmt.Sprintf("p%d", i)
		proxies = append(proxies, map[string]any{
			"name": name, "type": "vless", "server": "127.0.0.1", "port": r.srv.port, "uuid": labUUID,
			"flow": "xtls-rprx-vision", "tls": true, "servername": labSNI, "client-fingerprint": r.fingerprint,
			"network": "tcp", "udp": true, "reality-opts": opts,
		})
		ports[i] = freePort(t)
		listeners = append(listeners, map[string]any{"name": "l" + name, "type": "mixed", "listen": "127.0.0.1", "port": ports[i], "proxy": name})
	}
	cfg := map[string]any{
		"mixed-port": 0, "log-level": "info", "mode": "global", "ipv6": false,
		"dns":     map[string]any{"enable": true, "nameserver": []string{"1.1.1.1"}},
		"proxies": proxies, "listeners": listeners, "rules": []string{"MATCH,DIRECT"},
	}
	for k, v := range keys {
		cfg[k] = v
	}
	writeJSON(t, filepath.Join(home, "config.yaml"), cfg) // JSON is valid YAML

	logf, err := os.Create(filepath.Join(home, "core.log"))
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
			if b, err := os.ReadFile(logf.Name()); err == nil {
				t.Logf("core log (REALITY lines):\n%s", grepLines(string(b), "REALITY"))
			}
		}
	})
	waitTCP(t, ports[len(ports)-1], 20*time.Second)
	return ports
}

// probe reports whether any of n HTTPS requests through the listener returns
// 204. Each attempt opens a fresh proxied connection (fresh REALITY handshake).
func probe(port, n int) bool {
	proxy, _ := url.Parse(fmt.Sprintf("http://127.0.0.1:%d", port))
	for i := 0; i < n; i++ {
		client := &http.Client{
			Timeout:   8 * time.Second,
			Transport: &http.Transport{Proxy: http.ProxyURL(proxy), DisableKeepAlives: true},
		}
		resp, err := client.Get(probeURL)
		if err == nil {
			resp.Body.Close()
			if resp.StatusCode == http.StatusNoContent {
				return true
			}
		}
	}
	return false
}

func cmpVersion(a, b string) int {
	pa, pb := strings.Split(a, "."), strings.Split(b, ".")
	for i := 0; i < 3; i++ {
		x, _ := strconv.Atoi(pa[i])
		y, _ := strconv.Atoi(pb[i])
		if x != y {
			if x < y {
				return -1
			}
			return 1
		}
	}
	return 0
}

func writeJSON(t *testing.T, path string, v any) {
	t.Helper()
	b, err := json.MarshalIndent(v, "", " ")
	if err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, b, 0o644); err != nil {
		t.Fatal(err)
	}
}

func freePort(t *testing.T) int {
	t.Helper()
	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer l.Close()
	return l.Addr().(*net.TCPAddr).Port
}

func waitTCP(t *testing.T, port int, d time.Duration) {
	t.Helper()
	deadline := time.Now().Add(d)
	for time.Now().Before(deadline) {
		if c, err := net.DialTimeout("tcp", fmt.Sprintf("127.0.0.1:%d", port), time.Second); err == nil {
			c.Close()
			return
		}
		time.Sleep(200 * time.Millisecond)
	}
	t.Fatalf("127.0.0.1:%d not reachable after %v", port, d)
}

func grepLines(s, needle string) string {
	var out []string
	for _, l := range strings.Split(s, "\n") {
		if strings.Contains(l, needle) {
			out = append(out, l)
		}
	}
	return strings.Join(out, "\n")
}
