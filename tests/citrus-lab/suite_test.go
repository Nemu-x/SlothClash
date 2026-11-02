package citruslab

import (
	"bufio"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

// TestCitrusOwnSuite is stage 1: Citrus' own client matrix
// (test/interop TestMihomoClientCitrusServer, about 30 inbounds with TCP and
// UDP checks) run from the Citrus checkout of the version under test, with the
// patched core as the client. Nothing of it is copied here: it runs as Citrus
// wrote it at that commit, the way Citrus' interop-mihomo CI job runs it.
//
// knownDivergences are the suite's cases the patched core fails by design,
// with the reason. Such a case must keep failing: once it passes, the entry is
// stale and the test says so.
var knownDivergences = map[string]string{
	// The suite expects a client without support-x25519mlkem768 to be refused
	// by a server that requires the hybrid share. The patched core's adaptive
	// policy (reality-mlkem: auto, core patch 0002) retries with the hybrid
	// share after the refused classic hello and gets in, which is the point
	// of the patch. reality/require-mlkem/off in TestCitrusMatrix covers the
	// refusal of a classic hello.
	"vless-reality-require-mlkem-client-classic": "adaptive X25519MLKEM768 (core patch 0002) gets past a require-mlkem server",
}

func TestCitrusOwnSuite(t *testing.T) {
	needLab(t)
	if citrusSrc == "" {
		t.Skip("CITRUS_BIN without CITRUS_SRC: no Citrus source tree to run the suite from")
	}
	dir := filepath.Join(citrusSrc, "test", "interop")
	if _, err := os.Stat(filepath.Join(dir, "mihomo_test.go")); err != nil {
		t.Skipf("this Citrus has no mihomo client suite: %v", err)
	}
	suiteLogs := filepath.Join(logDir, "citrus-suite")
	if err := os.MkdirAll(suiteLogs, 0o755); err != nil {
		t.Fatal(err)
	}
	outPath := filepath.Join(suiteLogs, "go-test.log")
	out, err := os.Create(outPath)
	if err != nil {
		t.Fatal(err)
	}
	defer out.Close()

	cmd := exec.Command("go", "test", "-tags", "interop", "-v", "-count=1", "-timeout", "10m",
		"-run", "^TestMihomoClientCitrusServer$", ".")
	cmd.Dir = dir
	cmd.Env = append(os.Environ(),
		"MIHOMO_BIN="+patchedCore,
		"CITRUS_BIN="+citrusBin,
		"INTEROP_LOG_DIR="+suiteLogs,
		"CGO_ENABLED=0",
	)
	cmd.Stdout, cmd.Stderr = out, out
	runErr := cmd.Run()

	b, _ := os.ReadFile(outPath)
	results := regexp.MustCompile(`(?m)^\s*--- (PASS|FAIL|SKIP): (TestMihomoClientCitrusServer/\S+) \(([^)]*)\)`).FindAllStringSubmatch(string(b), -1)
	var failed, stale []string
	counts := map[string]int{}
	for _, r := range results {
		counts[r[1]]++
		name := strings.TrimPrefix(r[2], "TestMihomoClientCitrusServer/")
		why, known := knownDivergences[name]
		switch {
		case r[1] == "FAIL" && known:
			t.Logf("%-4s %s (%s): known divergence, %s", r[1], name, r[3], why)
			continue
		case r[1] == "FAIL":
			failed = append(failed, name)
		case r[1] == "PASS" && known:
			stale = append(stale, name)
		}
		t.Logf("%-4s %s (%s)", r[1], name, r[3])
	}
	if len(stale) > 0 {
		t.Errorf("listed in knownDivergences but passing now, drop them: %s", strings.Join(stale, ", "))
	}
	t.Logf("Citrus suite: %d passed, %d failed, %d skipped (full output: %s)", counts["PASS"], counts["FAIL"], counts["SKIP"], outPath)
	// go test exits non-zero for the known divergences too: judge by the
	// cases, and require a verdict for every case that started (a timeout or
	// a panic leaves some without one)
	started := len(regexp.MustCompile(`(?m)^=== RUN\s+TestMihomoClientCitrusServer/`).FindAllString(string(b), -1))
	if started != len(results) {
		t.Errorf("%d cases started, %d finished: the suite did not run to the end (%v)", started, len(results), runErr)
	}
	if len(failed) > 0 || counts["PASS"] == 0 || (runErr != nil && counts["FAIL"] == 0) {
		t.Logf("Citrus suite output, failures:\n%s", failureLines(string(b)))
		t.Errorf("Citrus' own mihomo client suite failed with the patched core (%v): %s", runErr, strings.Join(failed, ", "))
	}
}

// failureLines keeps the lines of a go test -v output around each failure.
func failureLines(s string) string {
	var keep []string
	sc := bufio.NewScanner(strings.NewReader(s))
	sc.Buffer(make([]byte, 1<<20), 1<<20)
	var window []string
	for sc.Scan() {
		line := sc.Text()
		window = append(window, line)
		if len(window) > 30 {
			window = window[1:]
		}
		if strings.Contains(line, "--- FAIL") || strings.HasPrefix(line, "FAIL") || strings.Contains(line, "panic:") {
			keep = append(keep, window...)
			keep = append(keep, "...")
			window = nil
		}
	}
	if len(keep) > 400 {
		keep = keep[len(keep)-400:]
	}
	return strings.Join(keep, "\n")
}
