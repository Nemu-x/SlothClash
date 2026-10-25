// Command fakecore is a stand-in for the mihomo binary, used by the core
// lifecycle tests (core_lifecycle_harness_test.go). It is built once per test
// run and handed to the app through SLOTH_MIHOMO_PATH, so the REAL start path
// (preflight `-t`, exec, readiness polling, hot reload, crash handling) runs
// against it unchanged.
//
// It speaks just enough of the mihomo external-controller API for those paths:
// /version, /configs (GET, PATCH mode, PUT reload), /proxies, /proxies/{name}
// (PUT selection), /proxies/{name}/delay, /traffic, /rules, /providers/rules,
// with the same bearer-secret check mihomo applies. It never opens the
// mixed-port, never touches TUN and never dials out.
//
// Behaviour is steered per data dir by an optional fakecore.json next to
// config.yaml (see control below), and every notable event is appended to
// fakecore-events.log in the same dir so tests can assert on what the app
// actually asked the core to do.
package main

import (
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"

	"gopkg.in/yaml.v3"
)

// control is the per-data-dir behaviour switchboard written by the tests.
type control struct {
	// PreflightError makes `-t` fail the way mihomo does on a bad config.
	PreflightError string `json:"preflightError"`
	// NeverReady keeps /version answering 503, so the app's readiness poll
	// runs into its deadline.
	NeverReady bool `json:"neverReady"`
	// BreakOnFirstReload makes the first PUT /configs of the whole data dir
	// fail AND take /version down with it (the "core really is unresponsive"
	// case). A marker file makes it one-shot across processes, so the app's
	// fallback restart gets a healthy core.
	BreakOnFirstReload bool `json:"breakOnFirstReload"`
	// FailReload makes every PUT /configs return 500 while /version stays
	// healthy (the "reload response lost, core fine" case).
	FailReload bool `json:"failReload"`
}

// maxLifetime bounds how long an orphaned fake core can outlive a crashed
// test binary. Generous next to any single test, short next to a dev session.
const maxLifetime = 3 * time.Minute

var (
	dataDir string
	evMu    sync.Mutex
)

func logEvent(format string, args ...any) {
	evMu.Lock()
	defer evMu.Unlock()
	f, err := os.OpenFile(filepath.Join(dataDir, "fakecore-events.log"), os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o644)
	if err != nil {
		return
	}
	defer f.Close()
	fmt.Fprintf(f, "%d "+format+"\n", append([]any{os.Getpid()}, args...)...)
}

func readControl() control {
	var c control
	b, err := os.ReadFile(filepath.Join(dataDir, "fakecore.json"))
	if err == nil {
		_ = json.Unmarshal(b, &c)
	}
	return c
}

func main() {
	flag.StringVar(&dataDir, "d", ".", "data dir")
	testOnly := flag.Bool("t", false, "test config and exit")
	flag.Parse()

	ctl := readControl()
	raw, err := os.ReadFile(filepath.Join(dataDir, "config.yaml"))
	if err != nil {
		fmt.Fprintf(os.Stderr, "parse config error: %v\n", err)
		os.Exit(1)
	}
	var cfg map[string]any
	if err := yaml.Unmarshal(raw, &cfg); err != nil {
		fmt.Fprintf(os.Stderr, "parse config error: %v\n", err)
		os.Exit(1)
	}

	if *testOnly {
		if ctl.PreflightError != "" {
			logEvent("preflight fail")
			fmt.Fprintf(os.Stderr, "time=x level=error msg=\"parse config error: %s\"\n", ctl.PreflightError)
			os.Exit(1)
		}
		logEvent("preflight ok")
		fmt.Println("configuration file test is successful")
		return
	}

	controller, _ := cfg["external-controller"].(string)
	secret, _ := cfg["secret"].(string)
	if strings.TrimSpace(controller) == "" {
		fmt.Fprintln(os.Stderr, "fakecore: config has no external-controller")
		os.Exit(2)
	}
	ln, err := net.Listen("tcp", controller)
	if err != nil {
		fmt.Fprintf(os.Stderr, "fakecore: listen %s: %v\n", controller, err)
		os.Exit(2)
	}
	time.AfterFunc(maxLifetime, func() { os.Exit(0) })

	s := &server{secret: secret, ctl: ctl, mode: "rule", now: map[string]string{"GLOBAL": "PROXY", "PROXY": "node-a"}}
	if m, ok := cfg["mode"].(string); ok && m != "" {
		s.mode = m
	}
	logEvent("start controller=%s", controller)
	// Something for core.log, so the app's per-run log rotation has a
	// non-empty file to keep.
	fmt.Printf("level=info msg=\"RESTful API listening at: %s\"\n", controller)
	_ = http.Serve(ln, s)
}

type server struct {
	secret string
	ctl    control

	mu     sync.Mutex
	mode   string
	broken bool
	now    map[string]string
}

func writeJSON(w http.ResponseWriter, code int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(code)
	_ = json.NewEncoder(w).Encode(v)
}

