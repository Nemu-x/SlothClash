// Package citruslab is the opt-in interop lab between the core SlothClash ships
// (core/patches/mihomo) and Citrus, the server several subscriptions run on.
// Everything is local: Citrus servers on the loopback, a local HTTP echo, a UDP
// echo and a local TLS 1.3 REALITY target. No internet, no Docker. See
// docs/core-patches.md.
//
//	CITRUS_LAB=1 SLOTH_MIHOMO_BIN=<patched core> [SLOTH_MIHOMO_STOCK_BIN=<stock core>] \
//	  [CITRUS_LAB_FLAVOR=release|dev|head] go test -v -count=1 -parallel 12 -timeout 15m .
//
// Stage 1 (TestCitrusOwnSuite) runs Citrus' own TestMihomoClientCitrusServer
// from the Citrus checkout with the patched core as the client. Stage 2
// (TestCitrusMatrix) is this lab's own cases, each with an encoded expectation
// for the patched core and, when SLOTH_MIHOMO_STOCK_BIN is set, for the stock
// core of the same tag. TestCitrusUDP and TestCitrusUDPLongIdle are the UDP
// block (the latter is skipped under -short).
//
// Citrus comes from citrus.json: "release" downloads the pinned build and
// checks its sha256, "dev" builds the pinned dev commit, "head" builds the
// current dev branch (the weekly, non-gating run). CITRUS_BIN overrides the
// binary (CITRUS_SRC then names its source tree, for stage 1 and for the
// feature checks; without it stage 1 is skipped and every case runs).
// CITRUS_LAB_LOG_DIR keeps every process log and config (CI uploads them on
// failure). CITRUS_LAB_CACHE is where downloads and checkouts go.
package citruslab

import (
	"archive/zip"
	"bytes"
	"compress/gzip"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"
)

var (
	labOn       bool
	patchedCore string // SLOTH_MIHOMO_BIN
	stockCore   string // SLOTH_MIHOMO_STOCK_BIN
	citrusBin   string
	citrusSrc   string // Citrus source tree at the commit citrusBin was built from ("" = unknown)
	citrusDesc  string // what citrusBin is, for the logs
	workDir     string
	logDir      string

	// features of the Citrus under test the cases depend on; with no source
	// tree every feature is assumed present.
	features = map[string]bool{}
)

type pinFile struct {
	Repo    string `json:"repo"`
	Release struct {
		Tag    string `json:"tag"`
		Commit string `json:"commit"`
		Assets map[string]struct {
			Name   string `json:"name"`
			SHA256 string `json:"sha256"`
		} `json:"assets"`
	} `json:"release"`
	Dev struct {
		Branch string `json:"branch"`
		Commit string `json:"commit"`
	} `json:"dev"`
}

func TestMain(m *testing.M) {
	labOn = os.Getenv("CITRUS_LAB") == "1"
	if !labOn {
		os.Exit(m.Run())
	}
	os.Exit(run(m))
}

func run(m *testing.M) int {
	var err error
	if patchedCore, err = absEnv("SLOTH_MIHOMO_BIN"); err != nil {
		return fail(err)
	}
	if stockCore, err = absEnv("SLOTH_MIHOMO_STOCK_BIN"); err != nil {
		return fail(err)
	}
	if patchedCore == "" {
		return fail(errors.New("set SLOTH_MIHOMO_BIN to the core under test"))
	}
	for _, bin := range []string{patchedCore, stockCore} {
		if bin == "" {
			continue
		}
		out, err := exec.Command(bin, "-v").CombinedOutput()
		if err != nil {
			return fail(fmt.Errorf("%s -v: %v\n%s", bin, err, out))
		}
		fmt.Printf("core: %s (%s)\n", firstLine(string(out)), bin)
	}

	if workDir, err = os.MkdirTemp("", "sloth-citrus-lab-"); err != nil {
		return fail(err)
	}
	defer os.RemoveAll(workDir)
	logDir = os.Getenv("CITRUS_LAB_LOG_DIR")
	if logDir == "" {
		logDir = filepath.Join(workDir, "logs")
	}
	if logDir, err = filepath.Abs(logDir); err != nil {
		return fail(err)
	}
	if err = os.MkdirAll(logDir, 0o755); err != nil {
		return fail(err)
	}

	if err = prepareCitrus(); err != nil {
		return fail(err)
	}
	out, err := exec.Command(citrusBin, "-v").CombinedOutput()
	if err != nil {
		return fail(fmt.Errorf("citrus -v: %v\n%s", err, out))
	}
	fmt.Printf("citrus: %s (%s)\n", firstLine(string(out)), citrusDesc)
	detectFeatures()

	stop, err := startFixtures()
	if err != nil {
		return fail(err)
	}
	defer stop()
	return m.Run()
}

func fail(err error) int {
	fmt.Fprintln(os.Stderr, "citrus lab setup:", err)
	return 1
}

func absEnv(name string) (string, error) {
	v := os.Getenv(name)
	if v == "" {
		return "", nil
	}
	return filepath.Abs(v)
}

