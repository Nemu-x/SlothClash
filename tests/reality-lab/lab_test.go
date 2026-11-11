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
	"archive/zip"
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/json"
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
// a classic one is rejected, in between both are accepted. 26.7.11 and 26.7.28
// also refuse clients below 26.3.27 when the server sets no minClientVer (the
// default of XTLS/Xray-core af7eb680, commented out again in 47cfe999, the
// 26.9.8 change that also requires the hybrid share). 26.9.30, 26.10.21,
// 26.10.29 and 26.11.9 track the current releases (26.11.9 sends Chrome 155 as
// its default fingerprint).
var xrayVersions = []string{"24.12.31", "25.7.25", "26.4.13", "26.7.11", "26.7.28", "26.9.9", "26.9.30", "26.10.21", "26.10.29", "26.11.9"}

// browserVersions get firefox, safari, chrome155 and no-fingerprint rows (Auto
// and forced) next to chrome: one per key-share generation plus every current
// release, because the firefox (Firefox 148) and safari (Safari 26.3) parrots
// of metacubex/utls v1.9.0-alpha.1 (mihomo v1.19.34) carry the hybrid share,
// 0029 adds the Chrome 155 hello and 0041 dials REALITY without a fingerprint.
var browserVersions = map[string]bool{"24.12.31": true, "26.4.13": true, "26.9.9": true, "26.9.30": true, "26.10.21": true, "26.10.29": true, "26.11.9": true}

// officialXray are releases teddysun/xray has not published (yet): the lab
// builds their image from the official release zip, pinned by the SHA-256 of
// Xray-linux-64.zip (the SHA2-256 line of its .dgst), on top of the newest
// teddysun image so the layout (binary, config path, CA bundle) is the same.
var officialXray = map[string]string{
	"26.11.9": "b651f7e1d9966ee2c1b1ee1bf8b52c72771824d3065dce96bbc8bcc19ed95f82",
}

const officialXrayBase = "teddysun/xray:26.10.29"

// patchedClientVersion is the REALITY client version the patched core sends
// by default (core patch 0029).
const patchedClientVersion = "26.11.9"

// hybridParrot reports whether the fingerprint's hello carries the
// X25519MLKEM768 share: chrome (Chrome 133), chrome155 (0029), and from
// mihomo v1.19.34 (metacubex/utls v1.9.0-alpha.1) firefox (Firefox 148) and
// safari (Safari 26.3) too, and Xray's hellofirefox_148 / hellosafari_26_3,
// which 0008 maps to those parrots. No fingerprint is chrome in the patched
// core (0041); stock mihomo never dials it.
func hybridParrot(fingerprint string) bool {
	switch fingerprint {
	case "", "chrome", "chrome155", "firefox", "safari", "hellofirefox_148", "hellosafari_26_3":
		return true
	}
	return false
}

// defaultMinClientVer is the minClientVer an Xray server applies when its
// config sets none: 26.3.27 from 26.7.11 until 26.9.8, none otherwise.
func defaultMinClientVer(version string) string {
	if cmpVersion(version, "26.7.11") >= 0 && cmpVersion(version, "26.9.8") < 0 {
		return "26.3.27"
	}
	return ""
}

var variants = map[string]map[string]any{
	"plain":  {},
	"minver": {"minClientVer": "25.1.1"},
	"maxver": {"maxClientVer": "2.0.0"},
}

type server struct {
	version, variant string
	port             int
	container        string
}

type row struct {
	srv         *server
	fingerprint string
	forced      bool // per-proxy support-x25519mlkem768: true
}

func (r row) name() string {
	fingerprint := r.fingerprint
	if fingerprint == "" {
		fingerprint = "nofp"
	}
	n := fmt.Sprintf("%s-%s-%s", r.srv.version, r.srv.variant, fingerprint)
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
	minVer := defaultMinClientVer(r.srv.version)
	if r.srv.variant == "minver" {
		minVer = "25.1.1" // an explicit minClientVer replaces the default
	}
	if minVer != "" && cmpVersion(clientVersion, minVer) < 0 {
		return false
	}
	if r.srv.variant == "maxver" && cmpVersion(clientVersion, "2.0.0") > 0 {
		return false
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
		case r.forced && policy == "off":
			// reality-mlkem off keeps the stock behaviour: the forced share
			// goes out from every parrot that has it.
			return acceptsShare(r.srv.version, hybridParrot(r.fingerprint))
		case r.forced || policy == "on":
			return acceptsShare(r.srv.version, hybridParrot(r.fingerprint))
		case policy == "off":
			return acceptsShare(r.srv.version, false)
		}
		return true // auto: one of the two shares works on every generation
	}
}

