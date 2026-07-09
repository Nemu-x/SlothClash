package main

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"strings"

	"github.com/metacubex/age"
	"github.com/metacubex/age/armor"
)

// Age-encrypted subscription support.
//
// Some providers ship the subscription body encrypted with age
// (https://age-encryption.org, armor format) so node credentials are not
// readable in transit/at rest at the provider. The user stores the matching
// age identity (AGE-SECRET-KEY-…) on the profile; we decrypt right after the
// HTTP fetch, BEFORE caching/parsing, so the rest of the pipeline (cache,
// merge templates, editors, baselines) keeps seeing plain YAML. This mirrors
// ClashFest/CMFA (which hand the key to their embedded engine) adapted to our
// architecture where the Go pipeline — not the core — parses subscriptions.
// The library is the same fork the mihomo core embeds (component/age).

// errAgeKeyMissing is surfaced when a body is age-armored but the profile has
// no key configured. Kept actionable: it names where to fix it.
var errAgeKeyMissing = errors.New(
	"subscription is age-encrypted; set the AGE secret key in the profile settings",
)

// bodyLooksAgeEncrypted reports whether the payload starts with the age armor
// header (ignoring leading whitespace/BOM noise some origins prepend).
func bodyLooksAgeEncrypted(body []byte) bool {
	return bytes.HasPrefix(bytes.TrimLeft(body, " \t\r\n\xef\xbb\xbf"), []byte(armor.Header))
}

// decryptSubscriptionBodyIfAge returns the body unchanged when it is not age
// armor. Otherwise it decrypts with the given identity string; a missing or
// non-matching key yields a clear error. Never log the key.
func decryptSubscriptionBodyIfAge(body []byte, secretKey string) ([]byte, error) {
	if !bodyLooksAgeEncrypted(body) {
		return body, nil
	}
	secretKey = strings.TrimSpace(secretKey)
	if secretKey == "" {
		return nil, errAgeKeyMissing
	}
	identities, err := age.ParseIdentities(strings.NewReader(secretKey))
	if err != nil {
		return nil, fmt.Errorf("invalid AGE secret key on this profile: %w", err)
	}
	trimmed := bytes.TrimLeft(body, " \t\r\n\xef\xbb\xbf")
	r, err := age.Decrypt(armor.NewReader(bytes.NewReader(trimmed)), identities...)
	if err != nil {
		return nil, fmt.Errorf("could not decrypt age-encrypted subscription (wrong key?): %w", err)
	}
	out, err := io.ReadAll(io.LimitReader(r, 6<<20))
	if err != nil {
		return nil, fmt.Errorf("could not read decrypted subscription: %w", err)
	}
	return out, nil
}

// validateAgeSecretKey accepts anything age.ParseIdentities accepts (X25519
// and hybrid identities, multi-line identity files). Empty is NOT valid here —
// callers treat empty as "clear the key" before validating.
func validateAgeSecretKey(secretKey string) error {
	_, err := age.ParseIdentities(strings.NewReader(strings.TrimSpace(secretKey)))
	return err
}

// generateAgeKeyPair returns a fresh identity: the public recipient (given to
// the provider) and the secret identity (stored on the profile). kind selects
// the algorithm: "x25519" (default, classic age) or "hybrid"
// (MLKEM768-X25519, post-quantum; supported by the same fork the core embeds).
func generateAgeKeyPair(kind string) (publicKey string, secretKey string, err error) {
	switch strings.ToLower(strings.TrimSpace(kind)) {
	case "", "x25519":
		id, err := age.GenerateX25519Identity()
		if err != nil {
			return "", "", err
		}
		return id.Recipient().String(), id.String(), nil
	case "hybrid", "mlkem768-x25519", "mlkem768x25519":
		id, err := age.GenerateHybridIdentity()
		if err != nil {
			return "", "", err
		}
		return id.Recipient().String(), id.String(), nil
	default:
		return "", "", fmt.Errorf("unknown age key type %q", kind)
	}
}

// deriveAgePublicKeys re-derives the public recipient(s) from a stored secret
// identity so the UI can show/copy the public key at any time — not just right
// after generation. Mirrors mihomo's component/age ToPublicKeys.
func deriveAgePublicKeys(secretKey string) (string, error) {
	identities, err := age.ParseIdentities(strings.NewReader(strings.TrimSpace(secretKey)))
	if err != nil {
		return "", err
	}
	var pubs []string
	for _, id := range identities {
		switch id := id.(type) {
		case *age.X25519Identity:
			pubs = append(pubs, id.Recipient().String())
		case *age.HybridIdentity:
			pubs = append(pubs, id.Recipient().String())
		default:
			// scrypt/passphrase identities have no shareable recipient; skip.
		}
	}
	if len(pubs) == 0 {
		return "", errors.New("no public recipient derivable from this key")
	}
	return strings.Join(pubs, "\n"), nil
}
