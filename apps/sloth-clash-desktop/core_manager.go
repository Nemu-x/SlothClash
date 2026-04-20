package main

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"net"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"sort"
	"strconv"
	"strings"
	"time"

	"gopkg.in/yaml.v3"
)

func slothDataRoot() (string, error) {
	d, err := os.UserConfigDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(d, "SlothClash"), nil
}

func randomSecret() string {
	b := make([]byte, 16)
	if _, err := rand.Read(b); err != nil {
		return "sloth-" + strconv.FormatInt(time.Now().UnixNano(), 36)
	}
	return hex.EncodeToString(b)
}

func pickFreePort() (int, error) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		return 0, err
	}
	addr := ln.Addr().String()
	_ = ln.Close()
	_, p, err := net.SplitHostPort(addr)
	if err != nil {
		return 0, err
	}
	return strconv.Atoi(p)
}

// winPipePathPrefix is the canonical Windows named-pipe path prefix (\\.\pipe\name).
// See: https://learn.microsoft.com/en-us/windows/win32/ipc/pipe-names
const winPipePathPrefix = `\\.\pipe\`

func isWinPipeEndpoint(addr string) bool {
	s := strings.TrimSpace(addr)
	if len(s) < len(winPipePathPrefix) {
		return false
	}
	// Pipe names are case-insensitive; host part must not be parsed as a URL host.
	return strings.EqualFold(s[:len(winPipePathPrefix)], winPipePathPrefix)
}

// effectiveCoreEndpointLocked returns the mihomo API address (TCP host:port or \\.\pipe\...).
// Caller must hold a.mu (Lock or RLock). Prefer coreListen; fall back to published Core.ControllerAddr.
func (a *App) effectiveCoreEndpointLocked() string {
	if s := strings.TrimSpace(a.coreListen); s != "" {
		return s
	}
	return strings.TrimSpace(a.state.Core.ControllerAddr)
}

func slothMihomoPipeName(profileID string) string {
	var b strings.Builder
	b.WriteString(`\\.\pipe\sloth-mihomo-`)
	for _, r := range profileID {
		switch {
		case (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') || (r >= '0' && r <= '9') || r == '-' || r == '_':
			b.WriteRune(r)
		default:
			b.WriteByte('-')
		}
	}
	s := b.String()
	const maxLen = 120
	if len(s) > maxLen {
		s = s[:maxLen]
	}
	if s == `\\.\pipe\sloth-mihomo-` {
		return `\\.\pipe\sloth-mihomo-default`
	}
	return s
}

func mihomoSidecarSearchDirs() []string {
	seen := map[string]bool{}
	var out []string
	add := func(p string) {
		if strings.TrimSpace(p) == "" {
			return
		}
		abs, err := filepath.Abs(p)
		if err != nil {
			abs = p
		}
		abs = filepath.Clean(abs)
		if seen[abs] {
			return
		}
		seen[abs] = true
		out = append(out, abs)
	}

	if exe, err := os.Executable(); err == nil {
		exeDir := filepath.Dir(exe)
		add(exeDir)
		add(filepath.Join(exeDir, "sidecar"))
		add(filepath.Join(exeDir, "build", "sidecar"))
		add(filepath.Join(filepath.Dir(exeDir), "sidecar"))
		add(filepath.Join(filepath.Dir(exeDir), "build", "sidecar"))
	}
	if v := strings.TrimSpace(os.Getenv("SLOTH_CLASH_DESKTOP_ROOT")); v != "" {
		add(filepath.Join(v, "build", "sidecar"))
	}
	wd, err := os.Getwd()
	if err != nil {
		return out
	}
	d := wd
	for i := 0; i < 14; i++ {
		// wails dev: cwd is often apps/sloth-clash-desktop OR monorepo root
		add(filepath.Join(d, "build", "sidecar"))
		add(filepath.Join(d, "apps", "sloth-clash-desktop", "build", "sidecar"))
		parent := filepath.Dir(d)
		if parent == d {
			break
		}
		d = parent
	}
	return out
}

func (a *App) resolveMihomoBinary() (string, error) {
	if p := strings.TrimSpace(os.Getenv("SLOTH_MIHOMO_PATH")); p != "" {
		if st, err := os.Stat(p); err == nil && !st.IsDir() {
			return p, nil
		}
	}

	var patterns []string
	for _, dir := range mihomoSidecarSearchDirs() {
		patterns = append(patterns,
			filepath.Join(dir, "verge-mihomo*.exe"),
			filepath.Join(dir, "verge-mihomo*"),
			filepath.Join(dir, "sloth-mihomo*.exe"),
			filepath.Join(dir, "sloth-mihomo*"),
		)
	}
	for _, preferNoAlpha := range []bool{true, false} {
		for _, pat := range patterns {
			matches, _ := filepath.Glob(pat)
			sort.Strings(matches)
			for _, m := range matches {
				if preferNoAlpha && strings.Contains(strings.ToLower(filepath.Base(m)), "alpha") {
					continue
				}
				if st, err := os.Stat(m); err == nil && !st.IsDir() {
					return m, nil
				}
			}
		}
	}
	if p, err := a.extractBundledMihomoBinary(); err == nil && strings.TrimSpace(p) != "" {
		return p, nil
	}
	return "", errors.New("mihomo binary not found — run `pnpm run prebuild` from repo root; run `wails dev` from apps/sloth-clash-desktop; or set SLOTH_MIHOMO_PATH / SLOTH_CLASH_DESKTOP_ROOT (absolute path to that app folder)")
}

func (a *App) extractBundledMihomoBinary() (string, error) {
	root, err := slothDataRoot()
	if err != nil {
		return "", err
	}
	sidecarDir := filepath.Join(root, "runtime", "_sidecar")
	if err := os.MkdirAll(sidecarDir, 0o755); err != nil {
		return "", err
	}

	patterns := []string{
		"build/sidecar/sloth-mihomo*",
		"build/sidecar/verge-mihomo*",
	}
	for _, preferNoAlpha := range []bool{true, false} {
		for _, pat := range patterns {
			matches, _ := fs.Glob(a.bundle, pat)
			sort.Strings(matches)
			for _, m := range matches {
				base := strings.ToLower(filepath.Base(m))
				if preferNoAlpha && strings.Contains(base, "alpha") {
					continue
				}
				info, statErr := fs.Stat(a.bundle, m)
				if statErr != nil || info.IsDir() {
					continue
				}
				dst := filepath.Join(sidecarDir, filepath.Base(m))
				if st, err := os.Stat(dst); err == nil && !st.IsDir() && st.Size() > 0 {
					return dst, nil
				}
				data, readErr := a.bundle.ReadFile(m)
				if readErr != nil || len(data) == 0 {
					continue
				}
				if writeErr := os.WriteFile(dst, data, 0o755); writeErr != nil {
					continue
				}
				if runtime.GOOS != "windows" {
					_ = os.Chmod(dst, 0o755)
				}
				return dst, nil
			}
		}
	}
	return "", errors.New("embedded mihomo not found in build/sidecar")
}

func (a *App) ensureGeoInDataDir(dataDir string) error {
	geoDir := filepath.Join(dataDir, "geo")
	if err := os.MkdirAll(geoDir, 0o755); err != nil {
		return err
	}
	names := []string{"geoip.dat", "geosite.dat", "Country.mmdb"}
	for _, n := range names {
		src := filepath.Join("build", "resources", n)
		data, err := a.bundle.ReadFile(src)
		if err != nil {
			continue
		}
		dst := filepath.Join(geoDir, n)
		if err := os.WriteFile(dst, data, 0o644); err != nil {
			return err
		}
	}
	return nil
}

func tunBlockForTraffic(traffic string) string {
	if strings.TrimSpace(traffic) != "tun" {
		return "tun:\n  enable: false\n"
	}
	// See https://wiki.metacubex.one/en/config/tun/
	switch runtime.GOOS {
	case "windows":
		return `tun:
  enable: true
  stack: system
  auto-route: true
  auto-redir: true
  auto-detect-interface: true
  strict-route: true
  dns-hijack:
    - any:53
    - tcp://any:53
