package main

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"slices"
	"sort"
	"strconv"
	"strings"
	"sync"
	"testing"

	"gopkg.in/yaml.v3"
)

// Citrus compatibility suite: client-side example configs of getmikan/Citrus
// (docs/config.yaml, the site docs, the interop tests) go through the app's
// config pipeline and then the real core (`mihomo -t`, the preflight every
// Connect runs), so the gap between what a Citrus-targeted subscription may
// contain and what SlothClash accepts is a test result instead of a guess.
//
// Fixtures and testdata/citrus/manifest.json are ClashFest's (same patch
// series; its numbering is ours minus one from 0004 on, see
// docs/core-patches.md). Outcomes per case:
//
//   - "accept": the pipeline keeps every Citrus-only key the case declares and
//     the core accepts the config and reads every one of them (minus the ones
//     listed as harmless).
//   - "unsupported:<reason>" / "deferred:<reason>": not taken; the case must
//     still show its gap (a rejection or an ignored key), or the manifest is
//     stale and the test fails.
//
// "Silently ignored" is measured: every declared key is replaced by a mapping
// and then by a sequence; a key the core decodes fails for at least one of
// them, a key it skips passes both. Client fingerprints are resolved by the
// core only when dialing, so the Xray fingerprint names are not probed here;
// patch 0008 tests its whole name table on every core build.
//
// Needs a core with the series: SLOTH_MIHOMO_BIN, or the sidecar from
// prebuild. Skips otherwise.

type citrusManifest struct {
	Citrus   string          `json:"citrus"`
	Fixtures []citrusFixture `json:"fixtures"`
}

type citrusFixture struct {
	File  string                `json:"file"`
	Split string                `json:"split,omitempty"` // "", "proxies" or "rules"
	Cases map[string]citrusCase `json:"cases"`           // "" for an unsplit fixture
}

type citrusCase struct {
	Outcome    string   `json:"outcome"`
	Note       string   `json:"note,omitempty"`
	CitrusOnly []string `json:"citrus-only,omitempty"` // key paths, relative to the proxy for split proxies
	Harmless   []string `json:"harmless,omitempty"`    // ignored keys vanilla handles the same way
}

type citrusResult struct {
	id, outcome, engineErr string
	dropped, ignored       []string
}

// citrusJob is one case after the pipeline, ready for the core.
type citrusJob struct {
	id    string
	split string
	want  citrusCase
	doc   map[string]any // pipeline output
	res   citrusResult
}

func TestCitrusCompat(t *testing.T) {
	bin := testCoreBinaryForPassthrough()
	if bin == "" {
		t.Skip("no core binary (set SLOTH_MIHOMO_BIN or run prebuild)")
	}
	if err := citrusCoreHasSeries(t, bin); err != nil {
		t.Skipf("core %s does not carry the Citrus series (stock or older build?): %v", bin, err)
	}
	withConnectionPrefs(t, ConnectionSettings{})

	dir := filepath.Join("testdata", "citrus")
	raw, err := os.ReadFile(filepath.Join(dir, "manifest.json"))
	if err != nil {
		t.Fatal(err)
	}
	var m citrusManifest
	if err := json.Unmarshal(raw, &m); err != nil {
		t.Fatalf("manifest.json: %v", err)
	}
	listed := map[string]bool{}
	for _, f := range m.Fixtures {
		listed[f.File] = true
	}
	files, _ := filepath.Glob(filepath.Join(dir, "*.yaml"))
	for _, f := range files {
		if !listed[filepath.Base(f)] {
			t.Errorf("%s is not in manifest.json", filepath.Base(f))
		}
	}

	// The pipeline runs here, one case at a time (it reads the desktop prefs);
	// the core runs below, in parallel.
	var jobs []*citrusJob
	for _, f := range m.Fixtures {
		data, err := os.ReadFile(filepath.Join(dir, f.File))
		if err != nil {
			t.Errorf("%s: %v", f.File, err)
			continue
		}
		cases, err := citrusExpand(data, f.Split)
		if err != nil {
			t.Errorf("%s: %v", f.File, err)
			continue
		}
		for name := range f.Cases {
			if _, ok := cases[name]; !ok {
				t.Errorf("%s: manifest case %q is not in the fixture", f.File, name)
			}
		}
		names := make([]string, 0, len(cases))
		for name := range cases {
			names = append(names, name)
		}
		sort.Strings(names)
		for _, name := range names {
			id := f.File
			if name != "" {
				id += "#" + name
			}
			want, ok := f.Cases[name]
			if !ok {
				t.Errorf("%s: no manifest entry", id)
				continue
			}
			jobs = append(jobs, citrusPipeline(t, id, f.Split, want, cases[name]))
		}
	}

	citrusRunCore(t, bin, jobs)

	counts := map[string]int{}
	for _, j := range jobs {
		citrusCheck(t, j.res, j.want)
		kind, _, _ := strings.Cut(j.want.Outcome, ":")
		counts[kind]++
		line := fmt.Sprintf("%-58s %s", j.id, j.want.Outcome)
		if j.res.engineErr != "" {
			line += " | rejected: " + j.res.engineErr
		}
		if len(j.res.dropped) > 0 {
			line += " | dropped by pipeline: " + strings.Join(j.res.dropped, ", ")
		}
		if len(j.res.ignored) > 0 {
			line += " | ignored: " + strings.Join(j.res.ignored, ", ")
		}
		t.Log(line)
	}
	t.Logf("Citrus@%s: %d cases, %d accept, %d unsupported, %d deferred",
		m.Citrus, len(jobs), counts["accept"], counts["unsupported"], counts["deferred"])
}

