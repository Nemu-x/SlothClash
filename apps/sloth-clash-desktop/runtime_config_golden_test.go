package main

import (
	"bytes"
	"flag"
	"fmt"
	"io/fs"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"runtime"
	"sort"
	"strings"
	"sync"
	"testing"

	"SlothClashDesktop/internal/runtimecfg"
	"SlothClashDesktop/internal/runtimecfg/runtimecfgtest"
)

// Golden snapshots of the generated runtime config.
//
// Every profile of the test corpora goes through the production entry point
// (runtimecfg.Write: fetch -> parse -> merge templates ->
// finalize -> marshal -> config.yaml) for both traffic modes and a few key
// desktop-prefs combinations, and the resulting config.yaml is compared byte
// for byte with testdata/golden/runtime-config/. The point is to make any
// change of the generated YAML visible, so the config-generation layer can be
// moved or refactored with proof that its output did not move with it.
//
// Regenerate after an intended change:
//
//	go test -run TestRuntimeConfigGolden -update
//
// and review the golden diff like code.
//
// Inputs are pinned: fixed ports and secret, a fresh data dir per case (so no
// subscription cache and no background refresh), zeroed desktop prefs apart
// from the Connection toggles under test. The output is normalized only where
// the generation is legitimately environment-specific: the data dir and the
// subscription URL become placeholders, and the Windows-only default wintun
// device name is dropped (asserted separately) so one golden set holds on
// every OS.

var updateRuntimeConfigGolden = flag.Bool("update", false, "rewrite testdata/golden/runtime-config from the current pipeline output")

const (
	goldenRuntimeConfigDir = "testdata/golden/runtime-config"
	goldenCtrlPort         = 9090
	goldenMixedPort        = 7890
	goldenSecret           = "golden-secret"
	goldenDataDirToken     = "{{DATA_DIR}}"
	goldenSubURLToken      = "{{SUB_URL}}"
)

type goldenPrefsVariant struct {
	name string
	conn ConnectionSettings
}

// goldenPrefsVariants are the Connection toggles that change the generated
// YAML. "default" is the never-touched state: IPv6 on, smart DNS off, LAN off.
var goldenPrefsVariants = []goldenPrefsVariant{
	{name: "default", conn: ConnectionSettings{}},
	{name: "ipv6-off", conn: ConnectionSettings{DNSIPv6: boolPtr(false)}},
	{name: "smart-dns", conn: ConnectionSettings{SmartDNS: boolPtr(true)}},
	{name: "allow-lan", conn: ConnectionSettings{AllowLan: boolPtr(true)}},
	{name: "lan-smartdns-ipv6off", conn: ConnectionSettings{AllowLan: boolPtr(true), SmartDNS: boolPtr(true), DNSIPv6: boolPtr(false)}},
}

// goldenTrafficModes mirror what Connect passes: tun traffic brings the
// adapter up (enableTun=true), proxy traffic never does.
var goldenTrafficModes = []struct {
	traffic   string
	enableTun bool
}{
	{traffic: "proxy", enableTun: false},
	{traffic: "tun", enableTun: true},
}

type goldenProfile struct {
	id   string // golden subdirectory, slash-separated
	body []byte // subscription body as served
	// fullPrefs runs the whole prefs matrix; otherwise only "default". The
	// Citrus corpus exercises proxy/transport keys the Connection toggles never
	// touch, so the full matrix there would only multiply identical diffs.
	fullPrefs                                bool
	extendTemplate, proxyTemplate, rulesTmpl string
	script                                   string
	// seedGeo pre-creates <dataDir>/geo/{geoip,geosite}.dat, the files the
	// bare fallback writer points geoip/geosite at when present.
	seedGeo bool
}

// goldenScriptedTemplates covers merge_template.go and config_script.go on top
// of a realistic profile: all three declarative templates plus a JS override.
const (
	goldenExtendTemplate = `
prepend:
  rules:
    - DOMAIN-SUFFIX,golden.example,DIRECT
`
	goldenProxyTemplate = `
append:
  proxy-groups:
    - name: Golden
      type: select
      proxies:
        - n1
        - DIRECT
`
	goldenRulesTemplate = `
append:
  rules:
    - DOMAIN,rules.golden.example,Golden
`
	goldenScript = `function main(config) {
  config['unified-delay'] = false;
  config.rules.unshift('DOMAIN,script.golden.example,DIRECT');
  config.tun['dns-hijack'].push('any:5353');
  return config;
}`
)

// goldenBareNodeList is not a full profile, so it takes the bare fallback
// writer in runtimecfg.Write (provider-based config).
var goldenBareNodeList = []byte("proxies:\n  - {name: n1, type: ss, server: s.example.com, port: 443, cipher: aes-128-gcm, password: pw}\n")

