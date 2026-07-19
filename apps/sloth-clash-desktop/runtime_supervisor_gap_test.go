package main

import (
	"testing"
	"time"
)

// The supervisor decides "the machine probably slept" from the gap between
// ticks. Go's monotonic clock does not advance during suspend, so the gap MUST
// be measured on the wall clock or the resume pass never runs.
func TestWallClockGapMeasuresWallClock(t *testing.T) {
	// Both samples carry a monotonic reading, exactly like time.Now() in the
	// loop. Shifting one by an hour of wall time must be visible as an hour.
	base := time.Now()
	slept := base.Add(time.Hour)

	if got := wallClockGap(base, slept); got != time.Hour {
		t.Fatalf("wallClockGap = %s, want 1h", got)
	}

	// Regression guard: time.Since-style monotonic subtraction is what we are
	// deliberately NOT doing. Kept as an explicit contrast so a future edit that
	// "simplifies" this back to Sub() fails here.
	if slept.Sub(base) != time.Hour {
		t.Fatalf("sanity: fabricated offset should be 1h")
	}
}

func TestWallClockGapIsSmallForAdjacentTicks(t *testing.T) {
	prev := time.Now()
	now := prev.Add(45 * time.Second)

	gap := wallClockGap(prev, now)
	if gap != 45*time.Second {
		t.Fatalf("wallClockGap = %s, want 45s", gap)
	}
	// The loop's resume threshold is 75s — a normal tick must stay below it.
	if gap > 75*time.Second {
		t.Fatalf("a normal 45s tick must not look like a resume (gap=%s)", gap)
	}
}

func TestWallClockGapHandlesBackwardsClock(t *testing.T) {
	// An NTP correction can move the wall clock backwards; the supervisor must
	// not panic or treat it as a huge resume gap.
	prev := time.Now()
	now := prev.Add(-10 * time.Second)

	if got := wallClockGap(prev, now); got != -10*time.Second {
		t.Fatalf("wallClockGap = %s, want -10s", got)
	}
	if wallClockGap(prev, now) > 75*time.Second {
		t.Fatal("a backwards clock must not be read as a resume")
	}
}
