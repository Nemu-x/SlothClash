package runtimecfg

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"sync/atomic"
	"testing"
	"time"

	"SlothClashDesktop/internal/boundedio"
	"SlothClashDesktop/internal/runtimecfg/runtimecfgtest"

	"github.com/metacubex/age"
	"github.com/metacubex/age/armor"
)

const realisticSubscriptionNodes = 10000

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

func fetchTestSubscription(t *testing.T, url, ageKey string) ([]byte, error) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	body, _, err := FetchSubscriptionBody(ctx, url, ageKey, SubscriptionClient{})
	return body, err
}

func assertSubscriptionNodes(t *testing.T, body []byte, want int) {
	t.Helper()
	doc, err := ParseClashDoc(body)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	proxies, _ := doc["proxies"].([]any)
	if len(proxies) != want || !IsFullProfile(doc) {
		t.Fatalf("parsed %d proxies (want %d), full=%t", len(proxies), want, IsFullProfile(doc))
	}
}

func TestRealisticSubscriptionFitsBodyCap(t *testing.T) {
	body := runtimecfgtest.RealisticSubscriptionYAML(realisticSubscriptionNodes)
	t.Logf("%d-node subscription: %d bytes (%.2f MiB), %d B/node; cap %s",
		realisticSubscriptionNodes, len(body), float64(len(body))/(1<<20), len(body)/realisticSubscriptionNodes,
		boundedio.FormatLimit(subscriptionMaxBodyBytes))
	if len(body) <= 6<<20 {
		t.Fatalf("fixture (%d bytes) must exceed the former 6 MiB cap it is meant to justify replacing", len(body))
	}
	if int64(len(body))*3 > subscriptionMaxBodyBytes {
		t.Fatalf("a %d-node subscription (%d bytes) leaves less than 3x headroom under %s",
			realisticSubscriptionNodes, len(body), boundedio.FormatLimit(subscriptionMaxBodyBytes))
	}
	assertSubscriptionNodes(t, body, realisticSubscriptionNodes)
}

func TestFetchSubscriptionBodyAtAndOverLimit(t *testing.T) {
	yaml := runtimecfgtest.RealisticSubscriptionYAML(realisticSubscriptionNodes)
	var body atomic.Pointer[[]byte]
	url := runtimecfgtest.ServeSubscription(t, &body)

	at := runtimecfgtest.PadYAML(t, yaml, subscriptionMaxBodyBytes)
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
		over := runtimecfgtest.PadYAML(t, yaml, tc.size)
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
	yaml := runtimecfgtest.RealisticSubscriptionYAML(realisticSubscriptionNodes)
	var body atomic.Pointer[[]byte]
	url := runtimecfgtest.ServeSubscription(t, &body)

	at := ageArmor(t, id.Recipient(), runtimecfgtest.PadYAML(t, yaml, subscriptionMaxBodyBytes))
	t.Logf("age armor of a %s body: %d bytes (%.3fx), wire cap %s",
		boundedio.FormatLimit(subscriptionMaxBodyBytes), len(at), float64(len(at))/subscriptionMaxBodyBytes,
		boundedio.FormatLimit(subscriptionMaxWireBytes))
	if int64(len(at)) > subscriptionMaxWireBytes {
		t.Fatalf("an armored body at the content cap (%d bytes) does not fit the wire cap", len(at))
	}
	body.Store(&at)
	got, err := fetchTestSubscription(t, url, id.String())
	if err != nil || len(got) != subscriptionMaxBodyBytes {
		t.Fatalf("at limit: %d bytes, %v", len(got), err)
	}
	assertSubscriptionNodes(t, got, realisticSubscriptionNodes)

	over := ageArmor(t, id.Recipient(), runtimecfgtest.PadYAML(t, yaml, subscriptionMaxBodyBytes+1))
	body.Store(&over)
	got, err = fetchTestSubscription(t, url, id.String())
	if err == nil || err.Error() != "decrypted subscription response exceeds the 32 MiB limit" || got != nil {
		t.Fatalf("oversized plaintext: %d bytes, %v", len(got), err)
	}
}

// An oversized body on a first Connect must fail loudly, not fall back to the
// bare provider profile that hides why the groups and rules are gone.
func TestFullProfilePipelineRefusesOversizedSubscription(t *testing.T) {
	over := runtimecfgtest.PadYAML(t, runtimecfgtest.RealisticSubscriptionYAML(100), subscriptionMaxBodyBytes+1)
	var body atomic.Pointer[[]byte]
	body.Store(&over)
	url := runtimecfgtest.ServeSubscription(t, &body)
	dataDir := t.TempDir()

	outcome, err := tryWriteMergedFullProfile(Input{
		DataDir:                dataDir,
		SubURL:                 url,
		CtrlPort:               9090,
		MixedPort:              7890,
		Secret:                 "s",
		WithExternalController: true,
		Traffic:                "rule",
		EnableTun:              false,
		Env:                    testEnv(),
	}, nil)
	if outcome != pipelineBodyTooLarge || outcome.useBareFallback() || !boundedio.IsLimitError(err) {
		t.Fatalf("outcome %s, err %v", outcome, err)
	}
	if _, statErr := os.Stat(SubscriptionBodyCachePath(dataDir)); !os.IsNotExist(statErr) {
		t.Fatalf("oversized body reached the cache: %v", statErr)
	}
	if _, statErr := os.Stat(filepath.Join(dataDir, "config.yaml")); !os.IsNotExist(statErr) {
		t.Fatalf("a config was written from the oversized body: %v", statErr)
	}
}
