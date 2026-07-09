package main

import (
	"bytes"
	"strings"
	"testing"

	"github.com/metacubex/age"
	"github.com/metacubex/age/armor"
)

const agePlainYAML = "proxies:\n  - name: n1\n    type: ss\n"

// encryptWithLib produces a real age armor payload for the given recipient.
func encryptWithLib(t *testing.T, recipient age.Recipient) []byte {
	t.Helper()
	var buf bytes.Buffer
	aw := armor.NewWriter(&buf)
	w, err := age.Encrypt(aw, recipient)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := w.Write([]byte(agePlainYAML)); err != nil {
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

func TestDecryptSubscriptionBodyRoundTrip(t *testing.T) {
	t.Parallel()
	id, err := age.GenerateX25519Identity()
	if err != nil {
		t.Fatal(err)
	}
	enc := encryptWithLib(t, id.Recipient())
	out, err := decryptSubscriptionBodyIfAge(enc, id.String())
	if err != nil {
		t.Fatalf("decrypt: %v", err)
	}
	if string(out) != agePlainYAML {
		t.Fatalf("round-trip mismatch: %q", out)
	}
}

func TestDecryptSubscriptionBodyPlainPassthrough(t *testing.T) {
	t.Parallel()
	// Plain body passes through byte-identical, with or without a key.
	for _, key := range []string{"", "AGE-SECRET-KEY-INVALIDBUTUNUSED"} {
		out, err := decryptSubscriptionBodyIfAge([]byte(agePlainYAML), key)
		if err != nil {
			t.Fatalf("plain passthrough (key=%q): %v", key, err)
		}
		if string(out) != agePlainYAML {
			t.Fatalf("plain body modified (key=%q)", key)
		}
	}
}

func TestDecryptSubscriptionBodyMissingKey(t *testing.T) {
	t.Parallel()
	id, err := age.GenerateX25519Identity()
	if err != nil {
		t.Fatal(err)
	}
	enc := encryptWithLib(t, id.Recipient())
	_, err = decryptSubscriptionBodyIfAge(enc, "")
	if err == nil || !strings.Contains(err.Error(), "profile settings") {
		t.Fatalf("expected actionable missing-key error, got: %v", err)
	}
}

func TestDecryptSubscriptionBodyWrongKey(t *testing.T) {
	t.Parallel()
	idA, _ := age.GenerateX25519Identity()
	idB, _ := age.GenerateX25519Identity()
	enc := encryptWithLib(t, idA.Recipient())
	_, err := decryptSubscriptionBodyIfAge(enc, idB.String())
	if err == nil || !strings.Contains(strings.ToLower(err.Error()), "decrypt") {
		t.Fatalf("expected decrypt error for wrong key, got: %v", err)
	}
}

func TestValidateAndGenerateAgeKeyPair(t *testing.T) {
	t.Parallel()
	pub, sec, err := generateAgeKeyPair("x25519")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(pub, "age1") || !strings.HasPrefix(sec, "AGE-SECRET-KEY-") {
		t.Fatalf("unexpected pair formats: pub=%q sec=%q", pub, sec)
	}
	if err := validateAgeSecretKey(sec); err != nil {
		t.Fatalf("generated key must validate: %v", err)
	}
	if err := validateAgeSecretKey("not-a-key"); err == nil {
		t.Fatal("expected invalid key to be rejected")
	}
	// Hybrid (MLKEM768-X25519) pair also validates and derives.
	hpub, hsec, err := generateAgeKeyPair("hybrid")
	if err != nil {
		t.Fatal(err)
	}
	if err := validateAgeSecretKey(hsec); err != nil {
		t.Fatalf("hybrid key must validate: %v", err)
	}
	if got, err := deriveAgePublicKeys(hsec); err != nil || got != hpub {
		t.Fatalf("hybrid derive: got %q err %v, want %q", got, err, hpub)
	}
	if _, _, err := generateAgeKeyPair("nonsense"); err == nil {
		t.Fatal("expected unknown kind to be rejected")
	}
}

func TestDeriveAgePublicKeys(t *testing.T) {
	t.Parallel()
	pub, sec, err := generateAgeKeyPair("x25519")
	if err != nil {
		t.Fatal(err)
	}
	got, err := deriveAgePublicKeys(sec)
	if err != nil {
		t.Fatal(err)
	}
	if got != pub {
		t.Fatalf("derived %q, want %q", got, pub)
	}
	if _, err := deriveAgePublicKeys("garbage"); err == nil {
		t.Fatal("expected error for invalid secret")
	}
}