`
	default:
		return `tun:
  enable: true
  stack: system
  auto-route: true
  auto-redir: true
  auto-detect-interface: true
  strict-route: true
  dns-hijack:
    - any:53
    - tcp://any:53
`
	}
}

func (a *App) writeRuntimeConfig(dataDir string, subURL string, extendTemplate string, proxyTemplate string, rulesTemplate string, ctrlPort, mixedPort int, secret string, traffic string, withExternalController bool) error {
	_ = os.MkdirAll(filepath.Join(dataDir, "providers"), 0o755)
	_ = os.MkdirAll(filepath.Join(dataDir, "ruleset"), 0o755)

	if ok, err := tryWriteMergedFullProfile(dataDir, subURL, extendTemplate, proxyTemplate, rulesTemplate, ctrlPort, mixedPort, secret, traffic, withExternalController); ok {
		return nil
	} else if err != nil {
		return err
	}

	geoDir := filepath.Join(dataDir, "geo")
	geoIP := filepath.Join(geoDir, "geoip.dat")
	geoSite := filepath.Join(geoDir, "geosite.dat")

	var cfg strings.Builder
	fmt.Fprintf(&cfg, "mixed-port: %d\n", mixedPort)
	fmt.Fprintf(&cfg, "socks-port: 0\n")
	fmt.Fprintf(&cfg, "port: 0\n")
	if withExternalController && ctrlPort > 0 {
		fmt.Fprintf(&cfg, "external-controller: 127.0.0.1:%d\n", ctrlPort)
	}
	fmt.Fprintf(&cfg, "secret: %q\n", secret)
	fmt.Fprintf(&cfg, "allow-lan: false\n")
	fmt.Fprintf(&cfg, "mode: rule\n")
	fmt.Fprintf(&cfg, "log-level: info\n")
	fmt.Fprintf(&cfg, "ipv6: true\n\n")

	if traffic == "tun" {
		// Without DNS, TUN + strict-route often yields “connected” apps but no working resolution / routing.
		cfg.WriteString(`dns:
  enable: true
  listen: 0.0.0.0:1053
  ipv6: true
  enhanced-mode: fake-ip
  fake-ip-range: 198.18.0.1/16
  use-hosts: true
  default-nameserver:
    - 1.1.1.1
    - 8.8.8.8
  nameserver:
    - https://1.1.1.1/dns-query
    - tls://8.8.8.8:853

