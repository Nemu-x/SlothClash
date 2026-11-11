package realitylab

import (
	"bytes"
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"
)

// xhttpServers are the Xray servers the XHTTP packet-up check runs against:
// the oldest the lab covers, one 25.x and the newest (core patch 0037 keeps
// several uploads in flight; every generation must put them back in order),
// and the newest again with scMaxBufferedPosts at the smallest value 0037's
// window of 8 uploads stays within (the default is 30).
var xhttpServers = []struct {
	version          string
	maxBufferedPosts int // 0: the server's default
}{{"24.12.31", 0}, {"25.7.25", 0}, {"26.11.9", 0}, {"26.11.9", 7}}

// xhttpUploadBytes is the payload of each packet-up upload: 16 uploads of
// sc-max-each-post-bytes (1 MB by default) and more.
const xhttpUploadBytes = 16 << 20

// xhttpListenerBase is the listener band of the XHTTP check, above the
// REALITY matrix's scenarios.
const xhttpListenerBase = listenerPortBase + 20*listenerPortStride

// TestXHTTPPacketUp sends xhttpUploadBytes through VLESS-REALITY-XHTTP in
// packet-up mode to an HTTP endpoint on the host that answers the SHA-256 of
// what it got, for every core under test and every server in xhttpVersions.
// The hash must match; the upload speed is logged.
func TestXHTTPPacketUp(t *testing.T) {
	if os.Getenv("XRAY_LAB") != "1" {
		t.Skip("set XRAY_LAB=1 (needs Docker + internet) to run the REALITY lab")
	}
	cores := map[string]string{"patched": os.Getenv("SLOTH_MIHOMO_BIN"), "stock": os.Getenv("SLOTH_MIHOMO_STOCK_BIN")}
	if cores["patched"] == "" && cores["stock"] == "" {
		t.Fatal("set SLOTH_MIHOMO_BIN (patched core) and/or SLOTH_MIHOMO_STOCK_BIN")
	}

	sumPort := startSumServer(t)
	servers := startXHTTPServers(t)
	target := fmt.Sprintf("http://host.docker.internal:%d/sum", sumPort)

	for ci, name := range []string{"patched", "stock"} {
		bin := cores[name]
		if bin == "" {
			continue
		}
		t.Run(name, func(t *testing.T) {
			ports := startXHTTPCore(t, bin, servers, xhttpListenerBase+ci*listenerPortStride)
			for i, s := range servers {
				t.Run(s.version+"-"+s.variant, func(t *testing.T) {
					// The first connection to a fresh Xray 25.7.25
					// XHTTP-REALITY server stalls about 5 s, with the stock
					// core too: warm up so the timing below is the upload's.
					_, err := uploadSum(ports[i], target, 64<<10)
					var mbps float64
					if err == nil {
						mbps, err = uploadSum(ports[i], target, xhttpUploadBytes)
					}
					if err != nil {
						if out, lerr := exec.Command("docker", "logs", "--tail", "40", s.container).CombinedOutput(); lerr == nil {
							t.Logf("server log %s:\n%s", s.container, out)
						}
						t.Fatalf("packet-up upload through Xray %s %s: %v", s.version, s.variant, err)
					}
					t.Logf("%s -> Xray %s %s: %d MiB intact, %.0f Mbit/s", name, s.version, s.variant, xhttpUploadBytes>>20, mbps)
				})
			}
		})
	}
}

// startSumServer serves POST /sum (the SHA-256 of the body, hex) on every
// interface, so the containers reach it as host.docker.internal.
func startSumServer(t *testing.T) int {
	t.Helper()
	ln, err := net.Listen("tcp", ":0")
	if err != nil {
		t.Fatal(err)
	}
	srv := &http.Server{Handler: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		h := sha256.New()
		if _, err := io.Copy(h, r.Body); err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		io.WriteString(w, hex.EncodeToString(h.Sum(nil)))
	}), ReadHeaderTimeout: 10 * time.Second}
	go srv.Serve(ln)
	t.Cleanup(func() { srv.Close() })
	return ln.Addr().(*net.TCPAddr).Port
}

