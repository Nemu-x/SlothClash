package main

import (
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"sync/atomic"
)

// The Desync report and attempt history show probe targets (custom domains,
// voice endpoints) only as a keyed digest: HMAC-SHA256 under a random
// per-install secret, truncated to 6 bytes. The secret lives in prefs.json
// next to the other prefs but outside DesktopPrefs, so it never reaches the
// UI (GetDesktopPrefs and the setters return DesktopPrefs) and never appears
// in a report. Digests are stable on one install, so a report still matches
// an entry with its attempts and flows; without the secret a reader cannot
// test a guess ("is this youtube.com?") against them.

// desktopPrefsFile is prefs.json's on-disk shape: DesktopPrefs plus fields
// that stay in the backend.
type desktopPrefsFile struct {
	DesktopPrefs
	DesyncReportKey string `json:"desyncReportKey,omitempty"`
	// LegacyDesync is the global Desync settings of builds before they
	// moved into the profiles (desync_profile.go), kept until moved.
	LegacyDesync json.RawMessage `json:"desync,omitempty"`
}

// prefsDesyncReportKey is the hex secret, saved with prefsCurrent. It is set
// only under prefsMu (load, first use); atomic so a save outside the lock
// (tests) reads it safely.
var prefsDesyncReportKey atomic.Value // string

func storedDesyncReportKey() string {
	s, _ := prefsDesyncReportKey.Load().(string)
	return s
}

// desyncReportKey returns the install's report secret, generating and saving
// it on first use.
func desyncReportKey() []byte {
	if key, err := hex.DecodeString(storedDesyncReportKey()); err == nil && len(key) == 32 {
		return key
	}
	prefsMu.Lock()
	defer prefsMu.Unlock()
	if key, err := hex.DecodeString(storedDesyncReportKey()); err == nil && len(key) == 32 {
		return key // generated meanwhile
	}
	key := make([]byte, 32)
	_, _ = rand.Read(key) // never fails (crypto/rand, Go 1.24+)
	prefsDesyncReportKey.Store(hex.EncodeToString(key))
	savePrefsBestEffort(prefsCurrent)
	return key
}

// desyncReportDigest is value's keyed digest under key (12 hex characters).
func desyncReportDigest(key []byte, value string) string {
	mac := hmac.New(sha256.New, key)
	mac.Write([]byte("sloth-desync-report-v1\n" + value))
	return hex.EncodeToString(mac.Sum(nil)[:6])
}