// expectStock models stock mihomo: client version 1.8.2, hybrid share only
// when forced and only from a parrot that has it (chrome, and from v1.19.34
// firefox and safari), unknown top-level keys ignored. It does not know
// chrome155 or Xray's names (0008), and REALITY needs a fingerprint it knows
// (without one every dial fails; 0041 makes it chrome).
func expectStock(r row) bool {
	if r.fingerprint == "" || r.fingerprint == "chrome155" || r.fingerprint == "hellofirefox_148" || r.fingerprint == "hellosafari_26_3" || !versionGate(r, "1.8.2") {
		return false
	}
	return acceptsShare(r.srv.version, r.forced && hybridParrot(r.fingerprint))
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
		if s.variant == "plain" && browserVersions[s.version] {
			for _, fp := range []string{"", "firefox", "safari", "chrome155", "hellofirefox_148", "hellosafari_26_3"} {
				rows = append(rows, row{srv: s, fingerprint: fp}, row{srv: s, fingerprint: fp, forced: true})
			}
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
			scenario{"patched/no-keys", patched, nil, expectPatched("auto", patchedClientVersion)},
			scenario{"patched/auto", patched, map[string]any{"reality-mlkem": "auto"}, expectPatched("auto", patchedClientVersion)},
			scenario{"patched/off", patched, map[string]any{"reality-mlkem": "off"}, expectPatched("off", patchedClientVersion)},
			scenario{"patched/on", patched, map[string]any{"reality-mlkem": "on"}, expectPatched("on", patchedClientVersion)},
			scenario{"patched/version-1.9.0", patched, map[string]any{"reality-client-version": "1.9.0"}, expectPatched("auto", "1.9.0")},
		)
	}
	if stock != "" {
		// Same keys as the app writes: an unpatched core must ignore them.
		scenarios = append(scenarios, scenario{"stock/with-keys", stock,
			map[string]any{"reality-mlkem": "on", "reality-client-version": "26.9.9"}, expectStock})
	}

	for si, sc := range scenarios {
		t.Run(sc.name, func(t *testing.T) {
			ports, coreLog := startCore(t, sc.bin, sc.keys, rows, listenerPortBase+si*listenerPortStride)
			got := make([]bool, len(rows))
			detail := make([]string, len(rows))
			// Bounded parallelism: every probe is a fresh REALITY handshake plus
			// a TLS fetch through the server's egress, and dozens at once on a
			// small runner time out for reasons that have nothing to do with
			// the core.
			sem := make(chan struct{}, probeParallel)
			var wg sync.WaitGroup
			for i := range rows {
				wg.Add(1)
				go func() {
					defer wg.Done()
					sem <- struct{}{}
					defer func() { <-sem }()
					got[i], detail[i] = probe(ports[i], 3)
				}()
			}
			wg.Wait()
			ok := 0
			for i, r := range rows {
				want := sc.expect(r)
				mark := "  "
				if got[i] != want {
					// A mismatch counts only if it reproduces: probe the cell
					// again, alone. Both rounds are logged either way.
					again, againDetail := probe(ports[i], 3)
					if again == want {
						mark = "~~"
						t.Logf("flaky %s: first round ok=%v [%s], recheck ok=%v [%s]", r.name(), got[i], detail[i], again, againDetail)
						got[i] = again
					} else {
						mark = "!!"
						t.Errorf("%s: got ok=%v, want ok=%v; round 1 [%s], round 2 [%s]", r.name(), got[i], want, detail[i], againDetail)
						// Evidence for the failing cell: what the core logged
						// for its proxy and what the server logged.
						if b, err := os.ReadFile(coreLog); err == nil {
							t.Logf("core log for p%d:\n%s", i, grepLines(string(b), fmt.Sprintf(" p%d ", i)))
						}
						if out, err := exec.Command("docker", "logs", "--tail", "60", r.srv.container).CombinedOutput(); err == nil {
							t.Logf("server log %s:\n%s", r.srv.container, out)
						}
					}
				}
				if got[i] {
					ok++
				}
				t.Logf("%s %-34s got=%-5v want=%v", mark, r.name(), got[i], want)
			}
			t.Logf("%s: %d/%d OK", sc.name, ok, len(rows))
			if strings.HasPrefix(sc.name, "patched/") {
				checkBrowserHellos(t, coreLog, sc.name)
			}
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
				"log": map[string]any{"loglevel": "info"},
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
	}
	for _, s := range servers {
		waitTCP(t, s.port, 30*time.Second)
	}
	return servers
}

// Listener ports come from a fixed band below every ephemeral range (Linux
// 32768+, Windows 49152+, Docker's published ports live there too), one block
// per scenario. They used to come from freePort(): bind :0, close, reuse later.
// That hands out the same port twice in ~11% of 54-row scenarios (measured on
// Linux), so two proxies shared a listener and a probe reached the wrong
// proxy — a random cell flipped in either direction, ~half of all lab runs.
const (
	listenerPortBase   = 20000
	listenerPortStride = 200
)

// reservePorts returns n distinct ports from base upward that are free right
// now, skipping any already taken.
func reservePorts(t *testing.T, base, n int) []int {
	t.Helper()
	ports := make([]int, 0, n)
	for p := base; len(ports) < n && p < base+listenerPortStride; p++ {
		l, err := net.Listen("tcp", fmt.Sprintf("127.0.0.1:%d", p))
		if err != nil {
			continue
		}
		l.Close()
		ports = append(ports, p)
	}
	if len(ports) < n {
		t.Fatalf("only %d free ports in %d..%d, need %d", len(ports), base, base+listenerPortStride-1, n)
	}
	return ports
}

// startCore runs the core with one mixed listener per row and returns the ports.
func startCore(t *testing.T, bin string, keys map[string]any, rows []row, portBase int) ([]int, string) {
	t.Helper()
	home := t.TempDir()
	var proxies, listeners []any
	ports := reservePorts(t, portBase, len(rows))
	for i, r := range rows {
		opts := map[string]any{"public-key": labPublicKey, "short-id": labShortID}
		if r.forced {
			opts["support-x25519mlkem768"] = true
		}
		name := fmt.Sprintf("p%d", i)
		proxy := map[string]any{
			"name": name, "type": "vless", "server": "127.0.0.1", "port": r.srv.port, "uuid": labUUID,
			"flow": "xtls-rprx-vision", "tls": true, "servername": labSNI,
			"network": "tcp", "udp": true, "reality-opts": opts,
		}
		if r.fingerprint != "" {
			proxy["client-fingerprint"] = r.fingerprint
		}
		proxies = append(proxies, proxy)
		listeners = append(listeners, map[string]any{"name": "l" + name, "type": "mixed", "listen": "127.0.0.1", "port": ports[i], "proxy": name})
	}
	cfg := map[string]any{
		"mixed-port": 0, "log-level": "debug", "mode": "global", "ipv6": false,
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
	// Every listener must be the core's own: wait for all of them, then make
	// sure the core did not fail to bind any (a probe would hit nothing or
	// someone else).
	for _, p := range ports {
		waitTCP(t, p, 20*time.Second)
	}
	if b, err := os.ReadFile(logf.Name()); err == nil {
		if bad := grepLines(strings.ToLower(string(b)), "address already in use"); bad != "" {
			t.Fatalf("core could not bind a listener:\n%s", bad)
		}
	}
	return ports, logf.Name()
}

// xrayImage returns the image of an Xray release: teddysun/xray, or for the
// officialXray releases an image built here from the official zip.
func xrayImage(t *testing.T, version string) string {
	t.Helper()
	sum, ok := officialXray[version]
	if !ok {
		return "teddysun/xray:" + version
	}
	image := "sloth-xlab/xray:" + version
	if exec.Command("docker", "image", "inspect", image).Run() == nil {
		return image
	}
	officialImageMu.Lock()
	defer officialImageMu.Unlock()
	if exec.Command("docker", "image", "inspect", image).Run() == nil {
		return image
	}
	dir := t.TempDir()
	zipURL := "https://github.com/XTLS/Xray-core/releases/download/v" + version + "/Xray-linux-64.zip"
	resp, err := http.Get(zipURL)
	if err != nil {
		t.Fatalf("download %s: %v", zipURL, err)
	}
	data, err := io.ReadAll(resp.Body)
	resp.Body.Close()
	if err != nil || resp.StatusCode != http.StatusOK {
		t.Fatalf("download %s: HTTP %d, %v", zipURL, resp.StatusCode, err)
	}
	if got := fmt.Sprintf("%x", sha256.Sum256(data)); got != sum {
		t.Fatalf("%s: SHA-256 %s, pinned %s", zipURL, got, sum)
	}
	zr, err := zip.NewReader(bytes.NewReader(data), int64(len(data)))
	if err != nil {
		t.Fatal(err)
	}
	var bin []byte
	for _, f := range zr.File {
		if f.Name == "xray" {
			rc, err := f.Open()
			if err != nil {
				t.Fatal(err)
			}
			bin, err = io.ReadAll(rc)
			rc.Close()
			if err != nil {
				t.Fatal(err)
			}
		}
	}
	if bin == nil {
		t.Fatalf("%s has no xray binary", zipURL)
	}
	if err := os.WriteFile(filepath.Join(dir, "xray"), bin, 0o755); err != nil {
		t.Fatal(err)
	}
	dockerfile := "FROM " + officialXrayBase + "\nCOPY --chmod=755 xray /usr/bin/xray\n"
	if err := os.WriteFile(filepath.Join(dir, "Dockerfile"), []byte(dockerfile), 0o644); err != nil {
		t.Fatal(err)
	}
	if out, err := exec.Command("docker", "build", "-q", "-t", image, dir).CombinedOutput(); err != nil {
		t.Fatalf("docker build %s: %v\n%s", image, err, out)
	}
	if out, err := exec.Command("docker", "run", "--rm", "--entrypoint", "/usr/bin/xray", image, "version").CombinedOutput(); err != nil || !strings.Contains(string(out), "Xray "+version+" ") {
		t.Fatalf("%s reports %q (%v), want Xray %s", image, out, err, version)
	}
	return image
}

var officialImageMu sync.Mutex

// checkBrowserHellos asserts what core patches 0002 and 0029 promise: the
// chrome155 hello is built by 0029 and never falls back to Chrome 133, and the
// browser parrots that carry the hybrid share (from mihomo v1.19.34 firefox and
// safari too) are never reported as unable to offer it. Needs the core at
// log-level debug.
func checkBrowserHellos(t *testing.T, coreLog, scenario string) {
	t.Helper()
	b, err := os.ReadFile(coreLog)
	if err != nil {
		t.Fatalf("read core log: %v", err)
	}
	logText := string(b)
	// chrome155 rows: 0029 builds the hello itself; a fallback to Chrome 133
	// would pass every probe and still be wrong.
	if bad := grepLines(logText, "chrome155 fingerprint:"); bad != "" {
		t.Errorf("%s: a chrome155 hello fell back:\n%s", scenario, bad)
	}
	for _, client := range []string{"Firefox", "Safari", "Chrome"} {
		if bad := grepLines(logText, "the "+client+" fingerprint cannot offer X25519MLKEM768"); bad != "" {
			t.Errorf("%s: the %s parrot was sent without the hybrid share:\n%s", scenario, client, bad)
		}
	}
}

// probeParallel bounds how many cells are probed at once.
const probeParallel = 8

// probe reports whether any of n HTTPS requests through the listener returns
// 204, and what each attempt saw. Each attempt opens a fresh proxied
// connection (fresh REALITY handshake).
func probe(port, n int) (bool, string) {
	proxy, _ := url.Parse(fmt.Sprintf("http://127.0.0.1:%d", port))
	var seen []string
	for i := 0; i < n; i++ {
		client := &http.Client{
			Timeout:   8 * time.Second,
			Transport: &http.Transport{Proxy: http.ProxyURL(proxy), DisableKeepAlives: true},
		}
		start := time.Now()
		resp, err := client.Get(probeURL)
		took := time.Since(start).Round(10 * time.Millisecond)
		if err != nil {
			seen = append(seen, fmt.Sprintf("#%d %v: %v", i+1, took, err))
			continue
		}
		resp.Body.Close()
		seen = append(seen, fmt.Sprintf("#%d %v: HTTP %d", i+1, took, resp.StatusCode))
		if resp.StatusCode == http.StatusNoContent {
			return true, strings.Join(seen, "; ")
		}
	}
	return false, strings.Join(seen, "; ")
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
