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
	"os/exec"
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

func pickWindowsInstallerAsset(assets []githubAsset) (name, url string) {
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

// fetchExpectedSHA256 grabs the release's checksums file (if any) and
// returns the digest for `installerName`. Returns ("", nil) when the release
// ships no checksums file at all — the caller decides whether to allow the
// update without verification (controlled by SLOTH_ALLOW_UNVERIFIED_UPDATE).
func fetchExpectedSHA256(installerName string) (string, error) {
	if strings.TrimSpace(installerName) == "" {
		return "", nil
	}
	tag, _, _, _, err := fetchLatestGitHubRelease()
	if err != nil {
		return "", fmt.Errorf("look up release: %w", err)
	}
	if strings.TrimSpace(tag) == "" {
		return "", nil
	}
	// Re-fetch release JSON to scan all assets (the cached pick returned only
	// the installer; we want the checksums asset too).
	u := fmt.Sprintf("https://api.github.com/repos/%s/%s/releases/latest", githubOwner, githubRepo)
	req, err := http.NewRequest(http.MethodGet, u, nil)
	if err != nil {
		return "", err
	}
	req.Header.Set("Accept", "application/vnd.github+json")
	req.Header.Set("User-Agent", "SlothClashDesktop/"+AppVersion)
	resp, err := githubAPIHTTPClient.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", err
	}
	var rel githubRelease
	if err := json.Unmarshal(body, &rel); err != nil {
		return "", err
	}
	_, csURL := pickChecksumsAsset(rel.Assets)
	if csURL == "" {
		return "", nil
	}
	csReq, err := http.NewRequest(http.MethodGet, csURL, nil)
	if err != nil {
		return "", err
	}
	csReq.Header.Set("User-Agent", "SlothClashDesktop/"+AppVersion)
	csResp, err := githubAPIHTTPClient.Do(csReq)
	if err != nil {
		return "", err
	}
	defer csResp.Body.Close()
	if csResp.StatusCode < 200 || csResp.StatusCode >= 300 {
		return "", fmt.Errorf("checksums download failed: %s", csResp.Status)
	}
	csBody, err := io.ReadAll(csResp.Body)
	if err != nil {
		return "", err
	}
	table := parseChecksumsFile(csBody)
	if h, ok := table[installerName]; ok {
		return h, nil
	}
	// Some checksum files use just the basename without paths; fallback to
	// case-insensitive scan in case publisher renamed the installer asset.
	for name, digest := range table {
		if strings.EqualFold(name, installerName) {
			return digest, nil
		}
	}
	return "", fmt.Errorf("checksums file did not list %q", installerName)
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

func fetchLatestGitHubRelease() (tag, htmlURL, assetName, assetURL string, err error) {
	u := fmt.Sprintf("https://api.github.com/repos/%s/%s/releases/latest", githubOwner, githubRepo)
	req, err := http.NewRequest(http.MethodGet, u, nil)
	if err != nil {
		return "", "", "", "", err
	}
	req.Header.Set("Accept", "application/vnd.github+json")
	req.Header.Set("User-Agent", "SlothClashDesktop/"+AppVersion)

	resp, err := githubAPIHTTPClient.Do(req)
	if err != nil {
		return "", "", "", "", err
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", "", "", "", err
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		snippet := string(body)
		if len(snippet) > 200 {
			snippet = snippet[:200]
		}
		return "", "", "", "", fmt.Errorf("GitHub API %s: %s", resp.Status, strings.TrimSpace(snippet))
	}
	var rel githubRelease
	if err := json.Unmarshal(body, &rel); err != nil {
		return "", "", "", "", err
	}
	tag = strings.TrimSpace(rel.TagName)
	htmlURL = strings.TrimSpace(rel.HTMLURL)
	assetName, assetURL = pickWindowsInstallerAsset(rel.Assets)
	return tag, htmlURL, assetName, assetURL, nil
}

func (a *App) runGitHubUpdateCheck() {
	tag, htmlURL, assetName, assetURL, err := fetchLatestGitHubRelease()

	a.mu.Lock()
	a.update.LastCheckedAt = time.Now().Unix()
	a.update.CurrentVersion = AppVersion
	a.update.ReleaseURL = htmlURL
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
	select {
	case <-time.After(50 * time.Second):
		a.runGitHubUpdateCheck()
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
			a.runGitHubUpdateCheck()
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

// ApplyUpdate downloads the latest Windows installer asset (if any), verifies
// its SHA-256 against the release's checksums file, and launches it to
// upgrade in place.
//
// Verification rules:
//   - If a SHA256SUMS / checksums.txt is published with the release: the
//     downloaded installer's digest MUST match. Mismatch deletes the temp
//     file and aborts — no installer launch.
//   - If no checksums file is published: by default the update still runs
//     (legacy releases shipped without it). Operators who want strict
//     verification can set SLOTH_REQUIRE_VERIFIED_UPDATE=1 to refuse such
//     releases.
//
// Signature verification (cosign / ed25519) is left as a future addition —
// the structure here is ready for it: insert the verify step between the
// hash check and cmd.Start().
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
	if err := downloadUpdateAsset(url, tmp); err != nil {
		return err
	}

	expectedHash, hashErr := fetchExpectedSHA256(installerName)
	if hashErr != nil {
		_ = os.Remove(tmp)
		return fmt.Errorf("could not fetch expected checksum: %w", hashErr)
	}
	if expectedHash != "" {
		gotHash, err := hashFileSHA256(tmp)
		if err != nil {
			_ = os.Remove(tmp)
			return fmt.Errorf("could not hash downloaded installer: %w", err)
		}
		if !strings.EqualFold(gotHash, expectedHash) {
			_ = os.Remove(tmp)
			return fmt.Errorf(
				"installer integrity check failed: expected sha256=%s, got %s — refusing to launch",
				expectedHash, gotHash,
			)
		}
		a.traceEvent("update.verify.sha256", "ok", 0, map[string]any{
			"asset": installerName,
		})
	} else {
		if strings.EqualFold(strings.TrimSpace(os.Getenv("SLOTH_REQUIRE_VERIFIED_UPDATE")), "1") {
			_ = os.Remove(tmp)
			return errors.New("release ships no checksums file and SLOTH_REQUIRE_VERIFIED_UPDATE=1 is set — refusing to launch")
		}
		a.traceEvent("update.verify.sha256", "skip", 0, map[string]any{
			"asset":  installerName,
			"reason": "no checksums file published",
		})
	}

	// Tear down the core + TUN before handing off to the installer. The installer
	// kills this process to replace it, bypassing the normal shutdown() path; without
	// this the core (and its wintun adapter) survive the update and the next launch's
	// first Connect hits an already-up TUN. See fix-tun-teardown-on-update.
	a.traceEvent("update.teardown", "core+tun", 0, nil)
	a.drainTunAndStopCore()

	cmd := exec.Command(tmp)
	if attr := hideWindowSysProcAttr(); attr != nil {
		cmd.SysProcAttr = attr
	}
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	return cmd.Start()
}

func downloadUpdateAsset(url, dest string) error {
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
	_, err = io.Copy(out, resp.Body)
	cerr := out.Close()
	if err != nil {
		return err
	}
	return cerr
}
