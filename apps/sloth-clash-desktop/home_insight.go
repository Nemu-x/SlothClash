package main

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"golang.org/x/net/proxy"
)

// RefreshHomeInsight measures node latency (mihomo API), exit IP via mixed-port proxy, and optionally direct WAN IP.
func (a *App) RefreshHomeInsight() (AppState, error) {
	if !a.insightRefreshRunning.CompareAndSwap(false, true) {
		return a.GetAppState(), nil
	}
	defer a.insightRefreshRunning.Store(false)

	a.mu.RLock()
	st := a.state.Connection.Status
	mode := strings.TrimSpace(a.state.Mode.Current)
	listen := a.effectiveCoreEndpointLocked()
	secret := a.coreSecret
	mixed := a.state.Core.MixedPort
	groups := append([]ProxyGroup(nil), a.state.Proxy.Groups...)
	ag := strings.TrimSpace(a.state.Proxy.ActiveGroup)
	prevInsight := a.state.Insight
	a.mu.RUnlock()

	if st != "connected" || listen == "" {
		a.mu.Lock()
		a.state.Insight = HomeInsight{}
		a.state.UpdatedAt = time.Now().Unix()
		a.mu.Unlock()
		a.emitAppStateChanged()
		return a.GetAppState(), nil
	}

	delayName := resolveInsightDelayName(groups, ag)
	ins := prevInsight
	ins.UpdatedAt = time.Now().Unix()
	ins.NodeLatencyMs = 0
	ins.LatencyError = ""

	// Each sub-step uses its own deadline. A single shared short context caused
	// "context deadline exceeded" in Rule (delay + ipify retries + geo + direct WAN).
	if delayName != "" {
		ctx, cancel := context.WithTimeout(context.Background(), 16*time.Second)
		ms, err := measureNodeLatencyMs(ctx, listen, secret, delayName)
		if err != nil {
			if !isIgnorableLatencyError(err) {
				ins.LatencyError = trimErr(err.Error())
			}
		} else if ms > 0 {
			ins.NodeLatencyMs = ms
		}
		cancel()

		// Fast-path UI update: publish node latency as soon as it's measured
		// so Home reflects a node switch quickly, without waiting for slower
		// sub-steps (exit IP / geo / direct WAN checks).
		a.mu.Lock()
		a.state.Insight.NodeLatencyMs = ins.NodeLatencyMs
		a.state.Insight.LatencyError = ins.LatencyError
		a.state.Insight.UpdatedAt = time.Now().Unix()
		a.state.UpdatedAt = time.Now().Unix()
		a.mu.Unlock()
		a.emitAppStateChanged()
	}

	if listen != "" {
		ctx, cancel := context.WithTimeout(context.Background(), 8*time.Second)
		if up, down, err := fetchMihomoTraffic(ctx, listen, secret); err != nil {
			ins.TrafficError = trimErr(err.Error())
		} else {
			ins.UploadKbps = up
			ins.DownloadKbps = down
		}
		cancel()
	}

	if mixed > 0 {
		ctx, cancel := context.WithTimeout(context.Background(), 55*time.Second)
		if ip, err := fetchExitIPViaMixed(ctx, mixed); err != nil {
			ins.LastError = trimErr("Exit IP: " + err.Error())
		} else {
			ins.ExitIP = ip
			gctx, gcancel := context.WithTimeout(context.Background(), 12*time.Second)
			if geo, err := geoForIP(gctx, ip); err == nil {
				if geo.Label != "" {
					ins.ExitLine = geo.Label
				}
				if geo.ISO2 != "" {
					ins.ExitFlagIso2 = geo.ISO2
				}
			}
			gcancel()
		}
		cancel()
	}

	// In rule mode, resolve WAN IP (and geo flag) for comparison with tunnel exit on Home.
	if mode == "rule" {
		ctx, cancel := context.WithTimeout(context.Background(), 14*time.Second)
		if ip, err := fetchIPDirect(ctx); err != nil {
			ins.DirectError = err.Error()
			ins.DirectIP = ""
			ins.DirectFlagIso2 = ""
		} else {
			ins.DirectIP = ip
			ins.DirectError = ""
			ins.DirectFlagIso2 = ""
			gctx, gcancel := context.WithTimeout(context.Background(), 12*time.Second)
			if geo, err := geoForIP(gctx, ip); err == nil && geo.ISO2 != "" {
				ins.DirectFlagIso2 = geo.ISO2
			}
			gcancel()
		}
		cancel()
	} else {
		ins.DirectIP = ""
		ins.DirectError = ""
		ins.DirectFlagIso2 = ""
	}

	a.mu.Lock()
	a.state.Insight = ins
	a.state.UpdatedAt = time.Now().Unix()
	a.mu.Unlock()
	a.emitAppStateChanged()
	return a.GetAppState(), nil
}

