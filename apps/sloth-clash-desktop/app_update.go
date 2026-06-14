package main

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"time"

	wailsrt "github.com/wailsapp/wails/v2/pkg/runtime"
)

const (
	githubOwner = "Nemu-x"
	githubRepo  = "SlothClash"

	githubAPIHTTPTimeout  = 45 * time.Second
	updateDownloadTimeout = 60 * time.Minute
)

var (
	githubAPIHTTPClient      = &http.Client{Timeout: githubAPIHTTPTimeout}
	updateDownloadHTTPClient = &http.Client{Timeout: updateDownloadTimeout}
)

type githubRelease struct {
	TagName string        `json:"tag_name"`
	HTMLURL string        `json:"html_url"`
	Body    string        `json:"body"`
	Assets  []githubAsset `json:"assets"`
}

type githubAsset struct {
	Name        string `json:"name"`
	DownloadURL string `json:"browser_download_url"`
}

func stripV(s string) string {
	return strings.TrimPrefix(strings.TrimSpace(s), "v")
}

func parseVersionParts(s string) (a, b, c int) {
	s = stripV(s)
	parts := strings.Split(s, ".")
	if len(parts) >= 1 {
		a, _ = strconv.Atoi(parts[0])
	}
	if len(parts) >= 2 {
		b, _ = strconv.Atoi(parts[1])
	}
	if len(parts) >= 3 {
		p3 := parts[2]
		for i, r := range p3 {
			if r < '0' || r > '9' {
				p3 = p3[:i]
				break
			}
		}
		c, _ = strconv.Atoi(p3)
	}
	return
}

// remoteIsNewer returns true if remoteTag is a higher version than localVer (e.g. 0.2.0 vs 0.1.0).
func remoteIsNewer(remoteTag, localVer string) bool {
	r1, r2, r3 := parseVersionParts(remoteTag)
	l1, l2, l3 := parseVersionParts(localVer)
	if r1 != l1 {
		return r1 > l1
	}
	if r2 != l2 {
		return r2 > l2
	}
	return r3 > l3
}

func windowsInstallerArchToken() string {
	switch runtime.GOARCH {
	case "arm64":
		return "arm64"
	default:
		// amd64 build runs on Windows x64 and on ARM64 via x64 emulation.
		return "amd64"
	}
}

func pickWindowsInstallerAsset(assets []githubAsset) (name, url string) {
	arch := windowsInstallerArchToken()
	for _, as := range assets {
		n := strings.ToLower(as.Name)
		if !strings.HasSuffix(n, ".exe") {
			continue
		}
		if strings.Contains(n, "installer") && strings.Contains(n, arch) {
			return as.Name, as.DownloadURL
		}
	}
	// Legacy fallback when release assets omit the arch token we expect.
	for _, as := range assets {
		n := strings.ToLower(as.Name)
		if !strings.HasSuffix(n, ".exe") {
			continue
		}
		if strings.Contains(n, "installer") && (strings.Contains(n, "amd64") || strings.Contains(n, "x64")) {
			return as.Name, as.DownloadURL
		}
	}
	for _, as := range assets {
		n := strings.ToLower(as.Name)
		if strings.HasSuffix(n, ".exe") && strings.Contains(n, "installer") {
			return as.Name, as.DownloadURL
		}
	}
	return "", ""
}

// pickChecksumsAsset locates a SHA256SUMS-style file published alongside the
// installer. The canonical convention (used by goreleaser, GitHub release
// tooling, and most Linux distros) is one of:
//   - SHA256SUMS
//   - SHA256SUMS.txt
//   - checksums.txt
//
// We accept any of them so the release workflow has flexibility. Returns
// ("", "") if the release does not ship a checksums file — in that mode the
// updater falls back to download-without-verification with a runtime warning.
func pickChecksumsAsset(assets []githubAsset) (name, url string) {
	for _, as := range assets {
		n := strings.ToLower(strings.TrimSpace(as.Name))
		switch n {
		case "sha256sums", "sha256sums.txt", "checksums.txt", "checksums-sha256.txt":
			return as.Name, as.DownloadURL
		}
	}
	return "", ""
}

// parseChecksumsFile reads SHA256SUMS-style content and returns a map keyed
// by file name → lowercase hex digest. The standard format is:
//
//	<hex-digest>  <filename>
//
// optionally with a leading "*" before the filename (sha256sum -b). Empty
// lines and lines starting with "#" are ignored.
func parseChecksumsFile(body []byte) map[string]string {
	out := map[string]string{}
	for _, line := range strings.Split(string(body), "\n") {
		s := strings.TrimSpace(line)
		if s == "" || strings.HasPrefix(s, "#") {
			continue
		}
		fields := strings.Fields(s)
		if len(fields) < 2 {
			continue
		}
		digest := strings.ToLower(strings.TrimSpace(fields[0]))
		name := strings.TrimPrefix(strings.TrimSpace(fields[len(fields)-1]), "*")
		if len(digest) != 64 || name == "" {
			continue
		}
		out[name] = digest
	}
	return out
}

