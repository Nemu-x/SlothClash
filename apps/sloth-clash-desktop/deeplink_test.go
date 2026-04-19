package main

import "testing"

func TestParseInstallConfigURL(t *testing.T) {
	name, u, err := ParseInstallConfigURL(
		"slothclash://install-config?name=MySub&url=https%3A%2F%2Fexample.com%2Fa%3Ft%3D1",
	)
	if err != nil {
		t.Fatal(err)
	}
	if name != "MySub" {
		t.Fatalf("name: got %q", name)
	}
	if u != "https://example.com/a?t=1" {
		t.Fatalf("url: got %q", u)
	}

	name2, u2, err := ParseInstallConfigURL("slothclash:///install-config?url=https%3A%2F%2Fa.test%2F")
	if err != nil {
		t.Fatal(err)
	}
	if name2 != "" {
		t.Fatalf("name2: got %q want empty", name2)
	}
	if u2 != "https://a.test/" {
		t.Fatalf("url2: got %q", u2)
	}
}