func trimErr(s string) string {
	s = strings.TrimSpace(s)
	if len(s) > 220 {
		return s[:220] + "…"
	}
	return s
}

func isIgnorableLatencyError(err error) bool {
	if err == nil {
		return false
	}
	s := strings.ToLower(strings.TrimSpace(err.Error()))
	if strings.Contains(s, "exit status 4") {
		return true
	}
	if strings.Contains(s, "parameters were not valid") {
		return true
	}
	if strings.Contains(s, "unsupported") && strings.Contains(s, "delay") {
		return true
	}
	return false
}

func resolveInsightDelayName(groups []ProxyGroup, activeGroup string) string {
	ag := strings.TrimSpace(activeGroup)
	if ag == "" || len(groups) == 0 {
		return ""
	}
	var g *ProxyGroup
	for i := range groups {
		if groups[i].Name == ag {
			g = &groups[i]
			break
		}
	}
	if g == nil {
		return ""
	}
	if strings.EqualFold(ag, "GLOBAL") {
		subName := strings.TrimSpace(g.Selected)
		if subName == "" {
			return "GLOBAL"
		}
		var sub *ProxyGroup
		for i := range groups {
			if groups[i].Name == subName {
				sub = &groups[i]
				break
			}
		}
		if sub == nil {
			return subName
		}
		leaf := strings.TrimSpace(sub.Selected)
		if leaf != "" {
			return leaf
		}
		return sub.Name
	}
	if leaf := strings.TrimSpace(g.Selected); leaf != "" {
		return leaf
	}
	return g.Name
}

func fetchMihomoDelay(ctx context.Context, listen, secret, proxyName string) (int, error) {
	return fetchMihomoDelayToURL(ctx, listen, secret, proxyName, "http://www.gstatic.com/generate_204")
}

func fetchMihomoDelayToURL(ctx context.Context, listen, secret, proxyName, testURL string) (int, error) {
	u := "/proxies/" + url.PathEscape(proxyName) + "/delay"
	q := url.Values{}
	q.Set("timeout", "4000")
	q.Set("url", testURL)
	u += "?" + q.Encode()

	resp, err := coreDoWithEndpoint(ctx, listen, secret, http.MethodGet, u, nil)
	if err != nil {
		return 0, err
	}
	defer resp.Body.Close()
	b, err := io.ReadAll(io.LimitReader(resp.Body, 4096))
	if err != nil {
		return 0, err
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return 0, fmt.Errorf("delay: HTTP %d %s", resp.StatusCode, strings.TrimSpace(string(b)))
	}
	var out struct {
		Delay int `json:"delay"`
	}
	if err := json.Unmarshal(b, &out); err != nil {
		return 0, err
	}
	if out.Delay <= 0 {
		return 0, fmt.Errorf("no delay value")
	}
	return out.Delay, nil
}

// measureNodeLatencyMs uses a live HTTP probe (generate_204) first so Home matches a real “ping”
// to the test host. History-only values were often stale or inflated compared to what users expect.
func measureNodeLatencyMs(ctx context.Context, listen, secret, proxyName string) (int, error) {
	ms, err := fetchMihomoDelay(ctx, listen, secret, proxyName)
	if err == nil && ms > 0 && ms <= 60000 {
		if ms > 6000 {
			if ms2, err2 := fetchMihomoDelayToURL(ctx, listen, secret, proxyName, "http://cp.cloudflare.com/generate_204"); err2 == nil && ms2 > 0 && ms2 < ms {
				ms = ms2
			}
		}
		return ms, nil
	}
	if ms, ok := fetchProxyLastHistoryDelay(ctx, listen, secret, proxyName); ok && ms > 0 && ms <= 60000 {
		return ms, nil
	}
	if err != nil {
		return 0, err
	}
	return 0, fmt.Errorf("no latency")
}

