package main

import (
	"net/url"
	"strings"
	"testing"
)

func TestNormalizeMieruSubscriptionURLPasswordContainsAt(t *testing.T) {
	t.Parallel()
	raw := "mieru://slothclash:1qaz@WSX3edc@136.244.111.222:9000"
	norm, err := normalizeSubscriptionURL(raw)
	if err != nil {
		t.Fatal(err)
	}
	u, err := url.Parse(norm)
	if err != nil {
		t.Fatal(err)
	}
	if u.User == nil {
		t.Fatal("expected userinfo")
	}
	if u.User.Username() != "slothclash" {
		t.Fatalf("username: got %q", u.User.Username())
	}
	pass, ok := u.User.Password()
	if !ok || pass != "1qaz@WSX3edc" {
		t.Fatalf("password: got %q ok=%v", pass, ok)
	}
	if u.Hostname() != "136.244.111.222" {
		t.Fatalf("hostname: got %q", u.Hostname())
	}
	if u.Port() != "9000" {
		t.Fatalf("port: got %q", u.Port())
	}
	if !strings.Contains(norm, "mieru://") {
		t.Fatalf("expected mieru scheme in %q", norm)
	}
}

func TestNormalizeMierusSubscriptionURLMixedCaseScheme(t *testing.T) {
	t.Parallel()
	raw := "MiErUs://u:p@h@10.0.0.1:8388"
	norm, err := normalizeSubscriptionURL(raw)
	if err != nil {
		t.Fatal(err)
	}
	u, err := url.Parse(norm)
	if err != nil {
		t.Fatal(err)
	}
	if u.Scheme != "mierus" {
		t.Fatalf("scheme: got %q", u.Scheme)
	}
	if u.Hostname() != "10.0.0.1" || u.Port() != "8388" {
		t.Fatalf("host/port: host=%q port=%q", u.Hostname(), u.Port())
	}
	pass, _ := u.User.Password()
	if pass != "p@h" {
		t.Fatalf("password: got %q", pass)
	}
}
