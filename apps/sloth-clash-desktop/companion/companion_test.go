package companion

import (
	"sync"
	"testing"
)

// fakeTokens is an in-memory TokenStore for tests (no OS keychain).
type fakeTokens struct {
	mu sync.Mutex
	m  map[string]string
}

func newFakeTokens() *fakeTokens { return &fakeTokens{m: map[string]string{}} }
func (f *fakeTokens) Get(id string) (string, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	t, ok := f.m[id]
	if !ok {
		return "", ErrTokenNotFound
	}
	return t, nil
}
func (f *fakeTokens) Set(id, t string) error { f.mu.Lock(); f.m[id] = t; f.mu.Unlock(); return nil }
func (f *fakeTokens) Delete(id string) error { f.mu.Lock(); delete(f.m, id); f.mu.Unlock(); return nil }
func (f *fakeTokens) has(id string) bool {
	f.mu.Lock()
	defer f.mu.Unlock()
	_, ok := f.m[id]
	return ok
}

// A valid clashctl-pair URI from clash-companion/vectors/pairing.json.
const validPairURI = "clashctl-pair://192.168.1.50:8443?id=AAECAwQFBgcICQoLDA0ODw&name=Living%20Room%20TV&app=slothclash&fp=0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef&token=c2FtcGxlLXRva2Vu"

func TestStoreRoundTripAndDeviceID(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	s, err := LoadStore(dir)
	if err != nil {
		t.Fatal(err)
	}
	if s.DeviceID() == "" {
		t.Fatal("expected a generated deviceId")
	}
	if err := s.Upsert(Agent{DeviceID: "dev1", Name: "TV", App: "clashfest", FP: "fp", LastHost: "1.2.3.4", LastPort: 8443}); err != nil {
		t.Fatal(err)
	}
	// Reload from disk and confirm persistence (deviceId + agent).
	s2, err := LoadStore(dir)
	if err != nil {
		t.Fatal(err)
	}
	if s2.DeviceID() != s.DeviceID() {
		t.Fatalf("deviceId not persisted: %q vs %q", s2.DeviceID(), s.DeviceID())
	}
	a, ok := s2.Get("dev1")
	if !ok || a.LastPort != 8443 || a.Name != "TV" {
		t.Fatalf("agent not persisted: %#v ok=%v", a, ok)
	}
	if err := s2.Delete("dev1"); err != nil {
		t.Fatal(err)
	}
	if _, ok := s2.Get("dev1"); ok {
		t.Fatal("agent should be gone after delete")
	}
}

func TestPairByStringInvalid(t *testing.T) {
	t.Parallel()
	m, err := NewManager(t.TempDir(), newFakeTokens())
	if err != nil {
		t.Fatal(err)
	}
	if _, err := m.PairByString("not-a-pairing-string"); err == nil {
		t.Fatal("expected error for malformed pairing string")
	}
}

func TestPairByStringValidThenUnpair(t *testing.T) {
	t.Parallel()
	toks := newFakeTokens()
	m, err := NewManager(t.TempDir(), toks)
	if err != nil {
		t.Fatal(err)
	}
	info, err := m.PairByString(validPairURI)
	if err != nil {
		t.Fatalf("pair by string: %v", err)
	}
	if info.DeviceID != "AAECAwQFBgcICQoLDA0ODw" || info.Name != "Living Room TV" || info.App != "slothclash" {
		t.Fatalf("unexpected agent info: %#v", info)
	}
	if !toks.has(info.DeviceID) {
		t.Fatal("token should be stored after pairing")
	}
	// Token value must be the one carried by the pairing string.
	if got, _ := toks.Get(info.DeviceID); got != "c2FtcGxlLXRva2Vu" {
		t.Fatalf("stored token mismatch: %q", got)
	}
	if _, ok := m.store.Get(info.DeviceID); !ok {
		t.Fatal("agent should be in the store")
	}

	if err := m.Unpair(info.DeviceID); err != nil {
		t.Fatal(err)
	}
	if toks.has(info.DeviceID) {
		t.Fatal("token should be deleted after unpair")
	}
	if _, ok := m.store.Get(info.DeviceID); ok {
		t.Fatal("agent should be removed after unpair")
	}
}

func TestPowerRejectsInvalidAction(t *testing.T) {
	t.Parallel()
	m, err := NewManager(t.TempDir(), newFakeTokens())
	if err != nil {
		t.Fatal(err)
	}
	if _, err := m.Power(t.Context(), "dev1", "explode"); err == nil {
		t.Fatal("expected invalid power action to be rejected")
	}
}