// minRecentHistoryDelayMS picks the minimum positive delay among the last `window` samples.
// Clash Verge–style panels often feel closer to "best recent ping" than the latest single probe.
func minRecentHistoryDelayMS(delays []int, window int) (int, bool) {
	if len(delays) == 0 || window < 1 {
		return 0, false
	}
	start := len(delays) - window
	if start < 0 {
		start = 0
	}
	best := 0
	for i := start; i < len(delays); i++ {
		d := delays[i]
		if d <= 0 {
			continue
		}
		if best == 0 || d < best {
			best = d
		}
	}
	if best <= 0 {
		return 0, false
	}
	return best, true
}

func delayIntsFromHistoryGeneric(hist []any) []int {
	var out []int
	for _, e := range hist {
		m, ok := e.(map[string]any)
		if !ok {
			continue
		}
		switch v := m["delay"].(type) {
		case float64:
			if v > 0 {
				out = append(out, int(v))
			}
		case int:
			if v > 0 {
				out = append(out, v)
			}
		}
	}
	return out
}

// fetchProxyLastHistoryDelay returns a Verge-like delay (ms) from mihomo proxy history, or ok=false.
func fetchProxyLastHistoryDelay(ctx context.Context, listen, secret, proxyName string) (ms int, ok bool) {
	const recentWindow = 8
	name := strings.TrimSpace(proxyName)
	if name == "" {
		return 0, false
	}
	path := "/proxies/" + url.PathEscape(name)
	resp, err := coreDoWithEndpoint(ctx, listen, secret, http.MethodGet, path, nil)
	if err != nil {
		return 0, false
	}
	defer resp.Body.Close()
	b, err := io.ReadAll(io.LimitReader(resp.Body, 256<<10))
	if err != nil || resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return 0, false
	}
	var detail struct {
		History []struct {
			Delay int `json:"delay"`
		} `json:"history"`
	}
	if err := json.Unmarshal(b, &detail); err == nil && len(detail.History) > 0 {
		ds := make([]int, 0, len(detail.History))
		for _, h := range detail.History {
			if h.Delay > 0 {
				ds = append(ds, h.Delay)
			}
		}
		return minRecentHistoryDelayMS(ds, recentWindow)
	}
	var generic map[string]any
	if err := json.Unmarshal(b, &generic); err != nil {
		return 0, false
	}
	hist, _ := generic["history"].([]any)
	ds := delayIntsFromHistoryGeneric(hist)
	return minRecentHistoryDelayMS(ds, recentWindow)
}

// parseTrafficSnapshot handles a single JSON object or NDJSON lines (mihomo may stream /traffic).
func parseTrafficLine(line []byte) (up int, down int, ok bool) {
	var snap struct {
		Up       int64 `json:"up"`
		Down     int64 `json:"down"`
		Upload   int64 `json:"upload"`
		Download int64 `json:"download"`
	}
	if err := json.Unmarshal(line, &snap); err != nil {
		return 0, 0, false
	}
	u, d := snap.Up, snap.Down
	if u == 0 && d == 0 && (snap.Upload != 0 || snap.Download != 0) {
		u, d = snap.Upload, snap.Download
	}
	return int(u), int(d), true
}

func parseTrafficSnapshot(b []byte) (up int, down int, ok bool) {
	s := strings.TrimSpace(string(b))
	if s == "" {
		return 0, 0, false
	}
	if u, d, ok := parseTrafficLine(b); ok {
		return u, d, true
	}
	var lastU, lastD int
	var found bool
	for _, line := range strings.Split(s, "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		if strings.HasPrefix(line, "data:") {
			line = strings.TrimSpace(strings.TrimPrefix(line, "data:"))
		}
		if u, d, ok := parseTrafficLine([]byte(line)); ok {
			lastU, lastD, found = u, d, true
		}
	}
	if found {
		return lastU, lastD, true
	}
	return 0, 0, false
}

