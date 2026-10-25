package main

import (
	"bytes"
	"context"
	"io"
	"net/http"
	"strconv"
	"strings"
	"testing"
	"time"
)

func ipcTestResponse(status int, body []byte) *http.Response {
	return &http.Response{StatusCode: status, Body: io.NopCloser(bytes.NewReader(body))}
}

func TestReadSlothIPCBodyRefusesOversizedJSON(t *testing.T) {
	doc := []byte(`{"code":0,"message":"ok","data":{"version":"1.2.3"}}`)
	got, err := readSlothIPCBody(ipcTestResponse(200, padJSON(t, doc, slothIPCMaxBodyBytes)), http.MethodGet, "/version")
	if err != nil || len(got) != slothIPCMaxBodyBytes {
		t.Fatalf("at limit: %d bytes, %v", len(got), err)
	}
	var env struct{ Message string }
	if err := decodeBoundedJSON(bytes.NewReader(got), "x", slothIPCMaxBodyBytes, &env); err != nil || env.Message != "ok" {
		t.Fatalf("at-limit envelope did not parse: %+v %v", env, err)
	}
	_, err = readSlothIPCBody(ipcTestResponse(200, padJSON(t, doc, slothIPCMaxBodyBytes+1)), http.MethodGet, "/version")
	if err == nil || err.Error() != "IPC GET /version response exceeds the 512 KiB limit" {
		t.Fatalf("oversized 2xx body: %v", err)
	}
	// Error bodies are only quoted, so they are still truncated, not refused.
	got, err = readSlothIPCBody(ipcTestResponse(500, bytes.Repeat([]byte("e"), slothIPCMaxBodyBytes+10)), http.MethodPost, "/clash/start")
	if err != nil || len(got) != slothIPCMaxBodyBytes {
		t.Fatalf("oversized error body: %d bytes, %v", len(got), err)
	}
}

func TestFetchVersionAtAndOverLimit(t *testing.T) {
	doc := []byte(`{"meta":true,"version":"v1.19.28"}`)
	at := coreJSONServer(t, map[string][]byte{"/version": padJSON(t, doc, coreVersionMaxJSONBytes)})
	if v, err := fetchVersionAt(at, ""); err != nil || v != "v1.19.28" {
		t.Fatalf("at limit: %q %v", v, err)
	}
	over := coreJSONServer(t, map[string][]byte{"/version": padJSON(t, doc, coreVersionMaxJSONBytes+1)})
	if _, err := fetchVersionAt(over, ""); err == nil || err.Error() != "GET /version response exceeds the 64 KiB limit" {
		t.Fatalf("oversized body: %v", err)
	}
}

func TestFetchMihomoDelayAtAndOverLimit(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	doc := []byte(`{"delay":123}`)
	at := coreJSONServer(t, map[string][]byte{"/proxies/node/delay": padJSON(t, doc, coreDelayMaxJSONBytes)})
	if ms, err := fetchMihomoDelayToURL(ctx, at, "", "node", "http://example.test/204"); err != nil || ms != 123 {
		t.Fatalf("at limit: %d %v", ms, err)
	}
	over := coreJSONServer(t, map[string][]byte{"/proxies/node/delay": padJSON(t, doc, coreDelayMaxJSONBytes+1)})
	if _, err := fetchMihomoDelayToURL(ctx, over, "", "node", "http://example.test/204"); err == nil ||
		err.Error() != "GET /proxies/<name>/delay response exceeds the 4 KiB limit" {
		t.Fatalf("oversized body: %v", err)
	}
}

type roundTripFunc func(*http.Request) (*http.Response, error)

func (f roundTripFunc) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

func TestFetchReleaseAssetsAtAndOverLimit(t *testing.T) {
	doc := []byte(`{"tag_name":"v1.0.0","assets":[{"name":"SlothClash-amd64-installer.exe","browser_download_url":"https://example.test/a.exe"}]}`)
	var body []byte
	prev := githubAPIHTTPClient
	githubAPIHTTPClient = &http.Client{Transport: roundTripFunc(func(r *http.Request) (*http.Response, error) {
		if r.URL.Host != "api.github.com" {
			t.Errorf("unexpected host %s", r.URL.Host)
		}
		return &http.Response{StatusCode: 200, Status: "200 OK", Body: io.NopCloser(bytes.NewReader(body)), Request: r}, nil
	})}
	t.Cleanup(func() { githubAPIHTTPClient = prev })

	body = padJSON(t, doc, githubReleaseMaxJSONBytes)
	assets, err := fetchReleaseAssets()
	if err != nil || len(assets) != 1 || assets[0].Name != "SlothClash-amd64-installer.exe" {
		t.Fatalf("at limit: %+v %v", assets, err)
	}
	body = padJSON(t, doc, githubReleaseMaxJSONBytes+1)
	if _, err := fetchReleaseAssets(); err == nil || err.Error() != "GitHub API response exceeds the 8 MiB limit" {
		t.Fatalf("oversized body: %v", err)
	}
}

