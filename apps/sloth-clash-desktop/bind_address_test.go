package main

import "testing"

func TestIsLoopbackBindAddress(t *testing.T) {
	cases := []struct {
		in   string
		want bool
	}{
		// Loopback — allow-lan would be silently defeated by these.
		{"127.0.0.1", true},
		{"127.0.0.2", true},
		{"localhost", true},
		{"LocalHost", true},
		{"::1", true},
		{"[::1]", true},
		{"  127.0.0.1  ", true},
		// Abbreviated IPv4 forms that net.ParseIP rejects but resolvers accept.
		{"127.1", true},
		{"127.0.1", true},

		// Not loopback — listener already reachable from the LAN.
		{"", false},
		{"*", false},
		{"0.0.0.0", false},
		{"::", false},
		{"[::]", false},
		{"192.168.1.10", false},
		{"10.0.0.1", false},
		{"example.com", false},
		{"128.0.0.1", false},
		// Malformed shorthand must not be mistaken for loopback.
		{"127.abc", false},
		{"127", false},
	}
	for _, tc := range cases {
		t.Run(tc.in, func(t *testing.T) {
			if got := isLoopbackBindAddress(tc.in); got != tc.want {
				t.Fatalf("isLoopbackBindAddress(%q) = %v, want %v", tc.in, got, tc.want)
			}
		})
	}
}