// fetchMihomoTraffic reads GET /traffic (kbps up/down).
// mihomo streams one JSON object per second forever; reading the full body would block until the
// HTTP context deadline. We only consume the first complete line(s) and close the response.
func fetchMihomoTraffic(ctx context.Context, listen, secret string) (up int, down int, err error) {
	resp, err := coreDoWithEndpoint(ctx, listen, secret, http.MethodGet, "/traffic", nil)
	if err != nil {
		return 0, 0, err
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		b, _ := io.ReadAll(io.LimitReader(resp.Body, 2048))
		return 0, 0, fmt.Errorf("traffic: HTTP %d %s", resp.StatusCode, strings.TrimSpace(string(b)))
	}
	br := bufio.NewReader(resp.Body)
	var lastU, lastD int
	var found bool
	// First usable line usually arrives after the server's 1s ticker; a few lines gives a fresher sample.
	for range 8 {
		line, rerr := br.ReadBytes('\n')
		line = bytes.TrimSpace(line)
		if len(line) > 0 {
			if u, d, ok := parseTrafficLine(line); ok {
				lastU, lastD, found = u, d, true
			}
		}
		if rerr != nil {
			if rerr == io.EOF {
				break
			}
			return 0, 0, rerr
		}
		if found {
			break
		}
	}
	if !found {
		return 0, 0, fmt.Errorf("traffic: no parseable snapshot")
	}
	return lastU, lastD, nil
}

// fetchExitIPViaMixed tries HTTP proxy (mixed port), then SOCKS5 on the same port.
// Disables ProxyFromEnvironment so a global HTTP_PROXY cannot break localhost dial (TUN/proxy edge cases).
func fetchExitIPViaMixed(ctx context.Context, mixedPort int) (string, error) {
	var last error
	for attempt := 0; attempt < 3; attempt++ {
		if attempt > 0 {
			select {
			case <-ctx.Done():
				return "", ctx.Err()
			case <-time.After(400 * time.Millisecond):
			}
		}
		if ip, err := fetchIPViaHTTPProxyOnce(ctx, mixedPort); err == nil && ip != "" {
			return ip, nil
		} else {
			last = err
		}
	}
	if ip, err := fetchIPViaSocks5Once(ctx, mixedPort); err == nil && ip != "" {
		return ip, nil
	} else if err != nil {
		last = err
	}
	if last != nil {
		return "", last
	}
	return "", fmt.Errorf("exit ip: no method succeeded")
}

func fetchIPViaHTTPProxyOnce(ctx context.Context, mixedPort int) (string, error) {
	proxyURL, err := url.Parse("http://127.0.0.1:" + strconv.Itoa(mixedPort))
	if err != nil {
		return "", err
	}
	tr := &http.Transport{
		// Fixed proxy only — ignore HTTP_PROXY/HTTPS_PROXY (they break 127.0.0.1 mixed dial).
		Proxy:                 http.ProxyURL(proxyURL),
		TLSHandshakeTimeout:   12 * time.Second,
		ResponseHeaderTimeout: 12 * time.Second,
		ExpectContinueTimeout: 1 * time.Second,
		IdleConnTimeout:       30 * time.Second,
	}
	client := &http.Client{Transport: tr, Timeout: 14 * time.Second}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, "https://api.ipify.org?format=json", nil)
	if err != nil {
		return "", err
	}
	resp, err := client.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	b, err := io.ReadAll(io.LimitReader(resp.Body, 2048))
	if err != nil {
		return "", err
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return "", fmt.Errorf("ipify: HTTP %d", resp.StatusCode)
	}
	var out struct {
		IP string `json:"ip"`
	}
	if err := json.Unmarshal(b, &out); err != nil {
		return "", err
	}
	return strings.TrimSpace(out.IP), nil
}

func fetchIPViaSocks5Once(ctx context.Context, mixedPort int) (string, error) {
	socksDialer, err := proxy.SOCKS5("tcp", net.JoinHostPort("127.0.0.1", strconv.Itoa(mixedPort)), nil, proxy.Direct)
	if err != nil {
		return "", err
	}
	tr := &http.Transport{
		Dial:                  socksDialer.Dial,
		TLSHandshakeTimeout:   12 * time.Second,
		ResponseHeaderTimeout: 12 * time.Second,
		ExpectContinueTimeout: 1 * time.Second,
		IdleConnTimeout:       30 * time.Second,
	}
	client := &http.Client{Transport: tr, Timeout: 14 * time.Second}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, "https://api.ipify.org?format=json", nil)
	if err != nil {
		return "", err
	}
	resp, err := client.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	b, err := io.ReadAll(io.LimitReader(resp.Body, 2048))
	if err != nil {
		return "", err
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return "", fmt.Errorf("ipify: HTTP %d", resp.StatusCode)
	}
	var out struct {
		IP string `json:"ip"`
	}
	if err := json.Unmarshal(b, &out); err != nil {
		return "", err
	}
	return strings.TrimSpace(out.IP), nil
}

