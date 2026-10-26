package runtimecfg

import (
	"errors"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"SlothClashDesktop/internal/runtimecfg/runtimecfgtest"
)

type bodyFetchLog struct {
	mu   sync.Mutex
	errs []error
	dirs []string
}

func (l *bodyFetchLog) hook(dataDir string, err error) {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.dirs = append(l.dirs, dataDir)
	l.errs = append(l.errs, err)
}

func (l *bodyFetchLog) snapshot() ([]string, []error) {
	l.mu.Lock()
	defer l.mu.Unlock()
	return append([]string(nil), l.dirs...), append([]error(nil), l.errs...)
}

// Both pipeline fetch paths report their outcome through OnBodyFetch: the
// blocking cache-miss fetch of a first Connect and the background refresh a
// cache hit kicks.
func TestPipelineReportsBodyFetchOutcome(t *testing.T) {
	empty := []byte("")
	var body atomic.Pointer[[]byte]
	body.Store(&empty)
	url := runtimecfgtest.ServeSubscription(t, &body)
	dataDir := t.TempDir()

	var log bodyFetchLog
	env := testEnv()
	env.Client.OnBodyFetch = log.hook
	in := Input{
		DataDir: dataDir, SubURL: url, CtrlPort: 9090, MixedPort: 7890, Secret: "s",
		WithExternalController: true, Traffic: "rule", Env: env,
	}

	// Cache miss, provider ships an empty body: reported as a failure.
	if outcome, err := tryWriteMergedFullProfile(in, nil); err != nil || outcome != pipelineCacheMissNoNet {
		t.Fatalf("cache miss: outcome %s, err %v", outcome, err)
	}
	dirs, errs := log.snapshot()
	if len(errs) != 1 || dirs[0] != dataDir || !errors.Is(errs[0], errEmptySubscriptionBody) {
		t.Fatalf("cache miss reported %v %v", dirs, errs)
	}

	// Cache miss, good body: reported as a success.
	good := runtimecfgtest.RealisticSubscriptionYAML(3)
	body.Store(&good)
	if outcome, err := tryWriteMergedFullProfile(in, nil); err != nil || outcome != pipelineOK {
		t.Fatalf("cache fill: outcome %s, err %v", outcome, err)
	}
	if _, errs = log.snapshot(); len(errs) != 2 || errs[1] != nil {
		t.Fatalf("cache fill reported %v", errs)
	}

	// Cache hit: the background refresh reports too.
	body.Store(&empty)
	if outcome, err := tryWriteMergedFullProfile(in, nil); err != nil || outcome != pipelineOK {
		t.Fatalf("cache hit: outcome %s, err %v", outcome, err)
	}
	errs = waitBodyFetchReports(t, &log, 3)
	if !errors.Is(errs[2], errEmptySubscriptionBody) {
		t.Fatalf("background refresh failure reported %v", errs[2])
	}

	// The hook fires just before the singleflight slot is released; wait for
	// the release so the next kick starts its own fetch instead of attaching.
	for deadline := time.Now().Add(10 * time.Second); ; time.Sleep(5 * time.Millisecond) {
		if _, busy := inflightSubscriptionFetch.Load(dataDir); !busy {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("background refresh never released its singleflight slot")
		}
	}
	body.Store(&good)
	if outcome, err := tryWriteMergedFullProfile(in, nil); err != nil || outcome != pipelineOK {
		t.Fatalf("cache hit: outcome %s, err %v", outcome, err)
	}
	if errs = waitBodyFetchReports(t, &log, 4); errs[3] != nil {
		t.Fatalf("background refresh success reported %v", errs[3])
	}
}

func waitBodyFetchReports(t *testing.T, log *bodyFetchLog, n int) []error {
	t.Helper()
	deadline := time.Now().Add(10 * time.Second)
	for {
		if _, errs := log.snapshot(); len(errs) >= n {
			return errs
		}
		if time.Now().After(deadline) {
			_, errs := log.snapshot()
			t.Fatalf("background refresh never reported (have %d of %d): %v", len(errs), n, errs)
		}
		time.Sleep(10 * time.Millisecond)
	}
}