// citrusCoreHasSeries checks the core knows a chain group (core patch 0006):
// a stock core rejects the type.
func citrusCoreHasSeries(t *testing.T, bin string) error {
	t.Helper()
	dir := t.TempDir()
	cfg := "proxies:\n- {name: a, type: ss, server: 192.0.2.1, port: 443, cipher: chacha20-ietf-poly1305, password: p}\n" +
		"- {name: b, type: ss, server: 192.0.2.2, port: 443, cipher: chacha20-ietf-poly1305, password: p}\n" +
		"proxy-groups:\n- {name: c, type: chain, proxies: [a, b]}\nrules:\n- MATCH,c\n"
	if err := os.WriteFile(filepath.Join(dir, "config.yaml"), []byte(cfg), 0o644); err != nil {
		t.Fatal(err)
	}
	return runConfigPreflight(bin, dir)
}

// citrusPipeline runs a case through the app's config pipeline and records
// Citrus-only keys the pipeline lost.
func citrusPipeline(t *testing.T, id, split string, want citrusCase, doc map[string]any) *citrusJob {
	t.Helper()
	j := &citrusJob{id: id, split: split, want: want, res: citrusResult{id: id, outcome: want.Outcome}}
	for _, path := range want.CitrusOnly {
		if !citrusHas(doc, citrusFullPath(split, path)) {
			t.Errorf("%s: citrus-only key %q is not in the case", id, path)
		}
	}
	out := citrusClone(doc)
	if err := finalizeRuntimeConfigPipeline(out, t.TempDir(), 7890, 9090, "secret", "proxy", false, false); err != nil {
		j.res.engineErr = "pipeline: " + err.Error()
		return j
	}
	for _, path := range want.CitrusOnly {
		if citrusHas(doc, citrusFullPath(split, path)) && !citrusHas(out, citrusFullPath(split, path)) {
			j.res.dropped = append(j.res.dropped, path)
		}
	}
	j.doc = out
	return j
}