// stubGitHubHTTP serves routes (keyed by URL path) through githubAPIHTTPClient
// for the duration of the test.
func stubGitHubHTTP(t *testing.T, status int, routes map[string][]byte) {
	t.Helper()
	prev := githubAPIHTTPClient
	githubAPIHTTPClient = &http.Client{Transport: roundTripFunc(func(r *http.Request) (*http.Response, error) {
		body, ok := routes[r.URL.Path]
		if !ok {
			t.Errorf("unexpected request %s", r.URL)
			return &http.Response{StatusCode: 404, Status: "404 Not Found", Body: io.NopCloser(bytes.NewReader(nil)), Request: r}, nil
		}
		return &http.Response{StatusCode: status, Status: strconv.Itoa(status) + " " + http.StatusText(status), Body: io.NopCloser(bytes.NewReader(body)), Request: r}, nil
	})}
	t.Cleanup(func() { githubAPIHTTPClient = prev })
}

func TestFetchLatestGitHubReleaseAtAndOverLimit(t *testing.T) {
	const path = "/repos/" + githubOwner + "/" + githubRepo + "/releases/latest"
	doc := []byte(`{"tag_name":"v1.0.0","html_url":"https://example.test/r","body":"notes"}`)

	stubGitHubHTTP(t, 200, map[string][]byte{path: padJSON(t, doc, githubReleaseMaxJSONBytes)})
	tag, htmlURL, notes, _, _, err := fetchLatestGitHubRelease()
	if err != nil || tag != "v1.0.0" || htmlURL != "https://example.test/r" || notes != "notes" {
		t.Fatalf("at limit: %q %q %q %v", tag, htmlURL, notes, err)
	}

	stubGitHubHTTP(t, 200, map[string][]byte{path: padJSON(t, doc, githubReleaseMaxJSONBytes+1)})
	if _, _, _, _, _, err := fetchLatestGitHubRelease(); err == nil || err.Error() != "GitHub API response exceeds the 8 MiB limit" {
		t.Fatalf("oversized body: %v", err)
	}

	// Error bodies are only quoted: an oversized one still reports the status.
	stubGitHubHTTP(t, 403, map[string][]byte{path: bytes.Repeat([]byte("e"), githubReleaseMaxJSONBytes+10)})
	want := "GitHub API 403 Forbidden: " + strings.Repeat("e", 200)
	if _, _, _, _, _, err := fetchLatestGitHubRelease(); err == nil || err.Error() != want {
		t.Fatalf("oversized error body: %v", err)
	}
}

func TestDownloadAssetBytesAtAndOverLimit(t *testing.T) {
	const limit = 64 * 1024
	at := bytes.Repeat([]byte("a"), limit)
	stubGitHubHTTP(t, 200, map[string][]byte{"/at": at, "/over": append(at, 'a')})
	got, err := downloadAssetBytes("https://example.test/at", limit)
	if err != nil || !bytes.Equal(got, at) {
		t.Fatalf("at limit: %d bytes, %v", len(got), err)
	}
	got, err = downloadAssetBytes("https://example.test/over", limit)
	if err == nil || got != nil || err.Error() != "release asset response exceeds the 64 KiB limit" {
		t.Fatalf("oversized asset: %d bytes, %v", len(got), err)
	}
}

// An oversized checksums file or signature is refused before signature
// verification instead of being verified (or parsed) truncated.
func TestResolveInstallerDigestRefusesOversizedAssets(t *testing.T) {
	const releasePath = "/repos/" + githubOwner + "/" + githubRepo + "/releases/latest"
	release := []byte(`{"assets":[` +
		`{"name":"SHA256SUMS","browser_download_url":"https://example.test/SHA256SUMS"},` +
		`{"name":"SHA256SUMS.minisig","browser_download_url":"https://example.test/SHA256SUMS.minisig"}]}`)
	checksums := []byte(strings.Repeat("0", 64) + "  SlothClash-amd64-installer.exe\n")

	cases := []struct {
		name      string
		checksums []byte
		signature []byte
		want      string
	}{
		{"checksums", padJSON(t, checksums, 1*1024*1024+1), []byte("sig"), "download checksums: release asset response exceeds the 1 MiB limit"},
		{"signature", checksums, bytes.Repeat([]byte("s"), 64*1024+1), "download signature: release asset response exceeds the 64 KiB limit"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			stubGitHubHTTP(t, 200, map[string][]byte{
				releasePath:           release,
				"/SHA256SUMS":         tc.checksums,
				"/SHA256SUMS.minisig": tc.signature,
			})
			digest, verified, err := (&App{}).resolveInstallerDigest("SlothClash-amd64-installer.exe")
			if err == nil || err.Error() != tc.want || digest != "" || verified {
				t.Fatalf("got %q %v %v, want error %q", digest, verified, err, tc.want)
			}
		})
	}
}
