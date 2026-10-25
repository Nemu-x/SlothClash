package main

import (
	"bytes"
	"context"
	"io"
	"net/http"
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