// citrusRunCore validates every case and every probe with the core, in parallel.
func citrusRunCore(t *testing.T, bin string, jobs []*citrusJob) {
	t.Helper()
	type task struct {
		job   *citrusJob
		probe int    // -1 = the case itself
		path  string // probed key
		doc   map[string]any
	}
	validate := func(doc map[string]any) string {
		dir, err := os.MkdirTemp("", "sloth-citrus-*")
		if err != nil {
			return "tempdir: " + err.Error()
		}
		defer os.RemoveAll(dir)
		b, err := marshalRuntimeYAML(doc)
		if err != nil {
			return "marshal: " + err.Error()
		}
		if err := os.WriteFile(filepath.Join(dir, "config.yaml"), b, 0o644); err != nil {
			return "write: " + err.Error()
		}
		if err := runConfigPreflight(bin, dir); err != nil {
			return citrusShortError(err.Error())
		}
		return ""
	}
	parallel := func(tasks []task, fn func(task, string)) {
		ch := make(chan task)
		var wg sync.WaitGroup
		var mu sync.Mutex
		for i := 0; i < max(2, runtime.NumCPU()/2); i++ {
			wg.Add(1)
			go func() {
				defer wg.Done()
				for tk := range ch {
					res := validate(tk.doc)
					mu.Lock()
					fn(tk, res)
					mu.Unlock()
				}
			}()
		}
		for _, tk := range tasks {
			ch <- tk
		}
		close(ch)
		wg.Wait()
	}

	var cases []task
	for _, j := range jobs {
		if j.doc != nil {
			cases = append(cases, task{job: j, probe: -1, doc: j.doc})
		}
	}
	parallel(cases, func(tk task, res string) { tk.job.res.engineErr = res })

	// Probe the declared keys of every case the core accepted.
	var probes []task
	consumed := map[*citrusJob]map[string]bool{}
	for _, j := range jobs {
		if j.doc == nil || j.res.engineErr != "" {
			continue
		}
		consumed[j] = map[string]bool{}
		for _, path := range j.want.CitrusOnly {
			full := citrusFullPath(j.split, path)
			if !citrusHas(j.doc, full) {
				continue // already reported as dropped by the pipeline
			}
			for i, v := range []any{
				map[string]any{"citrus-compat-probe": []any{1, 2}},
				[]any{"citrus-compat-probe", 1},
			} {
				c := citrusClone(j.doc)
				citrusSet(c, full, v)
				probes = append(probes, task{job: j, probe: i, path: path, doc: c})
			}
		}
	}
	parallel(probes, func(tk task, res string) {
		if res != "" {
			consumed[tk.job][tk.path] = true
		}
	})
	for _, j := range jobs {
		if j.doc == nil || j.res.engineErr != "" {
			continue
		}
		for _, path := range j.want.CitrusOnly {
			if citrusHas(j.doc, citrusFullPath(j.split, path)) && !consumed[j][path] {
				j.res.ignored = append(j.res.ignored, path)
			}
		}
	}
}

// citrusShortError picks the core's own error line out of the preflight output.
func citrusShortError(s string) string {
	lines := strings.Split(strings.TrimPrefix(s, "configuration preflight failed: "), "\n")
	for _, l := range lines {
		if i := strings.Index(l, `level=error msg="`); i >= 0 {
			return strings.TrimSuffix(strings.TrimSpace(l[i+len(`level=error msg="`):]), `"`)
		}
	}
	for _, l := range lines {
		if strings.Contains(strings.ToLower(l), "parse config error") {
			return strings.TrimSpace(l)
		}
	}
	return strings.TrimSpace(lines[len(lines)-1])
}

func citrusCheck(t *testing.T, r citrusResult, want citrusCase) {
	t.Helper()
	harmless := map[string]bool{}
	for _, h := range want.Harmless {
		harmless[h] = true
	}
	var gaps []string
	for _, ig := range r.ignored {
		if !harmless[ig] {
			gaps = append(gaps, ig)
		}
	}
	for _, h := range want.Harmless {
		// Fingerprint entries are not probed here (see the file comment).
		if strings.HasPrefix(h, "client-fingerprint: ") {
			continue
		}
		if r.engineErr == "" && !slices.Contains(r.ignored, h) {
			t.Errorf("%s: %q is listed as harmless but the core reads it now", r.id, h)
		}
	}
	kind, reason, _ := strings.Cut(want.Outcome, ":")
	switch kind {
	case "accept":
		if r.engineErr != "" {
			t.Errorf("%s: rejected: %s", r.id, r.engineErr)
		}
		if len(r.dropped) > 0 {
			t.Errorf("%s: the app pipeline dropped %v", r.id, r.dropped)
		}
		if len(gaps) > 0 {
			t.Errorf("%s: accepted but silently ignored: %v", r.id, gaps)
		}
	case "unsupported", "deferred":
		if reason == "" {
			t.Errorf("%s: outcome %q needs a reason", r.id, want.Outcome)
		}
		if r.engineErr == "" && len(gaps) == 0 && len(r.dropped) == 0 {
			t.Errorf("%s: marked %s but the core accepts it and reads every key: update manifest.json", r.id, want.Outcome)
		}
	default:
		t.Errorf("%s: unknown outcome %q", r.id, want.Outcome)
	}
}

