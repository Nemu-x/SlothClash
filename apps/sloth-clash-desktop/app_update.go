package main

import (
	"context"
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

	githubAPIHTTPTimeout   = 45 * time.Second
	updateDownloadTimeout = 60 * time.Minute
)

var (
	githubAPIHTTPClient       = &http.Client{Timeout: githubAPIHTTPTimeout}
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

// ApplyUpdate downloads the latest Windows installer asset (if any) and runs it to upgrade in place.
func (a *App) ApplyUpdate() error {
	if runtime.GOOS != "windows" {
		return errors.New("in-app installer launch is only supported on Windows — open the release page from Settings")
	}
	a.mu.RLock()
	url := strings.TrimSpace(a.update.AssetDownloadURL)
	a.mu.RUnlock()
	if url == "" {
		return errors.New("no installer URL — run Check for updates first")
	}
	tmp := filepath.Join(os.TempDir(), "SlothClash-desktop-update.exe")
	out, err := os.Create(tmp)
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
	if cerr != nil {
		return cerr
	}
	cmd := exec.Command(tmp)
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	return cmd.Start()
}
