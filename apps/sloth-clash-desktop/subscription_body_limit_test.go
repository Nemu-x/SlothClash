package main

import (
	"bytes"
	"os"
	"path/filepath"
	"sync/atomic"
	"testing"

	"SlothClashDesktop/internal/runtimecfg"
	"SlothClashDesktop/internal/runtimecfg/runtimecfgtest"
)

// The explicit Refresh tolerates transient fetch failures, but an oversized
// body must reach the UI; LastUpdated still advances so the auto-update loop
// does not re-download it every minute.
func TestRefreshProfileSubscriptionSurfacesOversizedBody(t *testing.T) {
	small := runtimecfgtest.RealisticSubscriptionYAML(10)
	var body atomic.Pointer[[]byte]
	body.Store(&small)
	url := runtimecfgtest.ServeSubscription(t, &body)
	const profileID = "profile-body-limit-test"
	root, err := slothDataRoot()
	if err != nil {
		t.Fatal(err)
	}
	dataDir := filepath.Join(root, "runtime", profileID)
	t.Cleanup(func() { _ = os.RemoveAll(dataDir) })

	a := &App{profiles: []Profile{{ID: profileID, Name: "big", Type: "subscription", URL: url}}}
	if _, err := a.refreshProfileSubscription(profileID, false); err != nil {
		t.Fatalf("normal refresh: %v", err)
	}
	cached := runtimecfg.ReadSubscriptionBodyCache(dataDir)
	if !bytes.Equal(cached, small) {
		t.Fatalf("normal refresh cached %d bytes, want %d", len(cached), len(small))
	}

	over := runtimecfgtest.PadYAML(t, small, 32<<20+1) // runtimecfg's subscription body cap + 1
	body.Store(&over)
	a.profiles[0].LastUpdated = 0
	state, err := a.refreshProfileSubscription(profileID, false)
	if err == nil || err.Error() != "subscription response exceeds the 32 MiB limit" {
		t.Fatalf("oversized refresh: %v", err)
	}
	if len(state.Profile.Profiles) != 1 || state.Profile.Profiles[0].LastUpdated == 0 {
		t.Fatalf("LastUpdated not advanced: %+v", state.Profile.Profiles)
	}
	if !bytes.Equal(runtimecfg.ReadSubscriptionBodyCache(dataDir), small) {
		t.Fatal("oversized refresh replaced the last-known-good cache")
	}
}
