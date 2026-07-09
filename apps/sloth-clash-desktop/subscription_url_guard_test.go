package main

import (
	"errors"
	"testing"
)

// A hostile page can open slothclash://install-config?url=… — the app must never
// be aimed at the user's LAN or at cloud metadata (audit finding SEC2).
func TestValidateUntrustedSubscriptionURL_BlocksInternalTargets(t *testing.T) {
	blocked := []string{
		"http://169.254.169.254/latest/meta-data/", // cloud metadata (link-local)
		"http://127.0.0.1:9090/configs",            // loopback (our own controller!)
		"http://localhost/admin",
		"http://192.168.1.1/",  // RFC1918 (home router)
		"http://10.0.0.5/sub",  // RFC1918
		"https://172.16.4.2/x", // RFC1918
		"http://[::1]/x",       // IPv6 loopback
		"http://0.0.0.0/x",     // unspecified
	}
	for _, raw := range blocked {
		if err := validateUntrustedSubscriptionURL(raw); !errors.Is(err, errSubscriptionURLPrivateTarget) {
			t.Errorf("%s: want errSubscriptionURLPrivateTarget, got %v", raw, err)
		}
	}
}

func TestValidateUntrustedSubscriptionURL_BlocksBadSchemes(t *testing.T) {
	for _, raw := range []string{"ftp://example.com/sub", "gopher://example.com", "javascript://x/"} {
		if err := validateUntrustedSubscriptionURL(raw); !errors.Is(err, errSubscriptionURLScheme) {
			t.Errorf("%s: want errSubscriptionURLScheme, got %v", raw, err)
		}
	}
}

func TestValidateUntrustedSubscriptionURL_AllowsPublicHosts(t *testing.T) {
	for _, raw := range []string{
		"https://sub.example.com/link?token=abc",
		"http://example.org/x",
		"https://8.8.8.8/sub", // public literal IP is fine
	} {
		if err := validateUntrustedSubscriptionURL(raw); err != nil {
			t.Errorf("%s: want allowed, got %v", raw, err)
		}
	}
}

// A user typing a self-hosted LAN subscription must keep working: the private-IP
// guard applies ONLY to deep-link-originated URLs, never to normalizeSubscriptionURL.
func TestNormalizeSubscriptionURL_StillAllowsPrivateHostsForTypedURLs(t *testing.T) {
	got, err := normalizeSubscriptionURL("http://192.168.1.10:8080/sub")
	if err != nil {
		t.Fatalf("self-hosted LAN subscription must remain importable, got %v", err)
	}
	if got == "" {
		t.Fatal("expected a normalized url")
	}
}

func TestNormalizeSubscriptionURL_RejectsUnsupportedScheme(t *testing.T) {
	if _, err := normalizeSubscriptionURL("ftp://example.com/sub"); !errors.Is(err, errSubscriptionURLScheme) {
		t.Errorf("want errSubscriptionURLScheme, got %v", err)
	}
}
