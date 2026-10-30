package main

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"SlothClashDesktop/internal/desync"
)

// Connect a Desync-only profile through the real lifecycle against the fake
// core and let the real controller loop calibrate it: the generated config is
// the Desync routes + MATCH,DIRECT, the mode is pinned to Rule, and no
// subscription reserve is ever probed, whether a strategy works or not.
func TestCoreLifecycleDesyncOnlyProfileNoReserve(t *testing.T) {
	for _, tc := range []struct {
		name      string
		failDelay bool
		phase     string
		selected  string
	}{
		{name: "strategy works", phase: "ready", selected: desync.YouTubeRoute + "-" + candID("youtube", 0)},
		{name: "nothing works", failDelay: true, phase: "no_strategy", selected: desyncProfileSelection},
	} {
		t.Run(tc.name, func(t *testing.T) {
			prefsMu.Lock()
			previous := prefsCurrent
			prefsCurrent.Desync = DesyncSettings{Enabled: true, YouTube: true}
			prefsCurrent.DesyncCache = nil
			prefsMu.Unlock()
			t.Cleanup(func() { prefsMu.Lock(); prefsCurrent = previous; prefsMu.Unlock() })

			h, p := newLifecycleHarness(t, fakeCoreControl{FailDelay: tc.failDelay})
			a := h.app
			// The harness profile becomes a Desync-only one (its seeded body
			// cache must then be ignored by the generation).
			a.mu.Lock()
			a.profiles[0].Kind = profileKindDesyncOnly
			sanitizeDesyncOnlyProfile(&a.profiles[0])
			a.mu.Unlock()

			if _, err := a.Connect(); err != nil {
				t.Fatalf("connect: %v", err)
			}
			h.waitConnectSettled()
			if mode := a.GetAppState().Mode.Current; mode != "rule" {
				t.Fatalf("mode = %q, want rule (harness starts in global)", mode)
			}
			if h.countEvents(p, "mode rule") == 0 {
				t.Fatalf("Rule mode never reached the core: %q", h.events(p))
			}
			raw, err := os.ReadFile(filepath.Join(h.dataDir(p), "config.yaml"))
			if err != nil {
				t.Fatal(err)
			}
			cfg := string(raw)
			if !strings.Contains(cfg, "- MATCH,DIRECT\n") || strings.Contains(cfg, "node-a") || strings.Contains(cfg, "proxy-providers") || !strings.Contains(cfg, "name: "+desync.YouTubeRoute+"\n") {
				t.Fatalf("generated config is not Desync + direct:\n%s", cfg)
			}

			ctx, cancel := context.WithCancel(context.Background())
			loopDone := make(chan struct{})
			go func() { defer close(loopDone); a.runDesyncAutoLoop(ctx) }()
			defer func() { cancel(); <-loopDone }()
			h.waitFor("YouTube "+tc.phase, 20*time.Second, func() bool {
				return a.GetDesyncAutoStatus().YouTube.Phase == tc.phase
			})
			if !a.GetDesyncStatus().DesyncOnly {
				t.Fatal("controller status does not report the Desync-only profile")
			}
			if n := h.countEvents(p, "delay "+desync.YouTubeRoute+"-"); n == 0 {
				t.Fatalf("no strategy was probed: %q", h.events(p))
			}
			if n := h.countEvents(p, "select "+desync.YouTubeRoute+"="+tc.selected); n != 1 {
				t.Fatalf("want one selection of %s, events=%q", tc.selected, h.events(p))
			}
			if n := h.countEvents(p, "profile-probe"); n != 0 {
				t.Fatalf("%d reserve probe(s) issued for a Desync-only profile: %q", n, h.events(p))
			}
		})
	}
}
