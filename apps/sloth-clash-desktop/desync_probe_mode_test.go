package main

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"encoding/json"
	"net"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"
)

// probeCore is a stand-in for the core's GET /proxies/{name}/delay that does
// what the patched core does (mihomo adapter.URLTest + patch 0020): a HEAD to
// url without following redirects, TLS verified against the trusted roots
// for the URL's host, and 503 when the status is outside `expected`. Every
// name dials backend, as if DNS pointed the probed host there. The real
// core's behavior on these cases was measured against public hosts (see
// docs/desync.md, custom domains).
func probeCore(t *testing.T, backend string, roots *x509.CertPool) *httptest.Server {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		target := r.URL.Query().Get("url")
		expected := r.URL.Query().Get("expected")
		client := http.Client{
			Transport: &http.Transport{
				DialContext: func(ctx context.Context, network, _ string) (net.Conn, error) {
					return (&net.Dialer{}).DialContext(ctx, network, backend)
				},
				TLSClientConfig: &tls.Config{RootCAs: roots},
			},
			CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse },
		}
		req, _ := http.NewRequestWithContext(r.Context(), http.MethodHead, target, nil)
		resp, err := client.Do(req)
		if err != nil {
			w.WriteHeader(http.StatusServiceUnavailable)
			_ = json.NewEncoder(w).Encode(map[string]string{"message": err.Error()})
			return
		}
		_ = resp.Body.Close()
		lo, hi, _ := strings.Cut(expected, "-")
		min, _ := strconv.Atoi(lo)
		max, _ := strconv.Atoi(hi)
		if resp.StatusCode < min || resp.StatusCode > max {
			w.WriteHeader(http.StatusServiceUnavailable)
			_ = json.NewEncoder(w).Encode(map[string]string{"message": "HTTP response did not match the expected status"})
			return
		}
		_ = json.NewEncoder(w).Encode(map[string]int{"delay": 7})
	}))
	t.Cleanup(srv.Close)
	return srv
}

func TestDesyncReachableProbeNeedsVerifiedTLSButAnyStatus(t *testing.T) {
	// httptest's certificate is valid for example.com.
	site := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodHead {
			t.Errorf("probe used %s", r.Method)
		}
		http.NotFound(w, r)
	}))
	defer site.Close()
	redirect := httptest.NewTLSServer(http.RedirectHandler("https://elsewhere.invalid/", http.StatusFound))
	defer redirect.Close()
	plain := httptest.NewServer(http.NotFoundHandler())
	defer plain.Close()
	roots := x509.NewCertPool()
	roots.AddCert(site.Certificate())
	roots.AddCert(redirect.Certificate())

	cases := []struct {
		name, backend, domain string
		strict, reachable     bool
	}{
		{name: "valid certificate, 404", backend: site.Listener.Addr().String(), domain: "example.com", strict: false, reachable: true},
		{name: "valid certificate, redirect elsewhere", backend: redirect.Listener.Addr().String(), domain: "example.com", strict: false, reachable: true},
		{name: "certificate for another host", backend: site.Listener.Addr().String(), domain: "blocked.example.org", strict: false, reachable: false},
		{name: "plain HTTP on the TLS port", backend: plain.Listener.Addr().String(), domain: "example.com", strict: false, reachable: false},
	}
	for _, c := range cases {
		core := probeCore(t, c.backend, roots)
		endpoint := strings.TrimPrefix(core.URL, "http://")
		target := desyncCustomTarget(c.domain)
		_, strictErr := probeDesyncOutboundMode(context.Background(), endpoint, "", "DIRECT", target, desyncProbeStrict)
		_, reachErr := probeDesyncOutboundMode(context.Background(), endpoint, "", "DIRECT", target, desyncProbeReachable)
		if (strictErr == nil) != c.strict || (reachErr == nil) != c.reachable {
			t.Errorf("%s: strict err=%v reachable err=%v", c.name, strictErr, reachErr)
		}
		if code := desyncErrorCode(reachErr); !c.reachable && (code == "" || code == "http_status") {
			t.Errorf("%s: code %q for %v", c.name, desyncErrorCode(reachErr), reachErr)
		}
	}
}

func TestDesyncProbeModesSendTheirStatusFilter(t *testing.T) {
	seen := map[string]string{}
	core := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		seen[r.URL.Query().Get("url")] = r.URL.Query().Get("expected")
		_, _ = w.Write([]byte(`{"delay":1}`))
	}))
	defer core.Close()
	endpoint := strings.TrimPrefix(core.URL, "http://")
	_, _ = probeDesyncOutbound(context.Background(), endpoint, "", "SLOTH-DESYNC-YOUTUBE", "https://www.youtube.com/generate_204")
	_, _ = probeDesyncOutboundMode(context.Background(), endpoint, "", "DIRECT", desyncCustomTarget("example.org"), desyncProbeReachable)
	if seen["https://www.youtube.com/generate_204"] != "200-299" || seen["https://example.org/"] != "100-599" {
		t.Fatalf("status filters: %v", seen)
	}
}