func startXHTTPServers(t *testing.T) []*server {
	t.Helper()
	dir := t.TempDir()
	run := strconv.FormatInt(time.Now().UnixNano()%1e8, 36)
	var servers []*server
	for _, xs := range xhttpServers {
		version, variant := xs.version, "default"
		cfg := map[string]any{
			"log": map[string]any{"loglevel": "warning"},
			"inbounds": []any{map[string]any{
				"listen": "0.0.0.0", "port": innerPort, "protocol": "vless",
				"settings": map[string]any{"clients": []any{map[string]any{"id": labUUID}}, "decryption": "none"},
				"streamSettings": map[string]any{
					"network": "xhttp", "xhttpSettings": map[string]any{"path": "/xh", "mode": "packet-up"},
					"security": "reality", "realitySettings": map[string]any{
						"show": false, "dest": labSNI + ":443", "xver": 0, "serverNames": []string{labSNI},
						"privateKey": labPrivateKey, "shortIds": []string{labShortID},
					},
				},
			}},
			// From 26.x freedom refuses private addresses unless a final rule
			// allows them; older releases ignore the key.
			"outbounds": []any{map[string]any{"protocol": "freedom", "settings": map[string]any{
				"finalRules": []any{map[string]any{"action": "allow"}},
			}}},
		}
		xhttp := cfg["inbounds"].([]any)[0].(map[string]any)["streamSettings"].(map[string]any)["xhttpSettings"].(map[string]any)
		if cmpVersion(version, "25.1.30") < 0 {
			// Xray before 25.1.30 reads x_padding from the URL query only, and
			// mihomo sends it in the Referer header: accept no padding.
			xhttp["xPaddingBytes"] = "0-1000"
		}
		if xs.maxBufferedPosts > 0 {
			xhttp["scMaxBufferedPosts"] = xs.maxBufferedPosts
			variant = fmt.Sprintf("max-buffered-posts-%d", xs.maxBufferedPosts)
		}
		cdir := filepath.Join(dir, version+"-"+variant)
		writeJSON(t, filepath.Join(cdir, "config.json"), cfg)
		name := fmt.Sprintf("sloth-xlab-%s-%s-xhttp-%s", run, version, variant)
		out, err := exec.Command("docker", "run", "-d", "--name", name,
			"--add-host", "host.docker.internal:host-gateway",
			"-p", fmt.Sprintf("127.0.0.1::%d", innerPort),
			"-v", filepath.ToSlash(cdir)+":/etc/xray:ro", xrayImage(t, version)).CombinedOutput()
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
		servers = append(servers, &server{version: version, variant: variant, port: port, container: name})
	}
	for _, s := range servers {
		waitTCP(t, s.port, 30*time.Second)
	}
	return servers
}

// startXHTTPCore runs the core with one mixed listener per server, each bound
// to a VLESS-REALITY-XHTTP packet-up proxy for it. The proxies force the
// hybrid key share only where the server requires it, so the stock core gets
// through to every generation as well.
func startXHTTPCore(t *testing.T, bin string, servers []*server, portBase int) []int {
	t.Helper()
	home := t.TempDir()
	var proxies, listeners []any
	ports := reservePorts(t, portBase, len(servers))
	for i, s := range servers {
		opts := map[string]any{"public-key": labPublicKey, "short-id": labShortID}
		if !acceptsShare(s.version, false) {
			opts["support-x25519mlkem768"] = true
		}
		name := fmt.Sprintf("x%d", i)
		proxies = append(proxies, map[string]any{
			"name": name, "type": "vless", "server": "127.0.0.1", "port": s.port, "uuid": labUUID,
			"tls": true, "servername": labSNI, "client-fingerprint": "chrome", "alpn": []string{"h2"},
			"network": "xhttp", "xhttp-opts": map[string]any{"path": "/xh", "mode": "packet-up"},
			"reality-opts": opts,
		})
		listeners = append(listeners, map[string]any{"name": "l" + name, "type": "mixed", "listen": "127.0.0.1", "port": ports[i], "proxy": name})
	}
	cfg := map[string]any{
		"mixed-port": 0, "log-level": "info", "mode": "global", "ipv6": false,
		"proxies": proxies, "listeners": listeners, "rules": []string{"MATCH,DIRECT"},
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
				t.Logf("core log:\n%s", b)
			}
		}
	})
	for _, p := range ports {
		waitTCP(t, p, 20*time.Second)
	}
	return ports
}

// uploadSum POSTs size random bytes to target through the HTTP proxy on the
// port and checks the SHA-256 the target answers. It returns the upload speed.
func uploadSum(port int, target string, size int) (float64, error) {
	proxy, _ := url.Parse(fmt.Sprintf("http://127.0.0.1:%d", port))
	client := &http.Client{
		Timeout:   2 * time.Minute,
		Transport: &http.Transport{Proxy: http.ProxyURL(proxy), DisableKeepAlives: true},
	}
	blob := make([]byte, size)
	rand.Read(blob)
	want := sha256.Sum256(blob)
	start := time.Now()
	resp, err := client.Post(target, "application/octet-stream", bytes.NewReader(blob))
	if err != nil {
		return 0, err
	}
	body, err := io.ReadAll(resp.Body)
	resp.Body.Close()
	took := time.Since(start)
	if err != nil {
		return 0, err
	}
	if resp.StatusCode != http.StatusOK || string(body) != hex.EncodeToString(want[:]) {
		return 0, fmt.Errorf("HTTP %d, body %q: the payload did not arrive intact", resp.StatusCode, body)
	}
	return float64(size) * 8 / took.Seconds() / 1e6, nil
}