// fetchReleaseAssets returns the asset list of the latest GitHub release.
func fetchReleaseAssets() ([]githubAsset, error) {
	u := fmt.Sprintf("https://api.github.com/repos/%s/%s/releases/latest", githubOwner, githubRepo)
	req, err := http.NewRequest(http.MethodGet, u, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Accept", "application/vnd.github+json")
	req.Header.Set("User-Agent", "SlothClashDesktop/"+AppVersion)
	resp, err := githubAPIHTTPClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(io.LimitReader(resp.Body, 8*1024*1024))
	if err != nil {
		return nil, err
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, fmt.Errorf("GitHub API %s", resp.Status)
	}
	var rel githubRelease
	if err := json.Unmarshal(body, &rel); err != nil {
		return nil, err
	}
	return rel.Assets, nil
}

// downloadAssetBytes fetches a (small) release asset into memory.
func downloadAssetBytes(url string, limit int64) ([]byte, error) {
	req, err := http.NewRequest(http.MethodGet, url, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("User-Agent", "SlothClashDesktop/"+AppVersion)
	resp, err := githubAPIHTTPClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, fmt.Errorf("download failed: %s", resp.Status)
	}
	return io.ReadAll(io.LimitReader(resp.Body, limit))
}

// pickSignatureAsset locates the minisign signature published for the checksums
// file (SHA256SUMS.minisig / SHA256SUMS.sig, or any *.minisig).
func pickSignatureAsset(assets []githubAsset) (name, url string) {
	for _, as := range assets {
		switch strings.ToLower(strings.TrimSpace(as.Name)) {
		case "sha256sums.minisig", "sha256sums.sig", "checksums.txt.minisig":
			return as.Name, as.DownloadURL
		}
	}
	for _, as := range assets {
		if strings.HasSuffix(strings.ToLower(strings.TrimSpace(as.Name)), ".minisig") {
			return as.Name, as.DownloadURL
		}
	}
	return "", ""
}

// resolveInstallerDigest fetches the release's checksums file and, if present,
// its minisign signature; verifies the signature against the embedded trusted
// keys; and returns the expected installer digest.
//
//   - verified=true only when a signature was present AND verified.
//   - A present-but-invalid signature is a hard error (an attack signal).
//   - No checksums file → ("", false, nil): the caller's fail-closed policy
//     decides whether to proceed (see ApplyUpdate).
func (a *App) resolveInstallerDigest(installerName string) (digest string, verified bool, err error) {
	if strings.TrimSpace(installerName) == "" {
		return "", false, nil
	}
	assets, err := fetchReleaseAssets()
	if err != nil {
		return "", false, fmt.Errorf("look up release: %w", err)
	}
	_, csURL := pickChecksumsAsset(assets)
	if csURL == "" {
		return "", false, nil
	}
	csBody, err := downloadAssetBytes(csURL, 1*1024*1024)
	if err != nil {
		return "", false, fmt.Errorf("download checksums: %w", err)
	}

	_, sigURL := pickSignatureAsset(assets)
	if sigURL != "" {
		sigBody, err := downloadAssetBytes(sigURL, 64*1024)
		if err != nil {
			return "", false, fmt.Errorf("download signature: %w", err)
		}
		if err := verifyMinisign(csBody, sigBody, trustedUpdateKeys); err != nil {
			return "", false, fmt.Errorf("checksums signature verification failed: %w", err)
		}
		verified = true
	}

	table := parseChecksumsFile(csBody)
	if h, ok := table[installerName]; ok {
		return h, verified, nil
	}
	for name, d := range table {
		if strings.EqualFold(name, installerName) {
			return d, verified, nil
		}
	}
	return "", verified, fmt.Errorf("checksums file did not list %q", installerName)
}

// hashFileSHA256 returns the lowercase hex SHA-256 digest of the file at path.
func hashFileSHA256(path string) (string, error) {
	f, err := os.Open(path)
	if err != nil {
		return "", err
	}
	defer f.Close()
	h := sha256.New()
	if _, err := io.Copy(h, f); err != nil {
		return "", err
	}
	return hex.EncodeToString(h.Sum(nil)), nil
}

func fetchLatestGitHubRelease() (tag, htmlURL, notes, assetName, assetURL string, err error) {
	u := fmt.Sprintf("https://api.github.com/repos/%s/%s/releases/latest", githubOwner, githubRepo)
	req, err := http.NewRequest(http.MethodGet, u, nil)
	if err != nil {
		return "", "", "", "", "", err
	}
	req.Header.Set("Accept", "application/vnd.github+json")
	req.Header.Set("User-Agent", "SlothClashDesktop/"+AppVersion)

	resp, err := githubAPIHTTPClient.Do(req)
	if err != nil {
		return "", "", "", "", "", err
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", "", "", "", "", err
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		snippet := string(body)
		if len(snippet) > 200 {
			snippet = snippet[:200]
		}
		return "", "", "", "", "", fmt.Errorf("GitHub API %s: %s", resp.Status, strings.TrimSpace(snippet))
	}
	var rel githubRelease
	if err := json.Unmarshal(body, &rel); err != nil {
		return "", "", "", "", "", err
	}
	tag = strings.TrimSpace(rel.TagName)
	htmlURL = strings.TrimSpace(rel.HTMLURL)
	notes = strings.TrimSpace(rel.Body)
	assetName, assetURL = pickWindowsInstallerAsset(rel.Assets)
	return tag, htmlURL, notes, assetName, assetURL, nil
}

func (a *App) runGitHubUpdateCheck() {
	tag, htmlURL, notes, assetName, assetURL, err := fetchLatestGitHubRelease()

	a.mu.Lock()
	a.update.LastCheckedAt = time.Now().Unix()
	a.update.CurrentVersion = AppVersion
	a.update.ReleaseURL = htmlURL
	a.update.ReleaseNotes = notes
	a.update.LatestVersion = stripV(tag)
	a.update.AssetName = assetName
	a.update.AssetDownloadURL = assetURL
	a.update.LastError = ""

	if err != nil {
		a.update.LastError = err.Error()
		a.update.HasUpdate = false
		a.mu.Unlock()
		a.emitUpdateEvent()
		return
	}
	if tag == "" {
		a.update.HasUpdate = false
		a.mu.Unlock()
		a.emitUpdateEvent()
		return
	}
	a.update.HasUpdate = remoteIsNewer(tag, AppVersion)
	a.mu.Unlock()
	a.emitUpdateEvent()
}

func (a *App) emitUpdateEvent() {
	if a.ctx == nil {
		return
	}
	go wailsrt.EventsEmit(a.ctx, "app:update", map[string]any{})
}

func (a *App) updateCheckLoop(ctx context.Context) {
	// The loop stays alive even when auto-check is disabled so toggling it back
	// on in Settings takes effect without an app restart — we just skip the
	// actual GitHub call while it's off. Manual CheckForUpdates is unaffected.
	select {
	case <-time.After(50 * time.Second):
		if currentDesktopPrefs().AppUpdate.IsAutoCheckEnabled() {
			a.runGitHubUpdateCheck()
		}
	case <-ctx.Done():
		return
	}
	t := time.NewTicker(6 * time.Hour)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
			if currentDesktopPrefs().AppUpdate.IsAutoCheckEnabled() {
				a.runGitHubUpdateCheck()
			}
		}
	}
}