func loadGoldenProfiles(t *testing.T) []goldenProfile {
	t.Helper()
	var out []goldenProfile

	readDir := func(prefix, dir string, fullPrefs bool) {
		files, err := filepath.Glob(filepath.Join(dir, "*.yaml"))
		if err != nil {
			t.Fatalf("glob %s: %v", dir, err)
		}
		if len(files) == 0 {
			t.Fatalf("corpus %s is empty", dir)
		}
		sort.Strings(files)
		for _, f := range files {
			b, err := os.ReadFile(f)
			if err != nil {
				t.Fatalf("read %s: %v", f, err)
			}
			name := strings.TrimSuffix(filepath.Base(f), filepath.Ext(f))
			out = append(out, goldenProfile{id: prefix + "/" + name, body: b, fullPrefs: fullPrefs})
		}
	}
	readDir("mihomo", filepath.Join("..", "..", "tests", "corpus", "mihomo", "valid"), true)
	readDir("citrus", filepath.Join("testdata", "citrus"), false)

	rep, err := runtimecfg.MarshalYAML(runtimecfgtest.RepresentativeFullProfile())
	if err != nil {
		t.Fatalf("marshal representative profile: %v", err)
	}
	out = append(out,
		goldenProfile{id: "synthetic/representative", body: rep, fullPrefs: true},
		goldenProfile{
			id:             "synthetic/representative-templates-script",
			body:           rep,
			extendTemplate: goldenExtendTemplate,
			proxyTemplate:  goldenProxyTemplate,
			rulesTmpl:      goldenRulesTemplate,
			script:         goldenScript,
		},
		goldenProfile{id: "synthetic/bare-node-list", body: goldenBareNodeList, fullPrefs: true},
		goldenProfile{id: "synthetic/bare-node-list-geo", body: goldenBareNodeList, seedGeo: true},
	)
	return out
}

// goldenSubscriptionServer serves one body at a time; cases run sequentially.
type goldenSubscriptionServer struct {
	mu   sync.Mutex
	body []byte
	srv  *httptest.Server
}

func newGoldenSubscriptionServer(t *testing.T) *goldenSubscriptionServer {
	t.Helper()
	g := &goldenSubscriptionServer{}
	g.srv = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		g.mu.Lock()
		b := g.body
		g.mu.Unlock()
		w.Header().Set("Content-Type", "text/yaml; charset=utf-8")
		_, _ = w.Write(b)
	}))
	t.Cleanup(g.srv.Close)
	return g
}

// serve returns the same URL for every case. That is only sound because each
// case gets a fresh data dir and the pipeline keeps no URL-keyed in-process
// subscription cache: every case really refetches its own body. If such a
// cache is ever added, make the URL vary per case (e.g. a per-case path or
// query) or later cases will silently be generated from an earlier body.
func (g *goldenSubscriptionServer) serve(b []byte) string {
	g.mu.Lock()
	g.body = b
	g.mu.Unlock()
	return g.srv.URL + "/sub"
}

// TestRuntimeConfigGolden must not run in parallel: it swaps the global desktop
// prefs, which the pipeline reads. Parallel tests are only released after all
// sequential top-level tests finish, so none of them can observe the swap.
func TestRuntimeConfigGolden(t *testing.T) {
	profiles := loadGoldenProfiles(t)
	server := newGoldenSubscriptionServer(t)

	expected := map[string]bool{}
	for _, p := range profiles {
		variants := goldenPrefsVariants
		if !p.fullPrefs {
			variants = goldenPrefsVariants[:1]
		}
		for _, mode := range goldenTrafficModes {
			for _, v := range variants {
				rel := p.id + "/" + mode.traffic + "-" + v.name + ".yaml"
				expected[rel] = true
				t.Run(rel, func(t *testing.T) {
					withConnectionPrefs(t, v.conn)
					got := generateGoldenRuntimeConfig(t, server, p, mode.traffic, mode.enableTun)
					compareRuntimeConfigGolden(t, rel, got)
				})
			}
		}
	}
	checkStaleRuntimeConfigGoldens(t, expected)
}