func (s *server) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if s.secret != "" && r.Header.Get("Authorization") != "Bearer "+s.secret {
		writeJSON(w, http.StatusUnauthorized, map[string]string{"message": "Unauthorized"})
		return
	}
	path := r.URL.Path
	switch {
	case path == "/version":
		s.mu.Lock()
		broken := s.broken
		s.mu.Unlock()
		if s.ctl.NeverReady || broken {
			writeJSON(w, http.StatusServiceUnavailable, map[string]string{"message": "not ready"})
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{"version": "fake-1.0.0", "meta": true})
	case path == "/configs":
		s.handleConfigs(w, r)
	case path == "/proxies" && r.Method == http.MethodGet:
		writeJSON(w, http.StatusOK, map[string]any{"proxies": s.proxies()})
	case strings.HasPrefix(path, "/proxies/") && strings.HasSuffix(path, "/delay"):
		writeJSON(w, http.StatusOK, map[string]int{"delay": 42})
	case strings.HasPrefix(path, "/proxies/") && r.Method == http.MethodPut:
		name := strings.TrimPrefix(path, "/proxies/")
		var body struct {
			Name string `json:"name"`
		}
		_ = json.NewDecoder(io.LimitReader(r.Body, 4096)).Decode(&body)
		s.mu.Lock()
		_, known := s.now[name]
		if known {
			s.now[name] = body.Name
		}
		s.mu.Unlock()
		if !known {
			writeJSON(w, http.StatusNotFound, map[string]string{"message": "proxy not found"})
			return
		}
		logEvent("select %s=%s", name, body.Name)
		w.WriteHeader(http.StatusNoContent)
	case path == "/traffic":
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprintln(w, `{"up":1024,"down":2048}`)
	case path == "/rules":
		writeJSON(w, http.StatusOK, map[string]any{"rules": []any{}})
	case path == "/providers/rules":
		writeJSON(w, http.StatusOK, map[string]any{"providers": map[string]any{}})
	case path == "/debug/exit":
		// Test hook: die like a crashed core (non-zero exit, no cleanup).
		code, _ := strconv.Atoi(r.URL.Query().Get("code"))
		if code == 0 {
			code = 3
		}
		logEvent("exit code=%d", code)
		w.WriteHeader(http.StatusNoContent)
		if f, ok := w.(http.Flusher); ok {
			f.Flush()
		}
		go func() {
			time.Sleep(50 * time.Millisecond)
			os.Exit(code)
		}()
	default:
		writeJSON(w, http.StatusNotFound, map[string]string{"message": "not found"})
	}
}

func (s *server) handleConfigs(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
		s.mu.Lock()
		mode := s.mode
		s.mu.Unlock()
		writeJSON(w, http.StatusOK, map[string]any{"mode": mode, "tun": map[string]any{"enable": false}})
	case http.MethodPatch:
		var body struct {
			Mode string `json:"mode"`
		}
		_ = json.NewDecoder(io.LimitReader(r.Body, 4096)).Decode(&body)
		if body.Mode != "" {
			s.mu.Lock()
			s.mode = body.Mode
			s.mu.Unlock()
			logEvent("mode %s", body.Mode)
		}
		w.WriteHeader(http.StatusNoContent)
	case http.MethodPut:
		cfgPath := r.URL.Query().Get("path")
		if s.ctl.BreakOnFirstReload {
			marker := filepath.Join(dataDir, "fakecore-broke-once")
			if _, err := os.Stat(marker); errors.Is(err, os.ErrNotExist) {
				_ = os.WriteFile(marker, []byte("1"), 0o644)
				s.mu.Lock()
				s.broken = true
				s.mu.Unlock()
				logEvent("reload broken path=%s", cfgPath)
				writeJSON(w, http.StatusInternalServerError, map[string]string{"message": "reload exploded"})
				return
			}
		}
		if s.ctl.FailReload {
			logEvent("reload fail path=%s", cfgPath)
			writeJSON(w, http.StatusInternalServerError, map[string]string{"message": "reload failed"})
			return
		}
		if _, err := os.Stat(cfgPath); err != nil {
			writeJSON(w, http.StatusBadRequest, map[string]string{"message": err.Error()})
			return
		}
		logEvent("reload ok path=%s", cfgPath)
		w.WriteHeader(http.StatusNoContent)
	default:
		w.WriteHeader(http.StatusMethodNotAllowed)
	}
}

func (s *server) proxies() map[string]any {
	s.mu.Lock()
	defer s.mu.Unlock()
	return map[string]any{
		"GLOBAL": map[string]any{"type": "Selector", "all": []string{"PROXY", "DIRECT"}, "now": s.now["GLOBAL"]},
		"PROXY":  map[string]any{"type": "Selector", "all": []string{"node-a", "DIRECT"}, "now": s.now["PROXY"]},
		"node-a": map[string]any{"type": "Shadowsocks", "history": []any{}},
		"DIRECT": map[string]any{"type": "Direct", "history": []any{}},
	}
}