// CheckForUpdates queries GitHub releases/latest and refreshes update state.
func (a *App) CheckForUpdates() UpdateState {
	a.runGitHubUpdateCheck()
	a.mu.RLock()
	defer a.mu.RUnlock()
	return a.update
}

// ApplyUpdate downloads the latest Windows installer asset, verifies it, and
// launches it to upgrade in place. It is **fail-closed (secure by default):**
//
//  1. Download the installer to a temp file.
//  2. Fetch the release's checksums file + its minisign signature; verify the
//     signature against the trusted public key(s) embedded in the binary
//     (`verifyMinisign` in resolveInstallerDigest). A present-but-invalid
//     signature is a hard error.
//  3. Verify the downloaded installer's SHA-256 against the (now-authenticated)
//     checksums. Any mismatch deletes the temp file and aborts — no launch.
//  4. Only then tear down the core/TUN and launch the installer.
//
// If the release is NOT signed by a trusted key, the update is REFUSED unless
// `SLOTH_ALLOW_UNVERIFIED_UPDATE=1` is set (local testing only). This closes
// audit findings F1/F8. (Signature verification is implemented here, not a
// future TODO.)
func (a *App) ApplyUpdate() error {
	if runtime.GOOS != "windows" {
		return errors.New("in-app installer launch is only supported on Windows — open the release page from Settings")
	}
	a.mu.RLock()
	url := strings.TrimSpace(a.update.AssetDownloadURL)
	installerName := strings.TrimSpace(a.update.AssetName)
	a.mu.RUnlock()
	if url == "" {
		return errors.New("no installer URL — run Check for updates first")
	}

	tmp := filepath.Join(os.TempDir(), "SlothClash-desktop-update.exe")
	if err := a.downloadUpdateAsset(url, tmp); err != nil {
		return err
	}

	// Verify the release is authentic before launching anything. Secure by
	// default (fail-closed): require a checksums file signed by a trusted minisign
	// key, then verify the downloaded installer's digest against it. The
	// SLOTH_ALLOW_UNVERIFIED_UPDATE=1 escape hatch (local testing only) downgrades
	// to best-effort. A present-but-invalid signature is always refused.
	allowUnverified := strings.EqualFold(strings.TrimSpace(os.Getenv("SLOTH_ALLOW_UNVERIFIED_UPDATE")), "1")
	digest, verified, vErr := a.resolveInstallerDigest(installerName)
	if vErr != nil {
		_ = os.Remove(tmp)
		return fmt.Errorf("refusing to launch update: %w", vErr)
	}
	if !verified && !allowUnverified {
		_ = os.Remove(tmp)
		return errors.New("refusing to launch update: release is not signed by a trusted key — set SLOTH_ALLOW_UNVERIFIED_UPDATE=1 to override (local testing only)")
	}
	if digest != "" {
		gotHash, err := hashFileSHA256(tmp)
		if err != nil {
			_ = os.Remove(tmp)
			return fmt.Errorf("could not hash downloaded installer: %w", err)
		}
		if !strings.EqualFold(gotHash, digest) {
			_ = os.Remove(tmp)
			return fmt.Errorf(
				"installer integrity check failed: expected sha256=%s, got %s — refusing to launch",
				digest, gotHash,
			)
		}
	} else if !allowUnverified {
		_ = os.Remove(tmp)
		return errors.New("refusing to launch update: no checksum found for the installer")
	}
	a.traceEvent("update.verify", "ok", 0, map[string]any{
		"asset":    installerName,
		"signed":   verified,
		"digested": digest != "",
	})

	// Tear down the core + TUN before handing off to the installer. The installer
	// kills this process to replace it, bypassing the normal shutdown() path; without
	// this the core (and its wintun adapter) survive the update and the next launch's
	// first Connect hits an already-up TUN. See fix-tun-teardown-on-update.
	a.traceEvent("update.teardown", "core+tun", 0, nil)
	a.drainTunAndStopCore()

	if err := launchUpdateInstaller(tmp, windowsUpdateInstallerArgs()); err != nil {
		return err
	}
	// Exit promptly so the elevated NSIS installer is not fighting this process
	// (and any WebView2/mihomo children) for file locks. ShellExecute returns as
	// soon as the UAC prompt is shown; the installer starts only after the user
	// accepts, so a graceful Quit() from the frontend is too late and too slow.
	scheduleProcessExitForUpdateHandoff()
	return nil
}

