package main

import (
	"context"
	"io"
	"net/http"
	"os"
	"regexp"
	"sort"
	"strings"
	"testing"
	"time"
)

// TestMarshalRuntimeYAMLKeyOrder pins the top-level key sequence of the
// generated config so we never regress to the alphabetical layout where
// secret and sniffer end up *below* rules. The check is structural — we
// only assert that recognised keys appear in the canonical relative order;
// unrecognised keys are free to land anywhere as long as they survive.
func TestMarshalRuntimeYAMLKeyOrder(t *testing.T) {
	in := map[string]any{
		// Intentionally fed in alphabetical-friendly order so a no-op
		// marshal would land exactly on the regression layout.
		"allow-lan":           false,
		"mixed-port":          7890,
		"mode":                "rule",
		"rules":               []string{"MATCH,DIRECT"},
		"secret":              "shh",
		"sniffer":             map[string]any{"enable": true},
		"tun":                 map[string]any{"enable": true},
		"dns":                 map[string]any{"enable": true},
		"proxies":             []any{},
		"geo-update-interval": 72,
		"rule-providers": map[string]any{
			"r1": map[string]any{"type": "http"},
		},
		"unknown-future-key": "kept",
	}
	out, err := marshalRuntimeYAML(in)
	if err != nil {
		t.Fatalf("marshalRuntimeYAML: %v", err)
	}

	// Build the position-of-first-occurrence for each top-level key by
	// scanning lines that start at column zero with `key:`.
	pos := make(map[string]int)
	for i, line := range strings.Split(string(out), "\n") {
		if line == "" || line[0] == ' ' || line[0] == '#' || line[0] == '-' {
			continue
		}
		colon := strings.IndexByte(line, ':')
		if colon <= 0 {
			continue
		}
		key := line[:colon]
		if _, seen := pos[key]; !seen {
			pos[key] = i
		}
	}

	mustBefore := func(a, b string) {
		t.Helper()
		ai, oka := pos[a]
		bi, okb := pos[b]
		if !oka || !okb {
			t.Fatalf("expected both %q (%v) and %q (%v) in output:\n%s", a, oka, b, okb, out)
		}
		if ai >= bi {
			t.Fatalf("expected %q before %q, got pos %d vs %d. Output:\n%s", a, b, ai, bi, out)
		}
	}

	// The exact regression the user reported: secret + sniffer +
	// geo-update-interval must precede rules.
	mustBefore("secret", "rules")
	mustBefore("sniffer", "rules")
	mustBefore("geo-update-interval", "rules")
	// Wider canonical sanity: ports → mode → secret → sniffer → tun → dns → proxies → rules.
	mustBefore("mixed-port", "mode")
	mustBefore("mode", "secret")
	mustBefore("secret", "sniffer")
	mustBefore("sniffer", "tun")
	mustBefore("tun", "dns")
	mustBefore("dns", "proxies")
	mustBefore("proxies", "rule-providers")
	mustBefore("rule-providers", "rules")
	// Unrecognised keys must survive the round-trip.
	if _, ok := pos["unknown-future-key"]; !ok {
		t.Fatalf("unknown key dropped during marshal:\n%s", out)
	}
}

// canonicalUpstreamExcluded lists yaml-tag names that exist in mihomo's
// RawConfig struct but that we deliberately do NOT enumerate in our
// canonicalRuntimeKeyOrder. The keys still pass through marshalRuntimeYAML
// (they end up in the "unknown" bucket after the canon block) — this set
// just suppresses the sync test from yelling about them.
//
// Add a key here ONLY with a one-line reason; otherwise the right action
// is to put it into canonicalRuntimeKeyOrder.
var canonicalUpstreamExcluded = map[string]string{
	// `proxy:` is a pre-1.16 legacy alias for `proxies:` — our subscription
	// normalizer either rewrites or rejects it before reordering.
	"proxy": "legacy alias for proxies; rewritten upstream of reorderer",
}

