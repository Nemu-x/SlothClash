package main

import (
	"crypto/ed25519"
	"crypto/rand"
	"encoding/base64"
	"testing"

	"golang.org/x/crypto/blake2b"
)

// buildTestKeypair returns a trusted-keys entry (base64 .pub value) plus the
// matching private key and key id.
func buildTestKeypair(t *testing.T) (pubB64 string, priv ed25519.PrivateKey, keyID [8]byte) {
	t.Helper()
	pub, priv, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatalf("genkey: %v", err)
	}
	if _, err := rand.Read(keyID[:]); err != nil {
		t.Fatalf("keyid: %v", err)
	}
	raw := append([]byte("Ed"), keyID[:]...)
	raw = append(raw, pub...)
	return base64.StdEncoding.EncodeToString(raw), priv, keyID
}

// makeMinisig builds a .minisig file body for message using algo "Ed" or "ED".
func makeMinisig(algo string, priv ed25519.PrivateKey, keyID [8]byte, message []byte, trustedComment string) []byte {
	signed := message
	if algo == "ED" {
		h := blake2b.Sum512(message)
		signed = h[:]
	}
	sig := ed25519.Sign(priv, signed)

	sigBin := append([]byte(algo), keyID[:]...)
	sigBin = append(sigBin, sig...)

	global := ed25519.Sign(priv, append(append([]byte(nil), sig...), []byte(trustedComment)...))

	body := "untrusted comment: signature from test\n" +
		base64.StdEncoding.EncodeToString(sigBin) + "\n" +
		"trusted comment: " + trustedComment + "\n" +
		base64.StdEncoding.EncodeToString(global) + "\n"
	return []byte(body)
}

func TestVerifyMinisign(t *testing.T) {
	pubB64, priv, keyID := buildTestKeypair(t)
	trusted := []string{pubB64}
	msg := []byte("0123abc  SlothClash-installer-amd64.exe\n")

	t.Run("valid Ed", func(t *testing.T) {
		sig := makeMinisig("Ed", priv, keyID, msg, "file:SHA256SUMS")
		if err := verifyMinisign(msg, sig, trusted); err != nil {
			t.Fatalf("want valid, got %v", err)
		}
	})

	t.Run("valid ED prehashed", func(t *testing.T) {
		sig := makeMinisig("ED", priv, keyID, msg, "file:SHA256SUMS")
		if err := verifyMinisign(msg, sig, trusted); err != nil {
			t.Fatalf("want valid, got %v", err)
		}
	})

	t.Run("tampered message", func(t *testing.T) {
		sig := makeMinisig("Ed", priv, keyID, msg, "file:SHA256SUMS")
		if err := verifyMinisign([]byte("deadbeef  evil.exe\n"), sig, trusted); err == nil {
			t.Fatal("want failure on tampered message")
		}
	})

	t.Run("untrusted key", func(t *testing.T) {
		otherPub, otherPriv, otherID := buildTestKeypair(t)
		_ = otherPub
		sig := makeMinisig("Ed", otherPriv, otherID, msg, "file:SHA256SUMS")
		if err := verifyMinisign(msg, sig, trusted); err == nil {
			t.Fatal("want failure when signed by an untrusted key")
		}
	})

	t.Run("wrong key same id", func(t *testing.T) {
		// Same key id as trusted, but a different private key → must fail.
		_, otherPriv, _ := buildTestKeypair(t)
		sig := makeMinisig("Ed", otherPriv, keyID, msg, "file:SHA256SUMS")
		if err := verifyMinisign(msg, sig, trusted); err == nil {
			t.Fatal("want failure when key id matches but signature is from another key")
		}
	})

	t.Run("tampered trusted comment", func(t *testing.T) {
		sig := makeMinisig("Ed", priv, keyID, msg, "file:SHA256SUMS")
		bad := append([]byte(nil), sig...)
		// Flip a byte in the trusted comment region by rebuilding with a different
		// comment but the original global signature is now invalid.
		tampered := makeMinisig("Ed", priv, keyID, msg, "file:SHA256SUMS")
		// Swap line 2 (trusted comment) for a different value while keeping the
		// original global signature line.
		_ = bad
		lines := splitLines(tampered)
		origLines := splitLines(sig)
		lines[2] = "trusted comment: file:EVIL"
		lines[3] = origLines[3] // keep original global sig → mismatch
		if err := verifyMinisign(msg, joinLines(lines), trusted); err == nil {
			t.Fatal("want failure when trusted comment is tampered")
		}
	})

	t.Run("malformed", func(t *testing.T) {
		if err := verifyMinisign(msg, []byte("not a signature"), trusted); err == nil {
			t.Fatal("want failure on malformed signature")
		}
	})
}

func TestTrustedUpdateKeysParse(t *testing.T) {
	if len(trustedUpdateKeys) == 0 {
		t.Fatal("no trusted update keys embedded")
	}
	for i, k := range trustedUpdateKeys {
		if _, err := parseMinisignPubKey(k); err != nil {
			t.Fatalf("trusted key %d does not parse: %v", i, err)
		}
	}
}

func splitLines(b []byte) []string {
	var out []string
	cur := ""
	for _, c := range string(b) {
		if c == '\n' {
			out = append(out, cur)
			cur = ""
			continue
		}
		cur += string(c)
	}
	if cur != "" {
		out = append(out, cur)
	}
	return out
}

func joinLines(lines []string) []byte {
	s := ""
	for _, l := range lines {
		s += l + "\n"
	}
	return []byte(s)
}
