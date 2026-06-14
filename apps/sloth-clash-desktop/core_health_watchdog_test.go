package main

import "testing"

func TestDecideCoreHealth(t *testing.T) {
	cases := []struct {
		name       string
		monitoring bool
		probeOK    bool
		fails      int
		wantAction coreHealthAction
		wantNext   int
	}{
		{"not monitoring resets", false, false, 5, coreHealthNoop, 0},
		{"healthy probe resets", true, true, 1, coreHealthNoop, 0},
		{"first failure counts", true, false, 0, coreHealthCountFail, 1},
		{"threshold reached restarts", true, false, 1, coreHealthRestart, 0},
		{"not-monitoring wins over failed probe", false, false, 1, coreHealthNoop, 0},
		{"recovery healthy after fails resets", true, true, 1, coreHealthNoop, 0},
	}
	for _, c := range cases {
		act, next := decideCoreHealth(c.monitoring, c.probeOK, c.fails)
		if act != c.wantAction || next != c.wantNext {
			t.Errorf("%s: decideCoreHealth(%v,%v,%d) = (%d,%d), want (%d,%d)",
				c.name, c.monitoring, c.probeOK, c.fails, act, next, c.wantAction, c.wantNext)
		}
	}
}

// Threshold sanity: it takes exactly coreHealthFailThreshold consecutive
// failures to trigger a restart (a single failure never does).
func TestDecideCoreHealthThresholdSequence(t *testing.T) {
	fails := 0
	var act coreHealthAction
	// First failure: count, no restart.
	act, fails = decideCoreHealth(true, false, fails)
	if act == coreHealthRestart {
		t.Fatalf("restarted after 1 failure (threshold=%d)", coreHealthFailThreshold)
	}
	// Second consecutive failure: restart.
	act, fails = decideCoreHealth(true, false, fails)
	if coreHealthFailThreshold == 2 && act != coreHealthRestart {
		t.Fatalf("did not restart after %d consecutive failures: action=%d", coreHealthFailThreshold, act)
	}
	_ = fails
}
