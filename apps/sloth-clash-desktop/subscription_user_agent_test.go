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
)

func TestSubscriptionUserAgentShape(t *testing.T) {
	cases := []struct {
		core, app, want string
	}{
		{"v1.19.32", "0.9.4", "clash.meta/v1.19.32 SlothClash/0.9.4"},
		{"1.19.32", "0.9.4", "clash.meta/v1.19.32 SlothClash/0.9.4"},
		{"", "0.9.4", "clash.meta/mihomo SlothClash/0.9.4"},
		{"  ", "0.9.4", "clash.meta/mihomo SlothClash/0.9.4"},
		{"v1.19.32", "", "clash.meta/v1.19.32 SlothClash/0.0.0"},
	}
	for _, c := range cases {
		if got := subscriptionUserAgent(c.core, c.app); got != c.want {
			t.Fatalf("subscriptionUserAgent(%q,%q) = %q, want %q", c.core, c.app, got, c.want)
		}
	}
}

// The two regexes panels actually use. mikan: core version gate. Marzban /
// Remnawave: format selection by UA prefix.
var (
	mikanCoreVersionRe = regexp.MustCompile(`(mihomo|clash[.-]?meta)/v?(\d+\.\d+\.\d+)`)
	panelClashMetaRe   = regexp.MustCompile(`^(clash-verge|clash[-.]?meta|flclash|mihomo)`)
)

func TestSubscriptionUserAgentSatisfiesPanelRegexes(t *testing.T) {
	withCore := subscriptionUserAgent("v1.19.32", "0.9.4")
	m := mikanCoreVersionRe.FindStringSubmatch(withCore)
	if m == nil || m[2] != "1.19.32" {
		t.Fatalf("mikan regex must extract the core version from %q, got %v", withCore, m)
	}
	if !panelClashMetaRe.MatchString(withCore) {
		t.Fatalf("Marzban/Remnawave prefix regex must match %q", withCore)
	}

	noCore := subscriptionUserAgent("", "0.9.4")
	if mikanCoreVersionRe.MatchString(noCore) {
		t.Fatalf("without a known core version the UA must not claim one: %q", noCore)
	}
	if !panelClashMetaRe.MatchString(noCore) {
		t.Fatalf("format-selecting prefix must survive the no-version form: %q", noCore)
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

	if _, _, err := fetchSubscriptionBody(ctx, srv.URL+"/sub", ""); err != nil {
		t.Fatalf("fetchSubscriptionBody: %v", err)
	}
	if _, err := fetchSubscriptionPeekHeaders(ctx, srv.URL+"/sub", want); err != nil {
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