// citrusExpand turns a fixture into its cases: the whole document, or one case
// per proxy / per rule.
func citrusExpand(data []byte, split string) (map[string]map[string]any, error) {
	doc := map[string]any{}
	if err := yaml.Unmarshal(data, &doc); err != nil {
		return nil, err
	}
	switch split {
	case "":
		return map[string]map[string]any{"": doc}, nil
	case "proxies":
		list, _ := doc["proxies"].([]any)
		byName := map[string]any{}
		for _, p := range list {
			byName[citrusName(p)] = p
		}
		cases := map[string]map[string]any{}
		for _, p := range list {
			name := citrusName(p)
			if _, dup := cases[name]; dup || name == "" {
				return nil, fmt.Errorf("proxy name %q missing or repeated", name)
			}
			c := citrusClone(doc)
			// the proxy, the proxies its dialer-proxy chain names, a group and a rule using it
			proxies := []any{citrusCloneAny(p)}
			for q := p; ; {
				dialer, _ := q.(map[string]any)["dialer-proxy"].(string)
				next, ok := byName[dialer]
				if !ok {
					break
				}
				proxies = append(proxies, citrusCloneAny(next))
				q = next
			}
			c["proxies"] = proxies
			c["proxy-groups"] = []any{map[string]any{"name": "citrus-compat", "type": "select", "proxies": []any{name}}}
			c["rules"] = []any{"MATCH,citrus-compat"}
			cases[name] = c
		}
		return cases, nil
	case "rules":
		list, _ := doc["rules"].([]any)
		cases := map[string]map[string]any{}
		for _, r := range list {
			rule, _ := r.(string)
			c := citrusClone(doc)
			c["rules"] = []any{rule, "MATCH,DIRECT"}
			cases[rule] = c
		}
		return cases, nil
	}
	return nil, fmt.Errorf("unknown split %q", split)
}

func citrusFullPath(split, path string) string {
	if split == "proxies" {
		return "proxies[0]." + path
	}
	return path
}

func citrusName(p any) string {
	m, _ := p.(map[string]any)
	name, _ := m["name"].(string)
	return name
}

func citrusClone(v any) map[string]any {
	return citrusCloneAny(v).(map[string]any)
}

func citrusCloneAny(v any) any {
	switch x := v.(type) {
	case map[string]any:
		c := make(map[string]any, len(x))
		for k, e := range x {
			c[k] = citrusCloneAny(e)
		}
		return c
	case []any:
		c := make([]any, len(x))
		for i, e := range x {
			c[i] = citrusCloneAny(e)
		}
		return c
	}
	return v
}

// citrusSegments splits "proxies[0].reality-opts.mldsa65-verify" into
// "proxies", 0, "reality-opts", "mldsa65-verify".
func citrusSegments(path string) []any {
	var segs []any
	for _, part := range strings.Split(path, ".") {
		key, idx, hasIdx := strings.Cut(part, "[")
		segs = append(segs, key)
		if hasIdx {
			n, _ := strconv.Atoi(strings.TrimSuffix(idx, "]"))
			segs = append(segs, n)
		}
	}
	return segs
}

func citrusWalk(doc any, segs []any) (parent any, last any, ok bool) {
	cur := doc
	for i, s := range segs {
		if i == len(segs)-1 {
			switch k := s.(type) {
			case string:
				m, isMap := cur.(map[string]any)
				if !isMap {
					return nil, nil, false
				}
				_, ok = m[k]
				return m, k, ok
			case int:
				l, isList := cur.([]any)
				return l, k, isList && k < len(l)
			}
		}
		switch k := s.(type) {
		case string:
			m, _ := cur.(map[string]any)
			cur = m[k]
		case int:
			l, _ := cur.([]any)
			if k >= len(l) {
				return nil, nil, false
			}
			cur = l[k]
		}
	}
	return nil, nil, false
}

func citrusHas(doc map[string]any, path string) bool {
	_, _, ok := citrusWalk(doc, citrusSegments(path))
	return ok
}

func citrusSet(doc map[string]any, path string, v any) {
	parent, last, _ := citrusWalk(doc, citrusSegments(path))
	switch p := parent.(type) {
	case map[string]any:
		p[last.(string)] = v
	case []any:
		p[last.(int)] = v
	}
}