// TestCanonicalKeyOrderCoversUpstreamMihomo fetches mihomo's RawConfig
// source at the version we ship and asserts that every top-level yaml
// key it declares is covered by canonicalRuntimeKeyOrder (or explicitly
// excluded above). Catches the class of "operator noticed a new mihomo
// field landing below rules" bugs at build time instead of in user
// reports.
//
// The test skips itself (rather than failing the suite) when the network
// is unavailable — required-tests still passes in air-gapped CI, but a
// machine with internet (which is what we use for releases) will see the
// regression immediately.
func TestCanonicalKeyOrderCoversUpstreamMihomo(t *testing.T) {
	if os.Getenv("SLOTH_OFFLINE") == "1" {
		t.Skip("SLOTH_OFFLINE=1 — skipping mihomo upstream key sync check")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()

	version, err := fetchLatestMihomoVersionForTest(ctx)
	if err != nil {
		t.Skipf("could not resolve latest mihomo version (network down or rate-limited): %v", err)
	}
	src, err := fetchMihomoConfigSourceForTest(ctx, version)
	if err != nil {
		t.Skipf("could not fetch mihomo %s config source: %v", version, err)
	}
	upstream := parseRawConfigYAMLKeysForTest(src)
	if len(upstream) == 0 {
		t.Skipf("mihomo %s config.go layout changed: RawConfig parse returned 0 keys (raw source size = %d)", version, len(src))
	}

	canon := make(map[string]bool, len(canonicalRuntimeKeyOrder))
	for _, k := range canonicalRuntimeKeyOrder {
		canon[k] = true
	}
	var missing []string
	for k := range upstream {
		if canon[k] {
			continue
		}
		if _, excluded := canonicalUpstreamExcluded[k]; excluded {
			continue
		}
		missing = append(missing, k)
	}
	sort.Strings(missing)
	if len(missing) > 0 {
		t.Fatalf(`canonicalRuntimeKeyOrder is missing %d top-level key(s) from mihomo %s:
  - %s

These appeared in mihomo's RawConfig
(https://github.com/MetaCubeX/mihomo/blob/%s/config/config.go)
but not in our canonical order list. To fix:
  • If we want the key to live in a specific section of the generated
    YAML, add it to canonicalRuntimeKeyOrder in subscription_yaml.go.
  • If we deliberately do not support it, add it to
    canonicalUpstreamExcluded above with a one-line reason.`,
			len(missing), version,
			strings.Join(missing, "\n  - "),
			version,
		)
	}
}

func fetchLatestMihomoVersionForTest(ctx context.Context) (string, error) {
	req, err := http.NewRequestWithContext(
		ctx,
		http.MethodGet,
		"https://github.com/MetaCubeX/mihomo/releases/latest/download/version.txt",
		nil,
	)
	if err != nil {
		return "", err
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return "", &httpStatusErr{code: resp.StatusCode}
	}
	b, err := io.ReadAll(io.LimitReader(resp.Body, 4096))
	if err != nil {
		return "", err
	}
	v := strings.TrimSpace(string(b))
	if v == "" {
		return "", &emptyBodyErr{}
	}
	return v, nil
}

func fetchMihomoConfigSourceForTest(ctx context.Context, version string) (string, error) {
	url := "https://raw.githubusercontent.com/MetaCubeX/mihomo/" + version + "/config/config.go"
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return "", err
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return "", &httpStatusErr{code: resp.StatusCode}
	}
	b, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if err != nil {
		return "", err
	}
	return string(b), nil
}

// parseRawConfigYAMLKeysForTest extracts yaml-tag names from the body of
// mihomo's RawConfig struct in config/config.go. We deliberately accept a
// best-effort regex rather than parsing Go's AST so the test stays
// resilient to comment changes / struct embeds: if upstream restructures
// drastically, the test logs `0 keys` and skips, surfacing the schema
// drift via the skip message rather than a noisy false positive.
func parseRawConfigYAMLKeysForTest(src string) map[string]bool {
	out := map[string]bool{}
	idx := strings.Index(src, "type RawConfig struct {")
	if idx < 0 {
		return out
	}
	rest := src[idx:]
	end := strings.Index(rest, "\n}")
	if end < 0 {
		return out
	}
	body := rest[:end]
	re := regexp.MustCompile("`[^`]*yaml:\"([^\"]+)\"")
	for _, m := range re.FindAllStringSubmatch(body, -1) {
		// yaml tags can carry options after a comma, e.g. `yaml:"foo,omitempty"`.
		raw := m[1]
		if i := strings.Index(raw, ","); i >= 0 {
			raw = raw[:i]
		}
		raw = strings.TrimSpace(raw)
		if raw == "" || raw == "-" {
			continue
		}
		out[raw] = true
	}
	return out
}

type httpStatusErr struct{ code int }

func (e *httpStatusErr) Error() string {
	return "upstream HTTP " + httpStatusCodeStr(e.code)
}

func httpStatusCodeStr(code int) string {
	const digits = "0123456789"
	if code <= 0 {
		return "0"
	}
	var buf [4]byte
	i := len(buf)
	for code > 0 && i > 0 {
		i--
		buf[i] = digits[code%10]
		code /= 10
	}
	return string(buf[i:])
}

type emptyBodyErr struct{}

func (*emptyBodyErr) Error() string { return "upstream returned empty body" }
