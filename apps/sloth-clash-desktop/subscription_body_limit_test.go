package main

import (
	"bytes"
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"sync/atomic"
	"testing"
	"time"

	"github.com/metacubex/age"
	"github.com/metacubex/age/armor"
)

const realisticSubscriptionNodes = 10000

// padYAML right-pads a YAML document with a trailing comment line to exactly
// size bytes, so the padded body still parses to the same document.
func padYAML(t *testing.T, body []byte, size int) []byte {
	t.Helper()
	if len(body)+2 > size || !bytes.HasSuffix(body, []byte("\n")) {
		t.Fatalf("fixture of %d bytes cannot be padded to %d", len(body), size)
	}
	out := append(append([]byte(nil), body...), '#')
	out = append(out, bytes.Repeat([]byte("x"), size-len(body)-2)...)
	return append(out, '\n')
}

func ageArmor(t *testing.T, recipient age.Recipient, payload []byte) []byte {
	t.Helper()
	var buf bytes.Buffer
	aw := armor.NewWriter(&buf)
	w, err := age.Encrypt(aw, recipient)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := w.Write(payload); err != nil {
		t.Fatal(err)
	}
	if err := w.Close(); err != nil {
		t.Fatal(err)
	}
	if err := aw.Close(); err != nil {
		t.Fatal(err)
	}
	return buf.Bytes()
}

// subscriptionServer serves whatever *body holds at the moment of the request.
func subscriptionServer(t *testing.T, body *atomic.Pointer[[]byte]) string {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "text/yaml")
		_, _ = w.Write(*body.Load())
	}))
	t.Cleanup(srv.Close)
	return srv.URL + "/sub"
}

func fetchTestSubscription(t *testing.T, url, ageKey string) ([]byte, error) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	body, _, err := fetchSubscriptionBody(ctx, url, ageKey, currentDesktopPrefs().Privacy.IsHwidEnabled())
	return body, err
}

func assertSubscriptionNodes(t *testing.T, body []byte, want int) {
	t.Helper()
	doc, err := parseClashDocToMap(body)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	proxies, _ := doc["proxies"].([]any)
	if len(proxies) != want || !subscriptionDocIsFullProfile(doc) {
		t.Fatalf("parsed %d proxies (want %d), full=%t", len(proxies), want, subscriptionDocIsFullProfile(doc))
	}
}

func TestRealisticSubscriptionFitsBodyCap(t *testing.T) {
	body := realisticSubscriptionYAML(realisticSubscriptionNodes)
	t.Logf("%d-node subscription: %d bytes (%.2f MiB), %d B/node; cap %s",
		realisticSubscriptionNodes, len(body), float64(len(body))/(1<<20), len(body)/realisticSubscriptionNodes,
		formatByteLimit(subscriptionMaxBodyBytes))
	if len(body) <= 6<<20 {
		t.Fatalf("fixture (%d bytes) must exceed the former 6 MiB cap it is meant to justify replacing", len(body))
	}
	if int64(len(body))*3 > subscriptionMaxBodyBytes {
		t.Fatalf("a %d-node subscription (%d bytes) leaves less than 3x headroom under %s",
			realisticSubscriptionNodes, len(body), formatByteLimit(subscriptionMaxBodyBytes))
	}
	assertSubscriptionNodes(t, body, realisticSubscriptionNodes)
}

func TestFetchSubscriptionBodyAtAndOverLimit(t *testing.T) {
	yaml := realisticSubscriptionYAML(realisticSubscriptionNodes)
	var body atomic.Pointer[[]byte]
	url := subscriptionServer(t, &body)

	at := padYAML(t, yaml, subscriptionMaxBodyBytes)
	body.Store(&at)
	got, err := fetchTestSubscription(t, url, "")
	if err != nil || len(got) != subscriptionMaxBodyBytes {
		t.Fatalf("at limit: %d bytes, %v", len(got), err)
	}
	assertSubscriptionNodes(t, got, realisticSubscriptionNodes)

	for _, tc := range []struct {
		size int
		want string
	}{
		// Plain body past the content cap but within the wire cap.
		{subscriptionMaxBodyBytes + 1, "subscription response exceeds the 32 MiB limit"},
		{subscriptionMaxWireBytes + 1, "subscription response exceeds the 48 MiB limit"},
	} {
		over := padYAML(t, yaml, tc.size)
		body.Store(&over)
		got, err := fetchTestSubscription(t, url, "")
		if err == nil || err.Error() != tc.want || got != nil {
			t.Fatalf("size %d: %d bytes, %v", tc.size, len(got), err)
		}
	}
}

