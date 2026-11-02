package main

import (
	"context"
	"io/fs"
	"net/http"
	"net/http/httptest"
	"os"
	"regexp"
	"strings"
	"testing"
	"testing/fstest"
	"time"

	"SlothClashDesktop/internal/runtimecfg"
)

func TestSubscriptionUserAgentShape(t *testing.T) {
	cases := []struct {
		core, citrus, app, want string
	}{
		{"v1.19.33", "v1.0.0-beta.2", "0.10.1", "clash.meta/v1.19.33 citrus/v1.0.0-beta.2 SlothClash/0.10.1"},
		{"1.19.33", " v1.0.0 ", "0.10.1", "clash.meta/v1.19.33 citrus/v1.0.0 SlothClash/0.10.1"},
		{"v1.19.32", "", "0.9.4", "clash.meta/v1.19.32 SlothClash/0.9.4"},
		{"1.19.32", "", "0.9.4", "clash.meta/v1.19.32 SlothClash/0.9.4"},
		{"", "", "0.9.4", "clash.meta/mihomo SlothClash/0.9.4"},
		{"  ", "", "0.9.4", "clash.meta/mihomo SlothClash/0.9.4"},
		{"v1.19.32", "", "", "clash.meta/v1.19.32 SlothClash/0.0.0"},
		{"", "v1.0.0-beta.2", "0.10.1", "clash.meta/mihomo citrus/v1.0.0-beta.2 SlothClash/0.10.1"},
	}
	for _, c := range cases {
		if got := subscriptionUserAgent(c.core, c.citrus, c.app); got != c.want {
			t.Fatalf("subscriptionUserAgent(%q,%q,%q) = %q, want %q", c.core, c.citrus, c.app, got, c.want)
		}
	}
}

// A Citrus release that is not a well-formed tag never reaches the header:
// the token is dropped, never guessed or half-sent.
func TestSubscriptionUserAgentDropsMalformedCitrusRelease(t *testing.T) {
	const want = "clash.meta/v1.19.33 SlothClash/0.10.1"
	for _, bad := range []string{
		"1.0.0-beta.2", "v1.0", "v1.0.0-", "v1.0.0-beta..2", "v1.0.0 beta", "v1.0.0\r\nX-Evil: 1",
		"v1.0.0-beta.2;", "latest", "dev", "v1.0.0+build.1",
	} {
		if got := subscriptionUserAgent("v1.19.33", bad, "0.10.1"); got != want {
			t.Errorf("subscriptionUserAgent(citrus=%q) = %q, want %q", bad, got, want)
		}
	}
}

// The regexes panels actually use. mikan: core version gate (exactly as in its
// internal/sub/apps) and app catalog word. Marzban / Remnawave: format
// selection by UA prefix. Citrus panels: the citrus product token.
var (
	mikanCoreVersionRe = regexp.MustCompile(`(?:mihomo|clash[.-]?meta)/v?(\d+)\.(\d+)\.(\d+)`)
	panelClashMetaRe   = regexp.MustCompile(`^(clash-verge|clash[-.]?meta|flclash|mihomo)`)
	citrusPanelRe      = regexp.MustCompile(`\bcitrus/(\S+)`)
)

func TestSubscriptionUserAgentSatisfiesPanelRegexes(t *testing.T) {
	for _, citrus := range []string{"v1.0.0-beta.2", ""} {
		withCore := subscriptionUserAgent("v1.19.33", citrus, "0.10.1")
		m := mikanCoreVersionRe.FindStringSubmatch(withCore)
		if m == nil || m[1]+"."+m[2]+"."+m[3] != "1.19.33" {
			t.Fatalf("mikan regex must extract the core version from %q, got %v", withCore, m)
		}
		if !strings.Contains(strings.ToLower(withCore), "slothclash") {
			t.Fatalf("mikan catalog word slothclash must match %q", withCore)
		}
		if !panelClashMetaRe.MatchString(withCore) {
			t.Fatalf("Marzban/Remnawave prefix regex must match %q", withCore)
		}
		c := citrusPanelRe.FindStringSubmatch(withCore)
		switch {
		case citrus != "" && (c == nil || c[1] != citrus):
			t.Fatalf("Citrus panel regex must extract %q from %q, got %v", citrus, withCore, c)
		case citrus == "" && c != nil:
			t.Fatalf("without a known Citrus release the UA must not claim one: %q", withCore)
		}
	}

	noCore := subscriptionUserAgent("", "", "0.9.4")
	if mikanCoreVersionRe.MatchString(noCore) {
		t.Fatalf("without a known core version the UA must not claim one: %q", noCore)
	}
	if !panelClashMetaRe.MatchString(noCore) {
		t.Fatalf("format-selecting prefix must survive the no-version form: %q", noCore)
	}
}