func generateGoldenRuntimeConfig(t *testing.T, server *goldenSubscriptionServer, p goldenProfile, traffic string, enableTun bool) []byte {
	t.Helper()
	dataDir := t.TempDir()
	if p.seedGeo {
		geoDir := filepath.Join(dataDir, "geo")
		if err := os.MkdirAll(geoDir, 0o755); err != nil {
			t.Fatal(err)
		}
		for _, n := range []string{"geoip.dat", "geosite.dat"} {
			if err := os.WriteFile(filepath.Join(geoDir, n), []byte("golden"), 0o644); err != nil {
				t.Fatal(err)
			}
		}
	}
	subURL := server.serve(p.body)
	var res runtimecfg.ScriptResult
	if err := runtimecfg.Write(runtimecfg.Input{
		DataDir:                dataDir,
		SubURL:                 subURL,
		ExtendTemplate:         p.extendTemplate,
		ProxyTemplate:          p.proxyTemplate,
		RulesTemplate:          p.rulesTmpl,
		Script:                 p.script,
		CtrlPort:               goldenCtrlPort,
		MixedPort:              goldenMixedPort,
		Secret:                 goldenSecret,
		WithExternalController: true,
		Traffic:                traffic,
		EnableTun:              enableTun,
		Env:                    currentRuntimeConfigEnv(),
	}, &res); err != nil {
		t.Fatalf("runtimecfg.Write: %v", err)
	}
	if p.script != "" && !res.Applied {
		t.Fatalf("script was not applied: %+v", res)
	}
	raw, err := os.ReadFile(filepath.Join(dataDir, "config.yaml"))
	if err != nil {
		t.Fatalf("read generated config: %v", err)
	}
	return normalizeGoldenRuntimeConfig(t, raw, dataDir, server.srv.URL)
}

