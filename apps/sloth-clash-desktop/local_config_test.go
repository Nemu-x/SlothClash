package main

import "testing"

func TestProfileHasLocalConfig(t *testing.T) {
	cases := []struct {
		name string
		p    Profile
		want bool
	}{
		{"local type", Profile{Type: "local"}, true},
		{"local type cased", Profile{Type: "Local"}, true},
		{"skip auto config", Profile{SkipAutoConfig: true}, true},
		{"url profile", Profile{URL: "https://example.com/sub"}, false},
		{"empty profile", Profile{}, false},
	}
	for _, c := range cases {
		if got := profileHasLocalConfig(c.p); got != c.want {
			t.Errorf("%s: profileHasLocalConfig = %v, want %v", c.name, got, c.want)
		}
	}
}