func fetchIPDirect(ctx context.Context) (string, error) {
	client := &http.Client{
		Timeout: 10 * time.Second,
		Transport: &http.Transport{
			Proxy: func(*http.Request) (*url.URL, error) { return nil, nil },
		},
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, "https://api.ipify.org?format=json", nil)
	if err != nil {
		return "", err
	}
	resp, err := client.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	b, err := io.ReadAll(io.LimitReader(resp.Body, 2048))
	if err != nil {
		return "", err
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return "", fmt.Errorf("ipify direct: HTTP %d", resp.StatusCode)
	}
	var out struct {
		IP string `json:"ip"`
	}
	if err := json.Unmarshal(b, &out); err != nil {
		return "", err
	}
	return strings.TrimSpace(out.IP), nil
}

// deriveISO2 returns a 2-letter code from country_code, or from country when it is exactly "ru"/"US" etc.
// Some APIs omit country_code but put the ISO in the country field.
func deriveISO2(countryCode, country string) string {
	cc := strings.TrimSpace(countryCode)
	if len(cc) == 2 {
		u := strings.ToUpper(cc)
		if u[0] >= 'A' && u[0] <= 'Z' && u[1] >= 'A' && u[1] <= 'Z' {
			return u
		}
	}
	c := strings.TrimSpace(country)
	if len(c) == 2 {
		u := strings.ToUpper(c)
		if u[0] >= 'A' && u[0] <= 'Z' && u[1] >= 'A' && u[1] <= 'Z' {
			return u
		}
	}
	return ""
}

// cleanCountryName avoids "RU Russia" / duplicate ISO in the label; keep a readable place name only.
func cleanCountryName(country, iso2 string) string {
	country = strings.TrimSpace(country)
	iso2 = strings.ToUpper(strings.TrimSpace(iso2))
	if country == "" {
		return ""
	}
	if len(country) == 2 && iso2 != "" && strings.EqualFold(country, iso2) {
		return ""
	}
	if iso2 != "" && len(country) > len(iso2)+1 &&
		strings.HasPrefix(strings.ToUpper(country), iso2+" ") {
		return strings.TrimSpace(country[len(iso2)+1:])
	}
	if iso2 != "" && strings.EqualFold(country, iso2) {
		return ""
	}
	return country
}

func normalizeGeoToken(s string) string {
	s = strings.TrimSpace(s)
	s = strings.ReplaceAll(s, " - ", " · ")
	return s
}

// geoSnapshot is geo text for the Exit row plus ISO2 for a flag image (emoji flags often render as "RU" on Windows WebView).
type geoSnapshot struct {
	Label string
	ISO2  string
}

func geoForIP(ctx context.Context, ip string) (geoSnapshot, error) {
	var out geoSnapshot
	if ip == "" {
		return out, nil
	}
	u := "https://ipwho.is/" + url.PathEscape(ip)
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u, nil)
	if err != nil {
		return out, err
	}
	client := &http.Client{Timeout: 8 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return out, err
	}
	defer resp.Body.Close()
	b, err := io.ReadAll(io.LimitReader(resp.Body, 8192))
	if err != nil {
		return out, err
	}
	var raw struct {
		Success     bool   `json:"success"`
		CountryCode string `json:"country_code"`
		Country     string `json:"country"`
		City        string `json:"city"`
	}
	if err := json.Unmarshal(b, &raw); err != nil {
		return out, err
	}
	if !raw.Success {
		return out, fmt.Errorf("geo failed")
	}
	iso2 := deriveISO2(raw.CountryCode, raw.Country)
	out.ISO2 = iso2
	co := normalizeGeoToken(cleanCountryName(raw.Country, iso2))
	ci := normalizeGeoToken(strings.TrimSpace(raw.City))
	if co == "" && ci == "" {
		return out, nil
	}
	if co != "" && ci != "" {
		out.Label = co + " · " + ci
		return out, nil
	}
	if co != "" {
		out.Label = co
		return out, nil
	}
	out.Label = ci
	return out, nil
}