// The UA of this build carries no feature comment and only a well-formed
// citrus token.
func TestSubscriptionUserAgentCurrentHasNoFeatureComment(t *testing.T) {
	ua := subscriptionUserAgentCurrent()
	if strings.ContainsAny(ua, "();") {
		t.Fatalf("UA must not carry a comment: %q", ua)
	}
	if c := citrusPanelRe.FindStringSubmatch(ua); c != nil && validCitrusRelease(c[1]) != c[1] {
		t.Fatalf("UA carries a malformed citrus token: %q", ua)
	}
}

func TestSubscriptionProbeUserAgentsStartWithTheRealOne(t *testing.T) {
	list := subscriptionProbeUserAgents()
	if len(list) < 2 {
		t.Fatalf("expected the real UA plus fallbacks, got %v", list)
	}
	if list[0] != subscriptionUserAgentCurrent() {
		t.Fatalf("first probe UA must be the real one: %q", list[0])
	}
	for _, ua := range list {
		if strings.Contains(ua, "SlothClash/1.0") {
			t.Fatalf("hard-coded SlothClash/1.0 must be gone: %q", ua)
		}
	}
	if !strings.Contains(list[len(list)-1], "SlothClash/"+AppVersion) {
		t.Fatalf("compat fallback must carry the real app version: %q", list[len(list)-1])
	}
}

func TestCoreVersionFromFS(t *testing.T) {
	mk := func(content string) fs.FS {
		return fstest.MapFS{embeddedCoreVersionFile: &fstest.MapFile{Data: []byte(content)}}
	}
	cases := map[string]string{
		"v1.19.32\n":      "v1.19.32",
		"1.19.32":         "v1.19.32",
		"  v1.19.32\r\n":  "v1.19.32",
		"v1.19.32\nextra": "v1.19.32",
		"":                "",
		"latest":          "",
		"v1.19":           "",
		"v1.19.32-alpha":  "",
	}
	for content, want := range cases {
		if got := coreVersionFromFS(mk(content)); got != want {
			t.Fatalf("coreVersionFromFS(%q) = %q, want %q", content, got, want)
		}
	}
	if got := coreVersionFromFS(fstest.MapFS{}); got != "" {
		t.Fatalf("missing file must yield empty, got %q", got)
	}
	if got := coreVersionFromFS(nil); got != "" {
		t.Fatalf("nil FS must yield empty, got %q", got)
	}
}

// A build that ships a core must also ship its version: otherwise every
// subscription request silently falls back to the versionless UA and panels
// withhold protocols the core actually supports.
func TestEmbeddedCoreVersionPresentWhenCoreIsBundled(t *testing.T) {
	cores, _ := fs.Glob(bundledResources, "build/sidecar/sloth-mihomo*")
	if len(cores) == 0 {
		t.Skip("no sidecar core embedded in this test binary")
	}
	v := embeddedCoreVersion()
	if v == "" {
		t.Fatalf("core is bundled (%v) but %s is missing or invalid — run: pnpm run prebuild --force", cores, embeddedCoreVersionFile)
	}
	if _, err := os.Stat(embeddedCoreVersionFile); err != nil {
		t.Fatalf("%s should exist on disk next to the sidecar: %v", embeddedCoreVersionFile, err)
	}
}

// End-to-end: the header that actually leaves the process on a subscription
// GET is exactly the one we claim, for both fetch paths (full body + peek).
func TestSubscriptionRequestsSendTheExactUserAgent(t *testing.T) {
	want := subscriptionUserAgentCurrent()
	var seen []string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		seen = append(seen, r.Header.Get("User-Agent"))
		w.Header().Set("Content-Type", "text/yaml")
		_, _ = w.Write([]byte("proxies: []\nproxy-groups: []\nrules: []\n"))
	}))
	defer srv.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	if _, _, err := runtimecfg.FetchSubscriptionBody(ctx, srv.URL+"/sub", "", currentSubscriptionClient()); err != nil {
		t.Fatalf("fetchSubscriptionBody: %v", err)
	}
	if _, err := fetchSubscriptionPeekHeaders(ctx, srv.URL+"/sub", want, currentSubscriptionClient()); err != nil {
		t.Fatalf("fetchSubscriptionPeekHeaders: %v", err)
	}
	if len(seen) != 2 {
		t.Fatalf("expected 2 requests, got %d", len(seen))
	}
	for i, ua := range seen {
		if ua != want {
			t.Fatalf("request %d sent User-Agent %q, want %q", i, ua, want)
		}
	}
}