// normalizeGoldenRuntimeConfig replaces the environment-specific parts of the
// output with stable tokens and fails if anything host-specific survives.
func normalizeGoldenRuntimeConfig(t *testing.T, raw []byte, dataDir, serverURL string) []byte {
	t.Helper()
	s := strings.ReplaceAll(string(raw), "\r\n", "\n")
	for _, form := range []string{
		strings.ReplaceAll(dataDir, `\`, `\\`), // as a double-quoted YAML scalar
		dataDir,
		filepath.ToSlash(dataDir),
	} {
		s = strings.ReplaceAll(s, form, goldenDataDirToken)
	}
	s = strings.ReplaceAll(s, serverURL, goldenSubURLToken)

	// Windows-only default (ensureTunOverlayForTraffic): the wintun adapter is
	// named runtimecfg.TunWindowsDeviceName unless the profile names one. Drop exactly
	// that line, and only as a direct key of the top-level tun block, so the
	// golden is OS-independent; but insist it was there.
	if runtime.GOOS == "windows" {
		if start, end, ok := topLevelTunBlock(s); ok {
			line := "    device: " + runtimecfg.TunWindowsDeviceName + "\n"
			body := s[start:end]
			i := -1
			if strings.HasPrefix(body, line) {
				i = 0
			} else if j := strings.Index(body, "\n"+line); j >= 0 {
				i = j + 1
			}
			switch {
			case i >= 0:
				s = s[:start+i] + s[start+i+len(line):]
			case !tunBlockHasDevice(s):
				t.Fatalf("windows: tun block without the default %q device", runtimecfg.TunWindowsDeviceName)
			}
		}
	}

	for _, leak := range []string{
		filepath.ToSlash(os.TempDir()),
		os.TempDir(),
		filepath.Base(filepath.Dir(dataDir)), // per-test temp dir name
		strings.TrimPrefix(serverURL, "http://"),
	} {
		if leak != "" && strings.Contains(s, leak) {
			t.Fatalf("normalized config still contains host-specific %q", leak)
		}
	}
	return []byte(s)
}

// topLevelTunBlock returns the byte span [start, end) of the body of the
// top-level tun mapping: the lines after "tun:" up to the next top-level key.
func topLevelTunBlock(s string) (start, end int, ok bool) {
	const header = "tun:\n"
	if strings.HasPrefix(s, header) {
		start = len(header)
	} else {
		i := strings.Index(s, "\n"+header)
		if i < 0 {
			return 0, 0, false
		}
		start = i + 1 + len(header)
	}
	end = start
	for end < len(s) {
		line, _, _ := strings.Cut(s[end:], "\n")
		if line != "" && !strings.HasPrefix(line, " ") {
			break // next top-level key
		}
		end += len(line)
		if end < len(s) {
			end++ // the newline
		}
	}
	return start, end, true
}

// tunBlockHasDevice reports whether the top-level tun mapping carries any
// device key (a profile- or user-chosen name, which production keeps).
func tunBlockHasDevice(s string) bool {
	start, end, ok := topLevelTunBlock(s)
	if !ok {
		return false
	}
	for _, line := range strings.Split(s[start:end], "\n") {
		if strings.HasPrefix(line, "    device:") {
			return true
		}
	}
	return false
}

func compareRuntimeConfigGolden(t *testing.T, rel string, got []byte) {
	t.Helper()
	path := filepath.Join(goldenRuntimeConfigDir, filepath.FromSlash(rel))
	if *updateRuntimeConfigGolden {
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, got, 0o644); err != nil {
			t.Fatal(err)
		}
		return
	}
	want, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("missing golden %s (run: go test -run TestRuntimeConfigGolden -update): %v", path, err)
	}
	want = bytes.ReplaceAll(want, []byte("\r\n"), []byte("\n"))
	if !bytes.Equal(want, got) {
		t.Errorf("runtime config differs from golden %s\n(run: go test -run TestRuntimeConfigGolden -update, then review the diff)\n%s",
			filepath.ToSlash(path), unifiedLineDiff("golden/"+rel, "generated/"+rel, string(want), string(got), 3))
	}
}

// checkStaleRuntimeConfigGoldens fails on goldens no case produces any more
// (a removed profile or variant), and deletes them under -update.
func checkStaleRuntimeConfigGoldens(t *testing.T, expected map[string]bool) {
	t.Helper()
	_ = filepath.WalkDir(goldenRuntimeConfigDir, func(p string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return nil
		}
		rel, rerr := filepath.Rel(goldenRuntimeConfigDir, p)
		if rerr != nil {
			return nil
		}
		rel = filepath.ToSlash(rel)
		if expected[rel] {
			return nil
		}
		if *updateRuntimeConfigGolden {
			if err := os.Remove(p); err != nil {
				t.Errorf("remove stale golden %s: %v", p, err)
			}
			return nil
		}
		t.Errorf("stale golden %s: no case generates it (run with -update to delete)", filepath.ToSlash(p))
		return nil
	})
}

// unifiedLineDiff renders a unified diff of two texts (LCS over lines). The
// configs are a few hundred lines, so the quadratic table is cheap.
func unifiedLineDiff(fromName, toName, a, b string, context int) string {
	al := strings.SplitAfter(a, "\n")
	bl := strings.SplitAfter(b, "\n")
	if len(al) > 0 && al[len(al)-1] == "" {
		al = al[:len(al)-1]
	}
	if len(bl) > 0 && bl[len(bl)-1] == "" {
		bl = bl[:len(bl)-1]
	}
	n, m := len(al), len(bl)
	lcs := make([][]int, n+1)
	for i := range lcs {
		lcs[i] = make([]int, m+1)
	}
	for i := n - 1; i >= 0; i-- {
		for j := m - 1; j >= 0; j-- {
			if al[i] == bl[j] {
				lcs[i][j] = lcs[i+1][j+1] + 1
			} else {
				lcs[i][j] = max(lcs[i+1][j], lcs[i][j+1])
			}
		}
	}
	type op struct {
		kind byte // ' ', '-', '+'
		text string
		ai   int // line index in a (for ' ' and '-')
		bi   int // line index in b (for ' ' and '+')
	}
	var ops []op
	i, j := 0, 0
	for i < n || j < m {
		switch {
		case i < n && j < m && al[i] == bl[j]:
			ops = append(ops, op{' ', al[i], i, j})
			i++
			j++
		case i < n && (j == m || lcs[i+1][j] >= lcs[i][j+1]):
			ops = append(ops, op{'-', al[i], i, j})
			i++
		default:
			ops = append(ops, op{'+', bl[j], i, j})
			j++
		}
	}

	var sb strings.Builder
	fmt.Fprintf(&sb, "--- %s\n+++ %s\n", fromName, toName)
	for k := 0; k < len(ops); {
		if ops[k].kind == ' ' {
			k++
			continue
		}
		start := max(k-context, 0)
		end := k
		for end < len(ops) {
			if ops[end].kind != ' ' {
				end++
				continue
			}
			run := end
			for run < len(ops) && ops[run].kind == ' ' {
				run++
			}
			if run == len(ops) || run-end > 2*context {
				end = min(end+context, len(ops))
				break
			}
			end = run
		}
		aStart, bStart, aLen, bLen := ops[start].ai, ops[start].bi, 0, 0
		for _, o := range ops[start:end] {
			if o.kind != '+' {
				aLen++
			}
			if o.kind != '-' {
				bLen++
			}
		}
		fmt.Fprintf(&sb, "@@ -%d,%d +%d,%d @@\n", aStart+1, aLen, bStart+1, bLen)
		for _, o := range ops[start:end] {
			sb.WriteByte(o.kind)
			sb.WriteString(o.text)
			if !strings.HasSuffix(o.text, "\n") {
				sb.WriteString("\n\\ No newline at end of file\n")
			}
		}
		k = end
	}
	return sb.String()
}
