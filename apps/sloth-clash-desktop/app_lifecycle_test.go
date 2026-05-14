package main

import (
	"embed"
	"strings"
	"testing"
)

func TestMarkConnectionDegradedLockedSetsHealthAndLifecycle(t *testing.T) {
	t.Parallel()
	var b embed.FS
	a := NewApp(b)
	a.state.Connection.Status = ConnConnected
	a.state.Connection.LastWarning = ""
	a.state.Connection.Health = "ready"
	a.state.Core.Lifecycle = "running"

	a.markConnectionDegradedLocked("warmup timeout")

	if a.state.Connection.Health != "degraded" {
		t.Fatalf("health = %q, want degraded", a.state.Connection.Health)
	}
	if a.state.Core.Lifecycle != "degraded" {
		t.Fatalf("core lifecycle = %q, want degraded", a.state.Core.Lifecycle)
	}
	if a.state.Connection.LastWarning == "" {
		t.Fatalf("expected warning to be populated")
	}
	evs := a.GetRuntimeDiagEvents()
	if len(evs) == 0 || evs[len(evs)-1].Category != "connection.degraded" {
		t.Fatalf("expected runtime diag degraded event, got %#v", evs)
	}
}

func TestMarkConnectionReadyLockedSetsReadyState(t *testing.T) {
	t.Parallel()
	var b embed.FS
	a := NewApp(b)
	a.state.Connection.Health = "degraded"
	a.state.Core.Lifecycle = "degraded"

	a.markConnectionReadyLocked()

	if a.state.Connection.Health != "ready" {
		t.Fatalf("health = %q, want ready", a.state.Connection.Health)
	}
	if a.state.Core.Lifecycle != "running" {
		t.Fatalf("core lifecycle = %q, want running", a.state.Core.Lifecycle)
	}
	evs := a.GetRuntimeDiagEvents()
	if len(evs) == 0 || evs[len(evs)-1].Category != "connection.ready" {
		t.Fatalf("expected runtime diag ready event, got %#v", evs)
	}
}

func TestMarkConnectionBrokenLockedDominatesReady(t *testing.T) {
	t.Parallel()
	var b embed.FS
	a := NewApp(b)
	a.state.Connection.Health = "broken"
	a.state.Connection.LastWarning = "traffic switch failed"

	a.markConnectionReadyLocked()
	if a.state.Connection.Health != "broken" {
		t.Fatalf("health = %q, want broken (ready must not override)", a.state.Connection.Health)
	}

	a.markConnectionDegradedLocked("should be ignored")
	if !strings.Contains(a.state.Connection.LastWarning, "traffic switch failed") {
		t.Fatalf("expected original warning preserved, got %q", a.state.Connection.LastWarning)
	}
	if a.state.Connection.Health != "broken" {
		t.Fatalf("health = %q, want broken", a.state.Connection.Health)
	}
}
