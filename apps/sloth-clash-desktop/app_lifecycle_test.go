package main

import (
	"embed"
	"testing"
)

func TestMarkConnectionDegradedLockedSetsHealthAndLifecycle(t *testing.T) {
	t.Parallel()
	var b embed.FS
	a := NewApp(b)
	a.state.Connection.Status = "connected"
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
}