func needLab(t *testing.T) {
	t.Helper()
	if !labOn {
		t.Skip("set CITRUS_LAB=1 and SLOTH_MIHOMO_BIN to run the Citrus lab")
	}
}

// --- Citrus ------------------------------------------------------------------

func readPins() (*pinFile, error) {
	b, err := os.ReadFile("citrus.json")
	if err != nil {
		return nil, err
	}
	var p pinFile
	if err := json.Unmarshal(b, &p); err != nil {
		return nil, fmt.Errorf("citrus.json: %w", err)
	}
	return &p, nil
}

func cacheDir() (string, error) {
	d := os.Getenv("CITRUS_LAB_CACHE")
	if d == "" {
		d = filepath.Join(os.TempDir(), "sloth-citrus-lab-cache")
	}
	d, err := filepath.Abs(d)
	if err != nil {
		return "", err
	}
	return d, os.MkdirAll(d, 0o755)
}

func prepareCitrus() error {
	if bin := os.Getenv("CITRUS_BIN"); bin != "" {
		var err error
		if citrusBin, err = filepath.Abs(bin); err != nil {
			return err
		}
		if src := os.Getenv("CITRUS_SRC"); src != "" {
			if citrusSrc, err = filepath.Abs(src); err != nil {
				return err
			}
		}
		citrusDesc = "CITRUS_BIN"
		return nil
	}
	pins, err := readPins()
	if err != nil {
		return err
	}
	cache, err := cacheDir()
	if err != nil {
		return err
	}
	flavor := os.Getenv("CITRUS_LAB_FLAVOR")
	if flavor == "" {
		flavor = "release"
	}
	switch flavor {
	case "release":
		if citrusBin, err = downloadRelease(pins, cache); err != nil {
			return err
		}
		if citrusSrc, err = checkout(pins.Repo, pins.Release.Commit, cache); err != nil {
			return err
		}
		citrusDesc = "release " + pins.Release.Tag + " @ " + pins.Release.Commit[:12]
	case "dev", "head":
		commit := pins.Dev.Commit
		if flavor == "head" {
			if commit, err = remoteHead(pins.Repo, pins.Dev.Branch); err != nil {
				return err
			}
		}
		if citrusSrc, err = checkout(pins.Repo, commit, cache); err != nil {
			return err
		}
		if citrusBin, err = buildCitrus(citrusSrc, cache, commit); err != nil {
			return err
		}
		citrusDesc = "pinned " + pins.Dev.Branch + " @ " + commit[:12]
		if flavor == "head" {
			citrusDesc = pins.Dev.Branch + " head @ " + commit[:12]
		}
	default:
		return fmt.Errorf("CITRUS_LAB_FLAVOR=%q: want release, dev or head", flavor)
	}
	return nil
}

// downloadRelease fetches the pinned release build for this host and checks
// it against the pinned sha256 and the release's own checksums.txt.
func downloadRelease(p *pinFile, cache string) (string, error) {
	key := runtime.GOOS + "-" + runtime.GOARCH
	asset, ok := p.Release.Assets[key]
	if !ok {
		return "", fmt.Errorf("citrus.json pins no release asset for %s (set CITRUS_BIN or CITRUS_LAB_FLAVOR=dev)", key)
	}
	bin := filepath.Join(cache, "citrus-"+p.Release.Tag+"-"+key+exeSuffix())
	base := p.Repo + "/releases/download/" + p.Release.Tag + "/"
	sums, err := fetch(base + "checksums.txt")
	if err != nil {
		return "", err
	}
	listed := ""
	for _, line := range strings.Split(string(sums), "\n") {
		f := strings.Fields(line)
		if len(f) == 2 && strings.TrimPrefix(f[1], "./") == asset.Name {
			listed = f[0]
		}
	}
	if listed != asset.SHA256 {
		return "", fmt.Errorf("%s: checksums.txt lists %q, citrus.json pins %q", asset.Name, listed, asset.SHA256)
	}
	archive, err := fetch(base + asset.Name)
	if err != nil {
		return "", err
	}
	sum := sha256.Sum256(archive)
	if got := hex.EncodeToString(sum[:]); got != asset.SHA256 {
		return "", fmt.Errorf("%s: sha256 %s, pinned %s", asset.Name, got, asset.SHA256)
	}
	var exe []byte
	switch {
	case strings.HasSuffix(asset.Name, ".gz"):
		zr, err := gzip.NewReader(bytes.NewReader(archive))
		if err != nil {
			return "", err
		}
		if exe, err = io.ReadAll(zr); err != nil {
			return "", err
		}
	case strings.HasSuffix(asset.Name, ".zip"):
		zr, err := zip.NewReader(bytes.NewReader(archive), int64(len(archive)))
		if err != nil {
			return "", err
		}
		for _, f := range zr.File {
			if strings.HasSuffix(f.Name, ".exe") {
				rc, err := f.Open()
				if err != nil {
					return "", err
				}
				exe, err = io.ReadAll(rc)
				rc.Close()
				if err != nil {
					return "", err
				}
			}
		}
	}
	if len(exe) == 0 {
		return "", fmt.Errorf("%s: no binary in the archive", asset.Name)
	}
	if err := os.WriteFile(bin, exe, 0o755); err != nil {
		return "", err
	}
	return bin, nil
}