func TestFetchAgeSubscriptionAtAndOverLimit(t *testing.T) {
	id, err := age.GenerateX25519Identity()
	if err != nil {
		t.Fatal(err)
	}
	yaml := realisticSubscriptionYAML(realisticSubscriptionNodes)
	var body atomic.Pointer[[]byte]
	url := subscriptionServer(t, &body)

	at := ageArmor(t, id.Recipient(), padYAML(t, yaml, subscriptionMaxBodyBytes))
	t.Logf("age armor of a %s body: %d bytes (%.3fx), wire cap %s",
		formatByteLimit(subscriptionMaxBodyBytes), len(at), float64(len(at))/subscriptionMaxBodyBytes,
		formatByteLimit(subscriptionMaxWireBytes))
	if int64(len(at)) > subscriptionMaxWireBytes {
		t.Fatalf("an armored body at the content cap (%d bytes) does not fit the wire cap", len(at))
	}
	body.Store(&at)
	got, err := fetchTestSubscription(t, url, id.String())
	if err != nil || len(got) != subscriptionMaxBodyBytes {
		t.Fatalf("at limit: %d bytes, %v", len(got), err)
	}
	assertSubscriptionNodes(t, got, realisticSubscriptionNodes)

	over := ageArmor(t, id.Recipient(), padYAML(t, yaml, subscriptionMaxBodyBytes+1))
	body.Store(&over)
	got, err = fetchTestSubscription(t, url, id.String())
	if err == nil || err.Error() != "decrypted subscription response exceeds the 32 MiB limit" || got != nil {
		t.Fatalf("oversized plaintext: %d bytes, %v", len(got), err)
	}
}

// An oversized body on a first Connect must fail loudly, not fall back to the
// bare provider profile that hides why the groups and rules are gone.
func TestFullProfilePipelineRefusesOversizedSubscription(t *testing.T) {
	over := padYAML(t, realisticSubscriptionYAML(100), subscriptionMaxBodyBytes+1)
	var body atomic.Pointer[[]byte]
	body.Store(&over)
	url := subscriptionServer(t, &body)
	dataDir := t.TempDir()

	outcome, err := tryWriteMergedFullProfile(runtimeConfigInput{
		DataDir:                dataDir,
		SubURL:                 url,
		CtrlPort:               9090,
		MixedPort:              7890,
		Secret:                 "s",
		WithExternalController: true,
		Traffic:                "rule",
		EnableTun:              false,
		Prefs:                  currentRuntimeConfigPrefs(),
	}, nil)
	if outcome != pipelineBodyTooLarge || outcome.useBareFallback() || !isReadLimitError(err) {
		t.Fatalf("outcome %s, err %v", outcome, err)
	}
	if _, statErr := os.Stat(subscriptionBodyCachePath(dataDir)); !os.IsNotExist(statErr) {
		t.Fatalf("oversized body reached the cache: %v", statErr)
	}
	if _, statErr := os.Stat(filepath.Join(dataDir, "config.yaml")); !os.IsNotExist(statErr) {
		t.Fatalf("a config was written from the oversized body: %v", statErr)
	}
}

// The explicit Refresh tolerates transient fetch failures, but an oversized
// body must reach the UI; LastUpdated still advances so the auto-update loop
// does not re-download it every minute.
func TestRefreshProfileSubscriptionSurfacesOversizedBody(t *testing.T) {
	small := realisticSubscriptionYAML(10)
	var body atomic.Pointer[[]byte]
	body.Store(&small)
	url := subscriptionServer(t, &body)
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
	cached := readSubscriptionBodyCache(dataDir)
	if !bytes.Equal(cached, small) {
		t.Fatalf("normal refresh cached %d bytes, want %d", len(cached), len(small))
	}

	over := padYAML(t, small, subscriptionMaxBodyBytes+1)
	body.Store(&over)
	a.profiles[0].LastUpdated = 0
	state, err := a.refreshProfileSubscription(profileID, false)
	if err == nil || err.Error() != "subscription response exceeds the 32 MiB limit" {
		t.Fatalf("oversized refresh: %v", err)
	}
	if len(state.Profile.Profiles) != 1 || state.Profile.Profiles[0].LastUpdated == 0 {
		t.Fatalf("LastUpdated not advanced: %+v", state.Profile.Profiles)
	}
	if !bytes.Equal(readSubscriptionBodyCache(dataDir), small) {
		t.Fatal("oversized refresh replaced the last-known-good cache")
	}
}