`)
	}

	if _, err := os.Stat(geoIP); err == nil {
		fmt.Fprintf(&cfg, "geo-auto-update: false\n")
		fmt.Fprintf(&cfg, "geodata-mode: standard\n")
		fmt.Fprintf(&cfg, "geoip: %q\n", filepath.ToSlash(geoIP))
		if _, err2 := os.Stat(geoSite); err2 == nil {
			fmt.Fprintf(&cfg, "geosite: %q\n", filepath.ToSlash(geoSite))
		}
		fmt.Fprintf(&cfg, "\n")
	}

	fmt.Fprintf(&cfg, "proxy-providers:\n")
	fmt.Fprintf(&cfg, "  sub1:\n")
	fmt.Fprintf(&cfg, "    type: http\n")
	fmt.Fprintf(&cfg, "    url: %q\n", subURL)
	fmt.Fprintf(&cfg, "    interval: 3600\n")
	fmt.Fprintf(&cfg, "    path: ./providers/sub1.yaml\n")
	fmt.Fprintf(&cfg, "    health-check:\n")
	fmt.Fprintf(&cfg, "      enable: true\n")
	fmt.Fprintf(&cfg, "      url: http://www.gstatic.com/generate_204\n")
	fmt.Fprintf(&cfg, "      interval: 600\n\n")

	fmt.Fprintf(&cfg, "proxy-groups:\n")
	fmt.Fprintf(&cfg, "  - name: Auto\n")
	fmt.Fprintf(&cfg, "    type: url-test\n")
	fmt.Fprintf(&cfg, "    use:\n")
	fmt.Fprintf(&cfg, "      - sub1\n")
	fmt.Fprintf(&cfg, "    url: http://www.gstatic.com/generate_204\n")
	fmt.Fprintf(&cfg, "    interval: 300\n")
	fmt.Fprintf(&cfg, "    tolerance: 50\n")
	fmt.Fprintf(&cfg, "  - name: Manual\n")
	fmt.Fprintf(&cfg, "    type: select\n")
	fmt.Fprintf(&cfg, "    use:\n")
	fmt.Fprintf(&cfg, "      - sub1\n\n")

	fmt.Fprintf(&cfg, "rules:\n")
	fmt.Fprintf(&cfg, "  - MATCH,Auto\n\n")
	cfg.WriteString(tunBlockForTraffic(traffic))

	var m map[string]any
	if err := yaml.Unmarshal([]byte(cfg.String()), &m); err != nil {
		return err
	}
	if err := applyProfileMergeTemplate(m, extendTemplate); err != nil {
		return err
	}
	if err := applyProfileMergeTemplate(m, proxyTemplate); err != nil {
		return err
	}
	if err := applyProfileMergeTemplate(m, rulesTemplate); err != nil {
		return err
	}
	overlaySlothRuntimeOnMap(m, mixedPort, ctrlPort, secret, traffic, withExternalController)
	mergeBundledGeoIfMissing(m, dataDir)

	out, err := yaml.Marshal(m)
	if err != nil {
		return err
	}
	cfgPath := filepath.Join(dataDir, "config.yaml")
	return os.WriteFile(cfgPath, out, 0o644)
}

// writeRuntimeConfigIfNeeded uses a hand-edited config.yaml as base when SkipAutoConfig is set,
// but always reapplies Sloth runtime overlay (ports, secret, tun) for a working connect.
func writeRuntimeConfigIfNeeded(a *App, dataDir string, profile Profile, ctrlPort, mixedPort int, secret string, traffic string, withEC bool) error {
	if profile.SkipAutoConfig {
		cfgPath := filepath.Join(dataDir, "config.yaml")
		if st, err := os.Stat(cfgPath); err == nil && st.Size() > 0 {
			b, err := os.ReadFile(cfgPath)
			if err != nil {
				return err
			}
			var m map[string]any
			if err := yaml.Unmarshal(b, &m); err != nil {
				return err
			}
			overlaySlothRuntimeOnMap(m, mixedPort, ctrlPort, secret, traffic, withEC)
			out, err := yaml.Marshal(m)
			if err != nil {
				return err
			}
			return os.WriteFile(cfgPath, out, 0o644)
		}
	}
	return a.writeRuntimeConfig(
		dataDir,
		profile.URL,
		profile.MergeTemplate,
		profile.ProxyTemplate,
		profile.RulesTemplate,
		ctrlPort,
		mixedPort,
		secret,
		traffic,
		withEC,
	)
}

func coreDoWithEndpoint(ctx context.Context, listen, secret, method, path string, body io.Reader) (*http.Response, error) {
	if strings.TrimSpace(listen) == "" {
		return nil, errors.New("core not configured")
	}
	var u string
	if isWinPipeEndpoint(listen) {
		u = "http://mihomo" + path
	} else {
		h := strings.TrimSpace(listen)
		h = strings.TrimPrefix(h, "http://")
		u = strings.TrimRight("http://"+h, "/") + path
	}
	req, err := http.NewRequestWithContext(ctx, method, u, body)
	if err != nil {
		return nil, err
	}
	// mihomo external API on Windows named pipe is started with an empty controller secret (see MetaCubeX/mihomo hub/route/server.go startPipe).
	if secret != "" && !isWinPipeEndpoint(listen) {
		req.Header.Set("Authorization", "Bearer "+secret)
	}
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	rt := coreTransportForListen(listen)
	var client *http.Client
	if rt != nil {
		// Named-pipe controller: rely on request context for deadlines (GET /proxies can wait on providers).
		client = &http.Client{Transport: rt}
	} else {
		client = &http.Client{Timeout: 20 * time.Second}
	}
	return client.Do(req)
}

func (a *App) coreDo(ctx context.Context, method, path string, body io.Reader) (*http.Response, error) {
	a.mu.RLock()
	listen := a.effectiveCoreEndpointLocked()
	secret := a.coreSecret
	a.mu.RUnlock()
	return coreDoWithEndpoint(ctx, listen, secret, method, path, body)
}

func (a *App) coreGetJSON(ctx context.Context, path string, out any) error {
	resp, err := a.coreDo(ctx, http.MethodGet, path, nil)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	b, err := io.ReadAll(io.LimitReader(resp.Body, 4<<20))
	if err != nil {
		return err
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return fmt.Errorf("HTTP %d: %s", resp.StatusCode, strings.TrimSpace(string(b)))
	}
	return json.Unmarshal(b, out)
}

func (a *App) coreFetchVersion(ctx context.Context) (string, error) {
	var v struct {
		Version string `json:"version"`
	}
	if err := a.coreGetJSON(ctx, "/version", &v); err != nil {
		return "", err
	}
	if v.Version != "" {
		return v.Version, nil
	}
	return "unknown", nil
}

func (a *App) stopCoreLocked() {
	a.clearWindowsSystemProxyLocked()
	a.restoreTakenOverTunServicesLocked()
	a.coreStopIntentional = true
	if a.coreCancel != nil {
		a.coreCancel()
	}
	if a.coreOverPipe {
		sctx, cancel := context.WithTimeout(context.Background(), 25*time.Second)
		_ = ipcSlothStopCore(sctx)
		cancel()
	} else {
		cmd := a.coreCmd
		if cmd != nil && cmd.Process != nil {
			_ = cmd.Process.Kill()
		}
	}
	a.coreCmd = nil
	a.coreCancel = nil
	a.coreOverPipe = false
	a.coreSecret = ""
	a.coreListen = ""
	a.state.Core.Running = false
	a.state.Core.Version = ""
	a.state.Core.ControllerAddr = ""
	a.state.Core.MixedPort = 0
}

func fetchVersionAt(listen, secret string) (string, error) {
	if strings.TrimSpace(listen) == "" {
		return "", errors.New("no listen address")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 4*time.Second)
	defer cancel()
	resp, err := coreDoWithEndpoint(ctx, listen, secret, http.MethodGet, "/version", nil)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	b, err := io.ReadAll(io.LimitReader(resp.Body, 64*1024))
	if err != nil {
		return "", err
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return "", fmt.Errorf("HTTP %d: %s", resp.StatusCode, strings.TrimSpace(string(b)))
	}
	var v struct {
		Version string `json:"version"`
	}
	if err := json.Unmarshal(b, &v); err != nil {
		return "", err
	}
	if v.Version == "" {
		return "unknown", nil
	}
	return v.Version, nil
}

// startEmbeddedCore starts mihomo for the given profile. Must not be called with a.mu held.
func (a *App) startEmbeddedCore(profile Profile) error {
	if strings.TrimSpace(profile.URL) == "" {
		return errors.New("active profile has no subscription URL")
	}

	a.mu.Lock()
	a.stopCoreLocked()
	a.coreStopIntentional = false

	bin, err := a.resolveMihomoBinary()
	if err != nil {
		a.mu.Unlock()
		return err
	}
	root, err := slothDataRoot()
	if err != nil {
		a.mu.Unlock()
		return err
	}
	dataDir := filepath.Join(root, "runtime", profile.ID)
	if err := os.MkdirAll(filepath.Join(dataDir, "providers"), 0o755); err != nil {
		a.mu.Unlock()
		return err
	}
	if err := a.ensureGeoInDataDir(dataDir); err != nil {
		a.mu.Unlock()
		return err
	}

	parent := a.ctx
	if parent == nil {
		parent = context.Background()
	}

	mixedPort, err := pickFreePort()
	if err != nil {
		a.mu.Unlock()
		return err
	}
	secret := randomSecret()
	traffic := strings.TrimSpace(a.state.Traffic)
	if traffic != "tun" && traffic != "proxy" {
		traffic = "proxy"
	}

	useWindowsServiceCore := runtime.GOOS == "windows" && a.state.Service.Installed
	if useWindowsServiceCore {
		if err := writeRuntimeConfigIfNeeded(a, dataDir, profile, 0, mixedPort, secret, traffic, false); err != nil {
			a.mu.Unlock()
			return err
		}
		dataDirAbs, errAbs := filepath.Abs(dataDir)
		if errAbs != nil {
			a.mu.Unlock()
			return errAbs
		}
		cfgAbs := filepath.Join(dataDirAbs, "config.yaml")
		binAbs, errB := filepath.Abs(bin)
		if errB != nil {
			binAbs = bin
		}
		logDir := filepath.Join(dataDirAbs, "logs")
		if err := os.MkdirAll(logDir, 0o755); err != nil {
			a.mu.Unlock()
			return err
		}
		pipeName := slothMihomoPipeName(profile.ID)
		a.mu.Unlock()

		if err := windowsEnsureSlothIPCReachable(parent); err != nil {
			return err
		}
		// Best effort: stop any previous core instance before starting a new one.
		// Without this, switching/reconnecting in TUN mode can leave a stale TUN instance
		// and mihomo then reports "Cannot create a file when that file already exists."
		stopCtx, stopCancel := context.WithTimeout(parent, 8*time.Second)
		_ = ipcSlothStopCore(stopCtx)
		stopCancel()
		time.Sleep(320 * time.Millisecond)
		startCtx, startCancel := context.WithTimeout(parent, 55*time.Second)
		errStart := ipcSlothStartClash(startCtx, slothIPCStartParams{
			CorePath:     binAbs,
			ConfigPath:   cfgAbs,
			ConfigDir:    dataDirAbs,
			CoreIpcPath:  pipeName,
			LogDirectory: logDir,
		})
		startCancel()
		if errStart != nil {
			return errStart
		}

		a.mu.Lock()
		a.coreOverPipe = true
		a.coreCmd = nil
		a.coreCancel = nil
		a.coreSecret = secret
		a.coreListen = pipeName
		a.state.Core.ControllerAddr = pipeName
		a.state.Core.MixedPort = mixedPort
		a.state.Core.Running = true
		a.state.Core.LastError = ""
		listenCopy := pipeName
		secretCopy := secret
		a.mu.Unlock()

		deadline := time.Now().Add(45 * time.Second)
		for time.Now().Before(deadline) {
			v, verr := fetchVersionAt(listenCopy, secretCopy)
			if verr == nil && v != "" {
				a.mu.Lock()
				a.state.Core.Version = v
				a.state.Core.LastError = ""
				a.state.UpdatedAt = time.Now().Unix()
				a.mu.Unlock()
				return nil
			}
			time.Sleep(400 * time.Millisecond)
		}

		a.mu.Lock()
		a.stopCoreLocked()
		a.mu.Unlock()
		return errors.New("core did not become ready in time (check Sloth Windows service logs and SlothClash/runtime/<profile-id>/ under your config directory)")
	}

	ctrlPort, err := pickFreePort()
	if err != nil {
		a.mu.Unlock()
		return err
	}
	if err := writeRuntimeConfigIfNeeded(a, dataDir, profile, ctrlPort, mixedPort, secret, traffic, true); err != nil {
		a.mu.Unlock()
		return err
	}

	ctx, cancel := context.WithCancel(parent)
	cmd := exec.CommandContext(ctx, bin, "-d", dataDir)
	cmd.Dir = dataDir
	if attr := hideWindowSysProcAttr(); attr != nil {
		cmd.SysProcAttr = attr
	}
	logPath := filepath.Join(dataDir, "core.log")
	lf, err := os.OpenFile(logPath, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o644)
	if err != nil {
		cancel()
		a.mu.Unlock()
		return err
	}
	cmd.Stdout = lf
	cmd.Stderr = lf

	if err := cmd.Start(); err != nil {
		cancel()
		_ = lf.Close()
		a.mu.Unlock()
		return err
	}

	a.coreOverPipe = false
	a.coreCmd = cmd
	a.coreCancel = cancel
	a.coreSecret = secret
	a.coreListen = fmt.Sprintf("127.0.0.1:%d", ctrlPort)
	a.state.Core.ControllerAddr = a.coreListen
	a.state.Core.MixedPort = mixedPort
	a.state.Core.Running = true
	a.state.Core.LastError = ""

	listenCopy := a.coreListen
	secretCopy := a.coreSecret
	waitCmd := cmd
	a.mu.Unlock()

	go func() {
		waitErr := waitCmd.Wait()
		_ = lf.Close()
		a.mu.Lock()
		defer a.mu.Unlock()
		if a.coreStopIntentional {
			return
		}
		if waitErr != nil && !errors.Is(waitErr, context.Canceled) {
			a.state.Core.Running = false
			a.state.Connection.Status = "error"
			a.state.Connection.LastError = "core exited: " + waitErr.Error()
			a.state.Core.LastError = waitErr.Error()
		}
		a.state.UpdatedAt = time.Now().Unix()
	}()

	deadline := time.Now().Add(45 * time.Second)
	for time.Now().Before(deadline) {
		v, verr := fetchVersionAt(listenCopy, secretCopy)
		if verr == nil && v != "" {
			a.mu.Lock()
			a.state.Core.Version = v
			a.state.Core.LastError = ""
			a.state.UpdatedAt = time.Now().Unix()
			a.mu.Unlock()
			return nil
		}
		time.Sleep(400 * time.Millisecond)
	}

	a.mu.Lock()
	a.stopCoreLocked()
	a.mu.Unlock()
	return errors.New("core did not become ready in time (see SlothClash/runtime/<profile-id>/core.log under your OS config directory)")
}

func pullProxyGroupsFromCore(listen, secret string) ([]ProxyGroup, error) {
	if strings.TrimSpace(listen) == "" {
		return nil, errors.New("core not running")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
	defer cancel()

	var raw map[string]json.RawMessage
	resp, err := coreDoWithEndpoint(ctx, listen, secret, http.MethodGet, "/proxies", nil)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	b, err := io.ReadAll(io.LimitReader(resp.Body, 4<<20))
	if err != nil {
		return nil, err
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, fmt.Errorf("GET /proxies: HTTP %d %s", resp.StatusCode, strings.TrimSpace(string(b)))
	}
	if err := json.Unmarshal(b, &raw); err != nil {
		return nil, err
	}
	proxiesNode, ok := raw["proxies"]
	if !ok {
		return nil, errors.New("unexpected /proxies shape")
	}
	var proxies map[string]struct {
		Type string   `json:"type"`
		All  []string `json:"all"`
		Now  string   `json:"now"`
	}
	if err := json.Unmarshal(proxiesNode, &proxies); err != nil {
		return nil, err
	}

	var groups []ProxyGroup
	for name, p := range proxies {
		if name == "PASS" || name == "REJECT" || strings.EqualFold(name, "default") {
			continue
		}
		// mihomo may expose group types as URLTest / Selector / load-balance / etc.
		// Prefer capability-based detection (has choices) over strict type matching.
		if len(p.All) == 0 {
			continue
		}
		groups = append(groups, ProxyGroup{
			Name:     name,
			Type:     p.Type,
			Proxies:  append([]string(nil), p.All...),
			Selected: p.Now,
		})
	}
	sort.Slice(groups, func(i, j int) bool { return groups[i].Name < groups[j].Name })
	return groups, nil
}

// pullProxiesIntoState fetches /proxies and updates state. It must not be called while holding a.mu
// (it acquires a.mu briefly for reads/writes around the network call).
func (a *App) pullProxiesIntoState() error {
	var listen, secret string
	a.mu.Lock()
	listen = a.effectiveCoreEndpointLocked()
	if listen == "" {
		a.mu.Unlock()
		return errors.New("core not running")
	}
	secret = a.coreSecret
	a.mu.Unlock()

	groups, err := pullProxyGroupsFromCore(listen, secret)
	if err != nil {
		return err
	}
	a.mu.Lock()
	a.state.Proxy.Groups = groups
	a.mu.Unlock()
	return nil
}

func putProxySelectionAt(ctx context.Context, listen, secret, group, node string) error {
	if strings.TrimSpace(listen) == "" {
		return errors.New("core not running")
	}
	body := fmt.Sprintf(`{"name":%q}`, node)
	path := "/proxies/" + url.PathEscape(group)
	resp, err := coreDoWithEndpoint(ctx, listen, secret, http.MethodPut, path, strings.NewReader(body))
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	b, _ := io.ReadAll(io.LimitReader(resp.Body, 2048))
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return fmt.Errorf("PUT %s: HTTP %d %s", path, resp.StatusCode, strings.TrimSpace(string(b)))
	}
	return nil
}

func getCoreModeAt(ctx context.Context, listen, secret string) (string, error) {
	resp, err := coreDoWithEndpoint(ctx, listen, secret, http.MethodGet, "/configs", nil)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	b, _ := io.ReadAll(io.LimitReader(resp.Body, 32*1024))
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return "", fmt.Errorf("GET /configs: HTTP %d %s", resp.StatusCode, strings.TrimSpace(string(b)))
	}
	var out struct {
		Mode string `json:"mode"`
	}
	if err := json.Unmarshal(b, &out); err != nil {
		return "", err
	}
	return strings.TrimSpace(out.Mode), nil
}

func applyCoreModeHTTPWithGlobal(ctx context.Context, listen, secret, mode, activeGroup string) error {
	if strings.TrimSpace(listen) == "" {
		return nil
	}
	apiMode := "rule"
	switch mode {
	case "rule":
		apiMode = "rule"
	case "global":
		apiMode = "global"
	case "direct":
		apiMode = "direct"
	default:
		return errors.New("invalid mode")
	}
	body := fmt.Sprintf(`{"mode":%q}`, apiMode)
	pctx, pcancel := context.WithTimeout(ctx, 8*time.Second)
	defer pcancel()
	resp, err := coreDoWithEndpoint(pctx, listen, secret, http.MethodPatch, "/configs", strings.NewReader(body))
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	br, _ := io.ReadAll(io.LimitReader(resp.Body, 2048))
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return fmt.Errorf("PATCH /configs: HTTP %d %s", resp.StatusCode, strings.TrimSpace(string(br)))
	}
	vctx, vcancel := context.WithTimeout(ctx, 8*time.Second)
	defer vcancel()
	if got, err := getCoreModeAt(vctx, listen, secret); err != nil {
		return err
	} else if !strings.EqualFold(got, apiMode) {
		return fmt.Errorf("mode not applied: requested=%s got=%s", apiMode, got)
	}
	if mode != "global" {
		return nil
	}
	gctx, gcancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer gcancel()
	return syncGlobalOutboundHTTP(gctx, listen, secret, activeGroup)
}

// syncGlobalOutboundHTTP points GLOBAL at a real outbound (Auto/Manual/…) using explicit endpoints.
func syncGlobalOutboundHTTP(ctx context.Context, listen, secret, activeGroup string) error {
	target := strings.TrimSpace(activeGroup)
	candidates := []string{}
	seen := map[string]bool{}
	isUnsafeGlobal := func(name string) bool {
		u := strings.ToUpper(strings.TrimSpace(name))
		return u == "" || u == "GLOBAL" || u == "DIRECT" || u == "REJECT" || u == "REJECT-DROP" || u == "PASS"
	}
	addCandidate := func(name string) {
		name = strings.TrimSpace(name)
		if seen[name] || isUnsafeGlobal(name) {
			return
		}
		seen[name] = true
		candidates = append(candidates, name)
	}
	addCandidate(target)

	groups, err := pullProxyGroupsFromCore(listen, secret)
	if err == nil {
		// Prefer automatic groups first to avoid bad UX where first Global switch lands on DIRECT.
		for _, g := range groups {
			t := strings.ToLower(strings.ReplaceAll(strings.TrimSpace(g.Type), "-", ""))
			if t == "urltest" || t == "fallback" || t == "loadbalance" {
				addCandidate(g.Name)
			}
		}
		for _, g := range groups {
			t := strings.ToLower(strings.ReplaceAll(strings.TrimSpace(g.Type), "-", ""))
			if t == "selector" || t == "relay" {
				addCandidate(g.Name)
			}
		}
		for _, g := range groups {
			addCandidate(g.Name)
		}
	}

	for _, name := range candidates {
		if err := putProxySelectionAt(ctx, listen, secret, "GLOBAL", name); err == nil {
			return nil
		}
	}
	// Fallback: try first safe item from GLOBAL group's proxies list (can be a real node).
	if err == nil {
		for _, g := range groups {
			if !strings.EqualFold(strings.TrimSpace(g.Name), "GLOBAL") {
				continue
			}
			for _, p := range g.Proxies {
				if isUnsafeGlobal(p) {
					continue
				}
				if err := putProxySelectionAt(ctx, listen, secret, "GLOBAL", strings.TrimSpace(p)); err == nil {
					return nil
				}
			}
			break
		}
	}
	return fmt.Errorf("could not set GLOBAL outbound (target=%s, candidates=%d)", target, len(candidates))
}

// rulesOverviewFetch loads /rules and /providers/rules from a running mihomo controller.
func (a *App) rulesOverviewFetch(listen, secret string) RulesOverview {
	listen = strings.TrimSpace(listen)
	out := RulesOverview{}
	if listen == "" {
		out.LastError = "connect Sloth core first"
		return out
	}
	if isWinPipeEndpoint(listen) {
		out.Controller = listen
	} else {
		out.Controller = "http://" + listen
	}

	do := func(path string) ([]byte, int, error) {
		ctx, cancel := context.WithTimeout(context.Background(), 8*time.Second)
		defer cancel()
		resp, err := coreDoWithEndpoint(ctx, listen, secret, http.MethodGet, path, nil)
		if err != nil {
			return nil, 0, err
		}
		defer resp.Body.Close()
		b, rerr := io.ReadAll(io.LimitReader(resp.Body, 256*1024))
		return b, resp.StatusCode, rerr
	}

	b, code, err := do("/rules")
	if err != nil {
		out.LastError = err.Error()
		return out
	}
	if code < 200 || code >= 300 {
		out.LastError = fmt.Sprintf("GET /rules: HTTP %d %s", code, strings.TrimSpace(string(b)))
		return out
	}
	out.Reachable = true
	out.RulesBody = truncateString(string(b), 14000)

	b2, code2, err2 := do("/providers/rules")
	if err2 != nil || code2 < 200 || code2 >= 300 {
		return out
	}
	out.RuleProvidersBody = truncateString(string(b2), 10000)
	return out
}