func fetch(url string) ([]byte, error) {
	var last error
	for attempt := 0; attempt < 3; attempt++ {
		resp, err := (&http.Client{Timeout: 2 * time.Minute}).Get(url)
		if err == nil {
			b, rerr := io.ReadAll(resp.Body)
			resp.Body.Close()
			if resp.StatusCode == http.StatusOK && rerr == nil {
				return b, nil
			}
			err = fmt.Errorf("HTTP %d, %v", resp.StatusCode, rerr)
		}
		last = fmt.Errorf("GET %s: %w", url, err)
		time.Sleep(time.Duration(attempt+1) * 3 * time.Second)
	}
	return nil, last
}

func git(dir string, args ...string) (string, error) {
	cmd := exec.Command("git", args...)
	cmd.Dir = dir
	out, err := cmd.CombinedOutput()
	if err != nil {
		return "", fmt.Errorf("git %s: %v\n%s", strings.Join(args, " "), err, out)
	}
	return strings.TrimSpace(string(out)), nil
}

func remoteHead(repo, branch string) (string, error) {
	out, err := git("", "ls-remote", repo, "refs/heads/"+branch)
	if err != nil {
		return "", err
	}
	f := strings.Fields(out)
	if len(f) == 0 || len(f[0]) != 40 {
		return "", fmt.Errorf("ls-remote %s %s: %q", repo, branch, out)
	}
	return f[0], nil
}

// checkout returns a shallow checkout of repo at commit, reused when present.
func checkout(repo, commit, cache string) (string, error) {
	dir := filepath.Join(cache, "src-"+commit[:12])
	if head, err := git(dir, "rev-parse", "HEAD"); err == nil && head == commit {
		return dir, nil
	}
	if err := os.RemoveAll(dir); err != nil {
		return "", err
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return "", err
	}
	for _, args := range [][]string{
		{"init", "-q"},
		{"fetch", "-q", "--depth", "1", repo, commit},
		{"-c", "advice.detachedHead=false", "checkout", "-q", "FETCH_HEAD"},
	} {
		if _, err := git(dir, args...); err != nil {
			return "", err
		}
	}
	if head, err := git(dir, "rev-parse", "HEAD"); err != nil || head != commit {
		return "", fmt.Errorf("checkout of %s is at %q, %v", commit, head, err)
	}
	return dir, nil
}

// buildCitrus builds the checkout the way Citrus' own CI does. GOTOOLCHAIN is
// left to the environment, so Go fetches the release Citrus' go.mod asks for
// when the installed one is older.
func buildCitrus(src, cache, commit string) (string, error) {
	bin := filepath.Join(cache, "citrus-"+commit[:12]+exeSuffix())
	if _, err := os.Stat(bin); err == nil {
		return bin, nil
	}
	start := time.Now()
	cmd := exec.Command("go", "build", "-trimpath", "-o", bin, ".")
	cmd.Dir = src
	cmd.Env = append(os.Environ(), "CGO_ENABLED=0")
	if out, err := cmd.CombinedOutput(); err != nil {
		return "", fmt.Errorf("go build citrus: %v\n%s", err, out)
	}
	fmt.Printf("built citrus %s in %s\n", commit[:12], time.Since(start).Round(time.Second))
	return bin, nil
}

// detectFeatures reads from the source tree what the Citrus under test
// serves; a case that needs a missing one is skipped, naming it.
func detectFeatures() {
	has := func(rel, needle string) bool {
		b, err := os.ReadFile(filepath.Join(citrusSrc, filepath.FromSlash(rel)))
		return err == nil && (needle == "" || strings.Contains(string(b), needle))
	}
	checks := map[string]func() bool{
		"max-client-ver": func() bool { return has("listener/inbound/reality.go", `"max-client-ver`) },
		"xdrive":         func() bool { return has("listener/inbound/vless.go", `"xdrive-config`) },
		"xhttp-h3":       func() bool { return has("listener/inbound/vless_xhttp_h3_test.go", "") },
		"shadowquic":     func() bool { return has("listener/inbound/shadowquic.go", "") },
	}
	var missing []string
	for name, check := range checks {
		features[name] = citrusSrc == "" || check()
		if !features[name] {
			missing = append(missing, name)
		}
	}
	if len(missing) > 0 {
		fmt.Printf("citrus: not served by this version, cases skipped: %s\n", strings.Join(missing, ", "))
	}
}

func needFeature(t *testing.T, name string) {
	t.Helper()
	if name == "" {
		return
	}
	on, known := features[name]
	if !known {
		t.Fatalf("unknown feature %q", name)
	}
	if !on {
		t.Skipf("this Citrus does not serve %s", name)
	}
}

func firstLine(s string) string {
	s, _, _ = strings.Cut(s, "\n")
	return strings.TrimSpace(s)
}

func exeSuffix() string {
	if runtime.GOOS == "windows" {
		return ".exe"
	}
	return ""
}