func (a *App) downloadUpdateAsset(url, dest string) error {
	out, err := os.Create(dest)
	if err != nil {
		return err
	}
	req, err := http.NewRequest(http.MethodGet, url, nil)
	if err != nil {
		out.Close()
		return err
	}
	req.Header.Set("User-Agent", "SlothClashDesktop/"+AppVersion)
	resp, err := updateDownloadHTTPClient.Do(req)
	if err != nil {
		out.Close()
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		out.Close()
		return fmt.Errorf("download failed: %s", resp.Status)
	}

	// Stream with throttled progress events so the UI can show a download bar.
	total := resp.ContentLength
	a.emitUpdateProgress(0, total)
	var downloaded int64
	buf := make([]byte, 64*1024)
	lastEmit := time.Now()
	for {
		n, rerr := resp.Body.Read(buf)
		if n > 0 {
			if _, werr := out.Write(buf[:n]); werr != nil {
				out.Close()
				return werr
			}
			downloaded += int64(n)
			if time.Since(lastEmit) >= 150*time.Millisecond {
				a.emitUpdateProgress(downloaded, total)
				lastEmit = time.Now()
			}
		}
		if rerr == io.EOF {
			break
		}
		if rerr != nil {
			out.Close()
			return rerr
		}
	}
	a.emitUpdateProgress(downloaded, total)
	return out.Close()
}

// emitUpdateProgress notifies the UI of download progress. pct is -1 when the
// server did not report a content length.
func (a *App) emitUpdateProgress(downloaded, total int64) {
	if a.ctx == nil {
		return
	}
	pct := -1.0
	if total > 0 {
		pct = float64(downloaded) / float64(total) * 100
	}
	wailsrt.EventsEmit(a.ctx, "app:update:progress", map[string]any{
		"downloaded": downloaded,
		"total":      total,
		"pct":        pct,
	})
}
