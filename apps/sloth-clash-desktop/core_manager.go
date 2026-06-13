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

// errConnectAborted is returned when connect context is cancelled or a newer connect/disconnect superseded this attempt.
var errConnectAborted = errors.New("connect aborted")

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

func isUnixSocketEndpoint(addr string) bool {
	s := strings.TrimSpace(addr)
	return strings.HasPrefix(s, "/")
}

// effectiveCoreEndpointLocked returns the mihomo API address (TCP host:port or \\.\pipe\...).
// Caller must hold a.mu (Lock or RLock). Prefer coreListen; fall back to published Core.ControllerAddr.
func (a *App) effectiveCoreEndpointLocked() string {
	if s := strings.TrimSpace(a.coreListen); s != "" {
		return s
	}
	return strings.TrimSpace(a.state.Core.ControllerAddr)
}

func slothMihomoIPCPath(profileID string) string {
	if runtime.GOOS != "windows" {
		var b strings.Builder
		b.WriteString("/tmp/slothclash/sloth-mihomo-")
		for _, r := range profileID {
			switch {
			case (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') || (r >= '0' && r <= '9') || r == '-' || r == '_':
				b.WriteRune(r)
			default:
				b.WriteByte('-')
			}
		}
		s := b.String()
		if s == "/tmp/slothclash/sloth-mihomo-" {
			return "/tmp/slothclash/sloth-mihomo-default.sock"
		}
		if !strings.HasSuffix(s, ".sock") {
			s += ".sock"
		}
		return s
	}
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
		// scripts/prebuild.mjs writes only sloth-mihomo* files into
		// build/sidecar/. Older verge-mihomo* binaries can still exist on
		// disk from pre-fork prebuild runs (we kept the clash-verge-rev
		// directory layout); they are intentionally NOT searched here so a
		// stale Apr-vintage verge-mihomo binary cannot win over today's
		// freshly-downloaded sloth-mihomo. Operators who deliberately want
		// to point at a different binary can set SLOTH_MIHOMO_PATH.
		patterns = append(patterns,
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
	p, extractErr := a.extractBundledMihomoBinary()
	if extractErr == nil && strings.TrimSpace(p) != "" {
		return p, nil
	}
	// Pass the real extract failure through to the UI instead of the
	// generic "run pnpm run prebuild" hint. On end-user installs the disk
	// search always misses (no sidecar/ next to the installed exe), so the
	// extract error IS the real diagnostic — most commonly antivirus /
	// Defender quarantining the written mihomo.exe, secondarily a Group
	// Policy that blocks write to %APPDATA%/SlothClash/runtime/_sidecar.
	hint := "Run `pnpm run prebuild` from repo root or set SLOTH_MIHOMO_PATH to an absolute path."
	if extractErr != nil {
		debugLog("startup", "H1", "core_manager.go:resolveMihomoBinary",
			"mihomo binary not found — extract from embed failed",
			map[string]any{"extractErr": extractErr.Error()})
		return "", fmt.Errorf("mihomo core unavailable: %w. If you are on an installed build this usually means antivirus quarantined the extracted file or `%%APPDATA%%/SlothClash/runtime/_sidecar` is not writable — check Windows Security › Protection history and add the SlothClash folder to exclusions. (%s)", extractErr, hint)
	}
	return "", fmt.Errorf("mihomo binary not found in embedded bundle or on disk. %s", hint)
}

func (a *App) extractBundledMihomoBinary() (string, error) {
	root, err := slothDataRoot()
	if err != nil {
		return "", fmt.Errorf("resolve data root: %w", err)
	}
	sidecarDir := filepath.Join(root, "runtime", "_sidecar")
	if err := os.MkdirAll(sidecarDir, 0o755); err != nil {
		return "", fmt.Errorf("create %s: %w", sidecarDir, err)
	}

	// Mirror the disk-search policy: only sloth-mihomo* is considered. Any
	// verge-mihomo* embedded from a stale build/sidecar checkout is ignored
	// so an upgrade installer cannot resurrect a months-old binary.
	patterns := []string{
		"build/sidecar/sloth-mihomo*",
	}
	// Collect every reason we rejected a candidate so the final error
	// message can tell the user/operator exactly what went wrong (read
	// failed? write failed? glob matched nothing? AV ate the file?).
	var rejections []string
	embedMatches := 0
	for _, preferNoAlpha := range []bool{true, false} {
		for _, pat := range patterns {
			matches, _ := fs.Glob(a.bundle, pat)
			sort.Strings(matches)
			embedMatches += len(matches)
			for _, m := range matches {
				base := strings.ToLower(filepath.Base(m))
				if preferNoAlpha && strings.Contains(base, "alpha") {
					continue
				}
				info, statErr := fs.Stat(a.bundle, m)
				if statErr != nil {
					rejections = append(rejections, fmt.Sprintf("%s: embed stat: %v", m, statErr))
					continue
				}
				if info.IsDir() {
					continue
				}
				// Each embedded mihomo version is materialised to its own
				// file under _sidecar/, suffixed with its size, so an
				// upgrade does NOT have to overwrite (and therefore
				// unlock) the previous version's binary. Background: on
				// Windows a running .exe holds an exclusive lock on its
				// own image, so when v0.4.0's mihomo was still alive
				// (the NSIS installer does not kill running children of
				// sloth-clash-service), v0.5.0's cache invalidation tried
				// to Remove + WriteFile the same path and silently failed
				// — surfacing as the generic "mihomo binary not found"
				// error. Versioned filenames sidestep the lock entirely:
				// the new file is a different name, the old one stays
				// locked by the previous mihomo until that process exits
				// and is cleaned up below on the NEXT successful extract.
				rawBase := filepath.Base(m)
				ext := filepath.Ext(rawBase)
				stem := strings.TrimSuffix(rawBase, ext)
				versioned := filepath.Join(sidecarDir, fmt.Sprintf("%s-%d%s", stem, info.Size(), ext))
				if st, err := os.Stat(versioned); err == nil && !st.IsDir() && st.Size() == info.Size() {
					// Already extracted by an earlier run — reuse.
					return versioned, nil
				}
				data, readErr := a.bundle.ReadFile(m)
				if readErr != nil {
					rejections = append(rejections, fmt.Sprintf("%s: embed read: %v", m, readErr))
					continue
				}
				if len(data) == 0 {
					rejections = append(rejections, fmt.Sprintf("%s: embed read returned 0 bytes (build/sidecar was empty at compile time?)", m))
					continue
				}
				if writeErr := os.WriteFile(versioned, data, 0o755); writeErr != nil {
					rejections = append(rejections, fmt.Sprintf("%s: write to %s: %v", m, versioned, writeErr))
					continue
				}
				// Post-write verification: antivirus / Defender may delete
				// the file BETWEEN our successful write and our return,
				// silently. Stat'ing again catches that window and surfaces
				// the cause instead of returning a path that races a
				// quarantine event.
				if st, statErr := os.Stat(versioned); statErr != nil || st.Size() != info.Size() {
					reason := "missing after write (likely antivirus quarantine — check Windows Security › Protection history and exclude SlothClash)"
					if statErr != nil {
						reason = statErr.Error()
					} else if st.Size() != info.Size() {
						reason = fmt.Sprintf("size shrunk after write (expected %d got %d)", info.Size(), st.Size())
					}
					rejections = append(rejections, fmt.Sprintf("%s: post-write check: %s", m, reason))
					continue
				}
				if runtime.GOOS != "windows" {
					_ = os.Chmod(versioned, 0o755)
				}
				// Best-effort cleanup of older versioned files and the
				// legacy unversioned path. On Windows Remove silently
				// fails when the previous mihomo still holds the lock —
				// that is fine, we will retry on the next launch after
				// the old process has exited.
				go cleanupStaleSidecarBinaries(sidecarDir, versioned, stem, ext)
				return versioned, nil
			}
		}
	}
	if embedMatches == 0 {
		return "", fmt.Errorf("embed.FS has no sloth-mihomo* entries under build/sidecar/ — installer was built from an empty sidecar directory; rebuild after `pnpm run prebuild --force`")
	}
	if len(rejections) == 0 {
		return "", errors.New("embedded mihomo not found in build/sidecar (no matches passed the alpha filter)")
	}
	return "", fmt.Errorf("could not write embedded mihomo to %s: %s", sidecarDir, strings.Join(rejections, "; "))
}

// cleanupStaleSidecarBinaries removes older extracted mihomo binaries from
// _sidecar/ once a freshly versioned one is in use. Two targets:
//
//  1. The legacy unversioned name (e.g. sloth-mihomo-x86_64-pc-windows-msvc.exe)
//     left over from app builds before the versioned-filename scheme.
//  2. Other versioned files for the same stem (e.g. sloth-mihomo-...-46177792.exe
//     when we just extracted ...-47221234.exe) — stale upgrade leftovers.
//
// All removals are best-effort: on Windows a still-running mihomo from a
// previous app session holds an exclusive lock on its image and Remove
// returns ERROR_SHARING_VIOLATION. That is fine — the file will be cleaned
// up on a subsequent app launch after the old mihomo has actually exited.
// Never log or surface failures; this is housekeeping, not a critical path.
func cleanupStaleSidecarBinaries(sidecarDir, keepPath, stem, ext string) {
	defer func() { _ = recover() }()
	legacy := filepath.Join(sidecarDir, stem+ext)
	if legacy != keepPath {
		_ = os.Remove(legacy)
	}
	pattern := filepath.Join(sidecarDir, stem+"-*"+ext)
	matches, _ := filepath.Glob(pattern)
	for _, m := range matches {
		if m == keepPath {
			continue
		}
		_ = os.Remove(m)
	}
}

// ensureGeoInDataDir copies the bundled geo data files into the running
// profile's working directory, mirroring clash-verge-rev's prepare_service_path
// pattern (src-tauri/src/utils/init.rs):
//
//   - Files land at the root of <dataDir>, not in a `geo/` subdirectory.
//     Mihomo's default lookup is "<workdir>/geoip.dat", "<workdir>/geosite.dat",
//     "<workdir>/Country.mmdb" — putting them at the root means we never have
//     to touch the user's YAML to point at custom paths.
//   - Each file is only overwritten when the destination is missing or
//     strictly older than the source. This preserves manual replacements
//     (the user dropping a fresher .dat in the runtime directory) across
//     normal app launches — only an explicit "Re-extract bundled geo" call
//     with force=true overwrites unconditionally.
//
// Files we don't ship in the bundle (e.g. when the build dropped Country.mmdb)
// are skipped silently rather than producing a hard error — the pipeline can
// continue with whatever subset is available.
func (a *App) ensureGeoInDataDir(dataDir string) error {
	return a.copyBundledGeoFiles(dataDir, false)
}

// reExtractGeoInDataDir is the "force" variant used by the Advanced
// "Re-extract bundled geo" power tool. Overwrites whatever is currently on
// disk with the files baked into the app bundle.
func (a *App) reExtractGeoInDataDir(dataDir string) error {
	return a.copyBundledGeoFiles(dataDir, true)
}

func (a *App) copyBundledGeoFiles(dataDir string, force bool) error {
	if err := os.MkdirAll(dataDir, 0o755); err != nil {
		return err
	}
	names := []string{"geoip.dat", "geosite.dat", "Country.mmdb"}
	for _, n := range names {
		// IMPORTANT: embed.FS always uses forward-slash paths, regardless of
		// the host OS. filepath.Join on Windows builds "build\resources\..."
		// which silently returns "file does not exist" from a.bundle.ReadFile
		// and we skip the file. This was the root cause of "Can't find
		// GeoIP.dat, start download" on Windows: extraction was a no-op for
		// the entire app lifetime, so mihomo always fell through to its
		// online fallback (which then DNS-fails behind restrictive networks).
		src := "build/resources/" + n
		data, err := a.bundle.ReadFile(src)
		if err != nil {
			debugLog(
				"geo.extract",
				"H4",
				"core_manager.go:copyBundledGeoFiles",
				"embed read failed (file may not be in bundle)",
				map[string]any{"name": n, "src": src, "error": err.Error()},
			)
			continue
		}
		dstPath := filepath.Join(dataDir, n)
		if !force {
			// Match Verge's "copy only if missing or source is newer"
			// semantic. Embedded files share the binary's mtime, so we
			// approximate "newer" by comparing byte counts: a fresh
			// meta-rules-dat build almost never matches the previous
			// bundle exactly, so a size mismatch is good enough to trigger
			// a refresh without baking in a sentinel version file.
			missing, srcNewer := destIsStale(dstPath, len(data))
			if !missing && !srcNewer {
				continue
			}
		}
		if err := atomicWriteFile(dstPath, data, 0o644); err != nil {
			return fmt.Errorf("write %s: %w", dstPath, err)
		}
	}
	return nil
}

func destIsStale(dst string, srcSize int) (missing bool, srcNewer bool) {
	info, err := os.Stat(dst)
	if err != nil {
		return true, false
	}
	if int(info.Size()) != srcSize {
		return false, true
	}
	return false, false
}

// tunBlockForTraffic returns the TUN configuration embedded in generated YAML.
// The enable argument is inlined verbatim into tun.enable so the YAML always
// reflects the caller's current intent. Defaults mirror clash-verge-rev's
// IClashTemp::template() + constants::tun (DEFAULT_STACK=gvisor, DNS_HIJACK=["any:53"],
// strict-route=false, auto-route=true, auto-detect-interface=true). gvisor is the
// userspace stack Mihomo defaults to and behaves consistently across Windows/macOS/Linux;
// it avoids the kernel ring-buffer stalls that `stack: system` on wintun shows under
// sustained UDP load (e.g. gaming). These are defaults only — if the subscription or
// user TUN settings provide a value the merger will honour it verbatim.
func tunBlockForTraffic(enable bool) string {
	enableStr := "false"
	if enable {
		enableStr = "true"
	}
	return `tun:
  enable: ` + enableStr + `
  stack: gvisor
  auto-route: true
  auto-detect-interface: true
  strict-route: false
  dns-hijack:
    - any:53
`
}

func (a *App) writeRuntimeConfig(dataDir string, subURL string, extendTemplate string, proxyTemplate string, rulesTemplate string, ctrlPort, mixedPort int, secret string, traffic string, withExternalController bool, enableTun bool) error {
	_ = os.MkdirAll(filepath.Join(dataDir, "providers"), 0o755)
	_ = os.MkdirAll(filepath.Join(dataDir, "ruleset"), 0o755)

	outcome, err := tryWriteMergedFullProfile(dataDir, subURL, extendTemplate, proxyTemplate, rulesTemplate, ctrlPort, mixedPort, secret, traffic, withExternalController, enableTun)
	if outcome == pipelineOK {
		return nil
	}
	if err != nil {
		return err
	}
	// outcome is one of the "soft" cases (cache_miss_no_net / not_full_profile);
	// fall through to write the bare provider profile below.
	_ = outcome.useBareFallback() // anchor for readers — see pipelineOutcome.

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

	// profile.store-selected mirrors clash-verge-rev's `use_clash` defaults:
	// preserves per-group node selections across hot reloads (we reload on
	// every Connect/Disconnect/SetTrafficMode). store-fake-ip keeps the
	// fake-IP map across reloads while TUN is on, so apps with cached
	// fake-IPs do not have to renegotiate after reconnect.
	fmt.Fprintf(&cfg, "profile:\n")
	fmt.Fprintf(&cfg, "  store-selected: true\n")
	fmt.Fprintf(&cfg, "  store-fake-ip: %t\n\n", enableTun)

	if enableTun {
		// Match enhance::tun::use_tun: fake-ip DNS block goes in only when TUN
		// is actually being brought up. Without it, TUN + strict-route gives
		// "connected" apps but no working resolution / routing.
		cfg.WriteString(`dns:
  enable: true
  listen: ":1053"
  ipv6: true
  respect-rules: true
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
		// geodata-mode: true selects .dat geoip database; the previous value
		// "standard" was not a valid mode (it's a value for the unrelated
		// `geodata-loader` key) — mihomo silently fell back to mmdb and our
		// bundled .dat files were never loaded.
		fmt.Fprintf(&cfg, "geodata-mode: true\n")
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

	// Bare fallback groups: emit a `select` group (Manual) that lists Auto
	// first followed by every proxy provider entry. MATCH routes through
	// Manual so the default selection stays "pick the fastest" (Manual →
	// Auto → url-test), but users can click an individual node in the
	// Proxies screen without hitting Mihomo's 400 "cannot change url-test
	// group selection" error. url-test groups refuse PUT /proxies/{group}
	// selection, which is why pointing MATCH directly at Auto made manual
	// node picking look broken in Rule mode.
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
	fmt.Fprintf(&cfg, "    proxies:\n")
	fmt.Fprintf(&cfg, "      - Auto\n")
	fmt.Fprintf(&cfg, "    use:\n")
	fmt.Fprintf(&cfg, "      - sub1\n\n")

	fmt.Fprintf(&cfg, "rules:\n")
	fmt.Fprintf(&cfg, "  - MATCH,Manual\n\n")
	cfg.WriteString(tunBlockForTraffic(enableTun))

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
	if err := finalizeRuntimeConfigPipeline(
		m,
		dataDir,
		mixedPort,
		ctrlPort,
		secret,
		traffic,
		withExternalController,
		enableTun,
	); err != nil {
		return err
	}

	out, err := marshalRuntimeYAML(m)
	if err != nil {
		return err
	}
	cfgPath := filepath.Join(dataDir, "config.yaml")
	return atomicWriteFile(cfgPath, out, 0o644)
}

// writeRuntimeConfigIfNeeded uses a hand-edited config.yaml as base when SkipAutoConfig is set,
// but always reapplies Sloth runtime overlay (ports, secret, tun) for a working connect.
// enableTun controls the single source of truth for tun.enable that ends up in the
// written YAML — match the user's current intent (connected && traffic=="tun") so
// that PUT /configs?force=true on subsequent reloads does not thrash the adapter.
func writeRuntimeConfigIfNeeded(a *App, binPath string, dataDir string, profile Profile, ctrlPort, mixedPort int, secret string, traffic string, withEC bool, enableTun bool) error {
	// Geo files must exist before the pipeline runs — overlayBundledGeoData
	// only points YAML at <dataDir>/geo/*.dat if those files are physically
	// present. Without this, an applyRuntimeConfig call that bypasses
	// startEmbeddedCore (warm-reload, prefs-driven trigger) writes a YAML
	// with the old geox-url still in place and preflight tries to download
	// geoip-lite.dat → DNS fail → Connect dies.
	if err := a.ensureGeoInDataDir(dataDir); err != nil {
		// Non-fatal: we still try the pipeline. overlayBundledGeoData will
		// safely no-op if the files are missing and mihomo's own fallback
		// kicks in (which may also fail, but at least we have not lied).
		_ = err
	}
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
			if err := finalizeRuntimeConfigPipeline(
				m,
				dataDir,
				mixedPort,
				ctrlPort,
				secret,
				traffic,
				withEC,
				enableTun,
			); err != nil {
				return err
			}
			out, err := marshalRuntimeYAML(m)
			if err != nil {
				return err
			}
			if err := atomicWriteFile(cfgPath, out, 0o644); err != nil {
				return err
			}
			return runConfigPreflight(binPath, dataDir)
		}
	}
	if err := a.writeRuntimeConfig(
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
		enableTun,
	); err != nil {
		return err
	}
	return runConfigPreflight(binPath, dataDir)
}

func runConfigPreflight(binPath, dataDir string) error {
	binPath = strings.TrimSpace(binPath)
	if binPath == "" {
		return nil
	}
	cfgPath := filepath.Join(dataDir, "config.yaml")
	if st, err := os.Stat(cfgPath); err != nil || st.Size() == 0 {
		return nil
	}
	// Last-mile hardening: normalize DNS invariants in the written file before preflight.
	// This guards against edge cases from user-edited YAML / merge templates.
	if err := repairRuntimeConfigDNS(cfgPath); err != nil {
		return fmt.Errorf("configuration preflight failed: cannot normalize dns invariants: %w", err)
	}

	// Clash/Mihomo supports `-t` test mode; use it before actual launch so users see
	// parse errors immediately instead of waiting for a failed start.
	argVariants := [][]string{
		{"-d", dataDir, "-t"},
		{"-t", "-d", dataDir},
	}
	sawUnsupportedFlag := false
	for _, args := range argVariants {
		ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
		cmd := exec.CommandContext(ctx, binPath, args...)
		cmd.Dir = dataDir
		if attr := hideWindowSysProcAttr(); attr != nil {
			cmd.SysProcAttr = attr
		}
		out, err := cmd.CombinedOutput()
		cancel()
		text := strings.TrimSpace(string(out))
		if err == nil {
			return nil
		}
		lower := strings.ToLower(text + "\n" + err.Error())
		if strings.Contains(lower, "unknown flag") ||
			strings.Contains(lower, "flag provided but not defined") ||
			strings.Contains(lower, "unknown shorthand flag") {
			sawUnsupportedFlag = true
			continue
		}
		if idx := strings.Index(strings.ToLower(text), "parse config error:"); idx >= 0 {
			msg := strings.TrimSpace(text[idx:])
			return fmt.Errorf("configuration preflight failed: %s", msg)
		}
		if text == "" {
			text = err.Error()
		}
		return fmt.Errorf("configuration preflight failed: %s", strings.TrimSpace(text))
	}
	if sawUnsupportedFlag {
		// Older binaries may not support test mode; run a short startup probe to still
		// catch fatal parse errors before regular launch.
		return runConfigStartupProbe(binPath, dataDir)
	}
	return nil
}

func runConfigStartupProbe(binPath, dataDir string) error {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, binPath, "-d", dataDir)
	cmd.Dir = dataDir
	if attr := hideWindowSysProcAttr(); attr != nil {
		cmd.SysProcAttr = attr
	}
	out, err := cmd.CombinedOutput()
	text := strings.TrimSpace(string(out))
	lower := strings.ToLower(text)
	if idx := strings.Index(lower, "parse config error:"); idx >= 0 {
		return fmt.Errorf("configuration preflight failed: %s", strings.TrimSpace(text[idx:]))
	}
	if err != nil {
		// deadline exceeded without parse error usually means the core started; accept.
		if errors.Is(ctx.Err(), context.DeadlineExceeded) {
			return nil
		}
		if text == "" {
			text = err.Error()
		}
		if strings.Contains(strings.ToLower(text), "parse config error") {
			return fmt.Errorf("configuration preflight failed: %s", text)
		}
	}
	return nil
}

func repairRuntimeConfigDNS(cfgPath string) error {
	b, err := os.ReadFile(cfgPath)
	if err != nil {
		return err
	}
	var m map[string]any
	if err := yaml.Unmarshal(b, &m); err != nil {
		return err
	}
	if len(m) == 0 {
		return nil
	}
	ensureDefaultDNSForTun(m)
	out, err := marshalRuntimeYAML(m)
	if err != nil {
		return err
	}
	return atomicWriteFile(cfgPath, out, 0o644)
}

func coreDoWithEndpoint(ctx context.Context, listen, secret, method, path string, body io.Reader) (*http.Response, error) {
	if strings.TrimSpace(listen) == "" {
		return nil, errors.New("core not configured")
	}
	var u string
	if isWinPipeEndpoint(listen) || isUnixSocketEndpoint(listen) {
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
	// mihomo external API on local socket/pipe endpoints starts without bearer auth.
	if secret != "" && !isWinPipeEndpoint(listen) && !isUnixSocketEndpoint(listen) {
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
	a.clearSystemProxyLocked()
	a.restoreTakenOverTunServicesLocked()
	a.coreStopIntentional = true
	a.coreProcToken++
	a.state.Core.Lifecycle = "stopping"
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
	a.coreActiveProfileID = ""
	a.state.Core.Running = false
	a.state.Core.Lifecycle = "stopped"
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

// waitForCoreEndpointStop waits until /version at the given endpoint stops responding.
// This is important on fast disconnect->connect cycles where service stop is asynchronous.
func waitForCoreEndpointStop(parent context.Context, runID, listen, secret string, timeout time.Duration) error {
	if strings.TrimSpace(listen) == "" {
		return nil
	}
	if timeout <= 0 {
		timeout = 3 * time.Second
	}
	deadline := time.Now().Add(timeout)
	sawAlive := false
	for time.Now().Before(deadline) {
		select {
		case <-parent.Done():
			return parent.Err()
		default:
		}
		_, err := fetchVersionAt(listen, secret)
		if err != nil {
			// #region agent log
			debugLog(
				runID,
				"H2",
				"core_manager.go:760",
				"waitForCoreEndpointStop observed endpoint stop",
				map[string]any{
					"endpoint":  listen,
					"sawAlive":  sawAlive,
					"stopError": err.Error(),
				},
			)
			// #endregion
			return nil
		}
		sawAlive = true
		time.Sleep(90 * time.Millisecond)
	}
	// #region agent log
	debugLog(
		runID,
		"H2",
		"core_manager.go:767",
		"waitForCoreEndpointStop timeout",
		map[string]any{
			"endpoint": listen,
			"timeout":  timeout.String(),
		},
	)
	// #endregion
	return fmt.Errorf("old core still responding at %s after %s", listen, timeout.String())
}

// ensureCoreForProfile guarantees that a Mihomo core is running for the given
// profile. In the reload model (v0.3+) this is the primary way the core is
// brought up: the core lives for the lifetime of the active profile and
// Connect/Disconnect/SetTrafficMode regenerate the runtime YAML with the
// desired tun.enable state and push it via PUT /configs?force=true without
// touching the process (mirrors clash-verge-rev's CoreManager::update_config).
//
//   - If a core is already running for the same profile, returns nil without any
//     restart.
//   - If a core is running for a different profile, stops it cleanly and starts
//     a fresh one.
//   - If no core is running, starts one.
//
// Must not be called with a.mu held. gen is an optional supersede token; pass
// 0 when the caller is not part of the connect-job generation machinery (e.g.
// startup background boot of the active profile).
// ensureCoreForProfile guarantees a running Mihomo bound to the given profile.
// If the core is already up for this profile it returns without touching the
// running process — callers that need to push fresh YAML in that case must go
// through applyRuntimeConfig (PUT /configs?force=true). If the core is down
// or bound to a different profile, a new one is spawned with YAML reflecting
// enableTun so the core comes up already in the desired state.
func (a *App) ensureCoreForProfile(profile Profile, gen uint64, enableTun bool) error {
	_, err := a.ensureCoreForProfileEx(profile, gen, enableTun)
	return err
}

// ensureCoreForProfileEx is the cold/reuse-aware variant: the second return
// value reports whether mihomo was just started fresh in this call. Callers
// that follow up with a hot-reload (PUT /configs?force=true) can skip it when
// coldStarted is true — mihomo already loaded the same YAML on startup, and
// a second init would just stall behind the rule-provider download phase.
func (a *App) ensureCoreForProfileEx(profile Profile, gen uint64, enableTun bool) (coldStarted bool, err error) {
	a.coreLifecycleMu.Lock()
	defer a.coreLifecycleMu.Unlock()

	a.mu.RLock()
	running := a.state.Core.Running && strings.TrimSpace(a.coreListen) != ""
	activeID := strings.TrimSpace(a.coreActiveProfileID)
	a.mu.RUnlock()

	if running && activeID == strings.TrimSpace(profile.ID) {
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		defer cancel()
		if _, err := a.coreFetchVersion(ctx); err == nil {
			return false, nil
		}
	}

	// startEmbeddedCore uses connectGen for supersede/abort semantics; gen==0
	// means "I'm not part of a connect-job generation" (e.g. background boot
	// on startup). Claim a fresh gen so a concurrent Connect can cleanly
	// supersede us instead of racing on a zero sentinel.
	if gen == 0 {
		gen = a.connectGen.Add(1)
	}
	if err := a.startEmbeddedCore(profile, gen, enableTun); err != nil {
		return false, err
	}
	return true, nil
}

// forceRestartCoreForProfile always restarts the core for the provided profile,
// even when the current running core already belongs to the same profile.
// Used as a controlled fallback after runtime reload failures.
func (a *App) forceRestartCoreForProfile(profile Profile, gen uint64, enableTun bool) error {
	a.coreLifecycleMu.Lock()
	defer a.coreLifecycleMu.Unlock()
	if gen == 0 {
		gen = a.connectGen.Add(1)
	}
	return a.startEmbeddedCore(profile, gen, enableTun)
}

// startEmbeddedCore starts mihomo for the given profile. Must not be called with a.mu held.
// gen must match a.connectGen for the in-flight Connect job so we can exit early on supersede or app shutdown.
// enableTun goes straight into the generated YAML (tun.enable). Callers compute
// it from the effective user intent at call time (traffic=="tun" && user wants
// to be connected) so the core boots in the final state without a follow-up
// PATCH flip — exactly how clash-verge-rev's start_core works.
func (a *App) startEmbeddedCore(profile Profile, gen uint64, enableTun bool) error {
	if strings.TrimSpace(profile.URL) == "" && !profileHasLocalConfig(profile) {
		return errors.New("active profile has no subscription URL")
	}

	var traffic string
	var serviceInstalled bool
	var prevListen string
	var prevSecret string
	a.mu.Lock()
	prevListen = a.coreListen
	prevSecret = a.coreSecret
	a.stopCoreLocked()
	a.coreStopIntentional = false
	traffic = strings.TrimSpace(a.state.Traffic)
	if traffic != "tun" && traffic != "proxy" {
		traffic = "proxy"
	}
	serviceInstalled = a.state.Service.Installed
	// pre-clear previous non-fatal banner on new connect attempt
	a.state.Connection.LastWarning = ""
	a.state.Core.Lifecycle = "starting"
	a.mu.Unlock()

	bin, err := a.resolveMihomoBinary()
	if err != nil {
		return err
	}
	root, err := slothDataRoot()
	if err != nil {
		return err
	}
	dataDir := filepath.Join(root, "runtime", profile.ID)
	if err := os.MkdirAll(filepath.Join(dataDir, "providers"), 0o755); err != nil {
		return err
	}
	if err := a.ensureGeoInDataDir(dataDir); err != nil {
		return err
	}

	parent := a.ctx
	if parent == nil {
		parent = context.Background()
	}

	mixedPort, err := pickFreePort()
	if err != nil {
		return err
	}
	secret := randomSecret()
	runID := "gen-" + strconv.FormatUint(gen, 10)

	useServiceCore := runtime.GOOS == "windows" && serviceInstalled
	if runtime.GOOS == "darwin" && serviceInstalled {
		if err := windowsEnsureSlothIPCReachable(parent); err != nil {
			if traffic == "tun" {
				return fmt.Errorf("darwin service IPC unavailable for TUN mode: %w", err)
			}
			// Safe fallback for proxy mode: keep app usable even if privileged helper is temporarily unreachable.
			a.mu.Lock()
			a.state.Connection.LastWarning = "Sloth service IPC unreachable, falling back to user-process core for Proxy mode: " + err.Error()
			a.mu.Unlock()
			a.appendRuntimeDiag("ipc.error", "darwin service IPC unreachable, using embedded core")
			useServiceCore = false
		} else {
			useServiceCore = true
		}
	}
	if useServiceCore {
		// External-controller TCP port for the privileged-service-managed core:
		// mihomo listens on BOTH the service-supplied pipe (CLI flag) AND this
		// TCP socket (YAML `external-controller`), and our HTTP client talks to
		// the TCP socket. The named-pipe HTTP transport on Windows has been
		// observed to stall 10-20 s on PUT /configs?force=true even after the
		// reload has completed on the core side — using TCP matches the
		// clash-verge-rev architecture and eliminates the long stalls. The pipe
		// is retained purely so the privileged service can keep its existing
		// start/stop IPC semantics (see stopCoreLocked → ipcSlothStopCore).
		ctrlPort, perr := pickFreePort()
		if perr != nil {
			return perr
		}
		// #region agent log
		debugLog(
			runID,
			"H3",
			"core_manager.go:850",
			"service-core path selected",
			map[string]any{
				"profileId": profile.ID,
				"traffic":   traffic,
				"pipe":      slothMihomoIPCPath(profile.ID),
				"ctrlPort":  ctrlPort,
			},
		)
		// #endregion
		if err := writeRuntimeConfigIfNeeded(a, bin, dataDir, profile, ctrlPort, mixedPort, secret, traffic, true, enableTun); err != nil {
			return err
		}
		dataDirAbs, errAbs := filepath.Abs(dataDir)
		if errAbs != nil {
			return errAbs
		}
		cfgAbs := filepath.Join(dataDirAbs, "config.yaml")
		binAbs, errB := filepath.Abs(bin)
		if errB != nil {
			binAbs = bin
		}
		logDir := filepath.Join(dataDirAbs, "logs")
		if err := os.MkdirAll(logDir, 0o755); err != nil {
			return err
		}
		pipeName := slothMihomoIPCPath(profile.ID)
		if isUnixSocketEndpoint(pipeName) {
			_ = os.Remove(pipeName)
		}

		if err := windowsEnsureSlothIPCReachable(parent); err != nil {
			return err
		}
		if strings.TrimSpace(prevListen) != "" {
			waitErr := waitForCoreEndpointStop(parent, runID, prevListen, prevSecret, 8*time.Second)
			// #region agent log
			debugLog(
				runID,
				"H2",
				"core_manager.go:903",
				"restart stop barrier completed",
				map[string]any{
					"prevListen": prevListen,
					"waitErr":    errorString(waitErr),
				},
			)
			// #endregion
			if waitErr != nil {
				return fmt.Errorf("previous core did not stop cleanly before restart: %w", waitErr)
			}
		}

		startCtx, startCancel := context.WithTimeout(parent, 55*time.Second)
		errStart := ipcSlothStartClash(startCtx, slothIPCStartParams{
			CorePath:     binAbs,
			ConfigPath:   cfgAbs,
			ConfigDir:    dataDirAbs,
			CoreIpcPath:  pipeName,
			LogDirectory: logDir,
		})
		startCancel()
		// #region agent log
		// Log the exact logDir we asked the privileged service to use so the
		// next "service logs missing" report is diagnosable: if this path
		// doesn't match what the user sees on disk, it points to a
		// service-side writing issue (permissions / different account) vs
		// a client-side bug.
		debugLog(
			runID,
			"H4",
			"core_manager.go:934",
			"ipc start finished",
			map[string]any{
				"pipe":     pipeName,
				"startErr": errorString(errStart),
				"logDir":   logDir,
			},
		)
		// #endregion
		if errStart != nil {
			return errStart
		}

		tcpListen := fmt.Sprintf("127.0.0.1:%d", ctrlPort)
		a.mu.Lock()
		prevMixed := a.state.Core.MixedPort
		// coreOverPipe stays true: it controls the *stop* path
		// (stopCoreLocked → ipcSlothStopCore vs cmd.Kill). The HTTP API
		// transport is decided by coreListen, which now points at TCP.
		a.coreOverPipe = true
		a.coreCmd = nil
		a.coreCancel = nil
		a.coreSecret = secret
		a.coreListen = tcpListen
		a.state.Core.ControllerAddr = tcpListen
		a.state.Core.MixedPort = mixedPort
		a.state.Core.Running = true
		a.state.Core.Lifecycle = "running"
		a.state.Core.LastError = ""
		listenCopy := tcpListen
		secretCopy := secret
		a.mu.Unlock()
		if runtime.GOOS == "windows" {
			go a.handleMixedPortChangeForWindowsSysProxy(prevMixed, mixedPort)
		}

		deadline := time.Now().Add(45 * time.Second)
		for time.Now().Before(deadline) {
			select {
			case <-parent.Done():
				a.mu.Lock()
				a.stopCoreLocked()
				a.mu.Unlock()
				return errConnectAborted
			default:
			}
			if a.connectGen.Load() != gen {
				a.mu.Lock()
				a.stopCoreLocked()
				a.mu.Unlock()
				return errConnectAborted
			}
			v, verr := fetchVersionAt(listenCopy, secretCopy)
			if verr == nil && v != "" {
				a.mu.Lock()
				a.state.Core.Version = v
				a.state.Core.LastError = ""
				a.state.Core.Lifecycle = "running"
				a.coreActiveProfileID = profile.ID
				a.state.UpdatedAt = time.Now().Unix()
				a.mu.Unlock()
				return nil
			}
			select {
			case <-parent.Done():
				a.mu.Lock()
				a.stopCoreLocked()
				a.mu.Unlock()
				return errConnectAborted
			case <-time.After(400 * time.Millisecond):
			}
		}

		a.mu.Lock()
		a.stopCoreLocked()
		a.mu.Unlock()
		return errors.New("core did not become ready in time (check Sloth service logs and SlothClash/runtime/<profile-id>/ under your config directory)")
	}

	ctrlPort, err := pickFreePort()
	if err != nil {
		return err
	}
	if err := writeRuntimeConfigIfNeeded(a, bin, dataDir, profile, ctrlPort, mixedPort, secret, traffic, true, enableTun); err != nil {
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
		return err
	}
	cmd.Stdout = lf
	cmd.Stderr = lf

	if err := cmd.Start(); err != nil {
		cancel()
		_ = lf.Close()
		return err
	}

	a.mu.Lock()
	prevMixed := a.state.Core.MixedPort
	a.coreOverPipe = false
	a.coreCmd = cmd
	a.coreCancel = cancel
	a.coreProcToken++
	a.coreSecret = secret
	a.coreListen = fmt.Sprintf("127.0.0.1:%d", ctrlPort)
	a.state.Core.ControllerAddr = a.coreListen
	a.state.Core.MixedPort = mixedPort
	a.state.Core.Running = true
	a.state.Core.Lifecycle = "running"
	a.state.Core.LastError = ""

	listenCopy := a.coreListen
	secretCopy := a.coreSecret
	waitCmd := cmd
	waitToken := a.coreProcToken
	a.mu.Unlock()
	if runtime.GOOS == "windows" {
		go a.handleMixedPortChangeForWindowsSysProxy(prevMixed, mixedPort)
	}

	go func() {
		waitErr := waitCmd.Wait()
		_ = lf.Close()
		a.mu.Lock()
		if waitToken != a.coreProcToken || a.coreCmd != waitCmd {
			a.mu.Unlock()
			return
		}
		if a.coreStopIntentional {
			a.mu.Unlock()
			return
		}
		notify := false
		shouldAutoRestart := false
		var restartProfileID, restartTraffic string
		if waitErr != nil && !errors.Is(waitErr, context.Canceled) {
			prevStatus := strings.TrimSpace(a.state.Connection.Status)
			a.state.Core.Running = false
			a.state.Core.Lifecycle = "degraded"
			a.state.Connection.Status = ConnError
			a.state.Connection.Health = ""
			a.state.Connection.LastError = "core exited: " + waitErr.Error()
			a.state.Core.LastError = waitErr.Error()
			notify = true
			// Auto-restart only when the user was actively connected. A warm
			// boot core (status == "disconnected") dying is not worth a noisy
			// retry; the user will get fresh attempt on next Connect.
			if prevStatus == "connected" {
				shouldAutoRestart = true
				restartProfileID = a.coreActiveProfileID
				restartTraffic = strings.TrimSpace(a.state.Traffic)
			}
		}
		a.state.UpdatedAt = time.Now().Unix()
		a.mu.Unlock()
		if notify {
			a.emitAppStateChanged()
		}
		if shouldAutoRestart && restartProfileID != "" {
			a.traceEvent("core.exit.unexpected", "fail", 0, map[string]any{
				"profileId": restartProfileID,
				"error":     waitErr.Error(),
			})
			go a.attemptCoreAutoRestart(restartProfileID, restartTraffic)
		}
	}()

	deadline := time.Now().Add(45 * time.Second)
	for time.Now().Before(deadline) {
		select {
		case <-parent.Done():
			cancel()
			a.mu.Lock()
			a.stopCoreLocked()
			a.mu.Unlock()
			return errConnectAborted
		default:
		}
		if a.connectGen.Load() != gen {
			cancel()
			a.mu.Lock()
			a.stopCoreLocked()
			a.mu.Unlock()
			return errConnectAborted
		}
		v, verr := fetchVersionAt(listenCopy, secretCopy)
		if verr == nil && v != "" {
			a.mu.Lock()
			a.state.Core.Version = v
			a.state.Core.LastError = ""
			a.coreActiveProfileID = profile.ID
			a.state.UpdatedAt = time.Now().Unix()
			a.mu.Unlock()
			return nil
		}
		select {
		case <-parent.Done():
			cancel()
			a.mu.Lock()
			a.stopCoreLocked()
			a.mu.Unlock()
			return errConnectAborted
		case <-time.After(400 * time.Millisecond):
		}
	}

	cancel()
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
