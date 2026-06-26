package main

import "testing"

func TestScanTunBringUpLog(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name string
		log  string
		want tunVerifyResult
	}{
		{
			name: "success",
			log: "time=\"...\" level=info msg=\"[TUN] Tun adapter listening at: Meta([198.18.0.1/30])\"\n" +
				"time=\"...\" level=info msg=\"DNS server listening at: :1053\"",
			want: tunVerifyUp,
		},
		{
			name: "operation-not-permitted",
			log: "time=\"...\" level=error msg=\"Start TUN listening error: configure tun interface: " +
				"Connect: operation not permitted\"",
			want: tunVerifyFailed,
		},
		{
			name: "configure-interface-error-only",
			log:  "level=error msg=\"configure tun interface: The system cannot find the file specified\"",
			want: tunVerifyFailed,
		},
		{
			name: "no-marker",
			log:  "level=info msg=\"Start initial Compatible provider default\"",
			want: tunVerifyUnknown,
		},
		{
			name: "transient-error-then-success",
			log: "level=error msg=\"Start TUN listening error: configure tun interface: in use\"\n" +
				"level=info msg=\"[TUN] Tun adapter listening at: Meta([198.18.0.1/30])\"",
			want: tunVerifyUp,
		},
		{
			name: "success-then-later-failure",
			log: "level=info msg=\"[TUN] Tun adapter listening at: Meta\"\n" +
				"level=error msg=\"Start TUN listening error: operation not permitted\"",
			want: tunVerifyFailed,
		},
	}
	for _, tc := range cases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			if got := scanTunBringUpLog(tc.log); got != tc.want {
				t.Fatalf("scanTunBringUpLog(%s) = %d, want %d", tc.name, got, tc.want)
			}
		})
	}
}

func TestClassifyTunFailure(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name    string
		log     string
		wantSub string
	}{
		{"anyconnect", "Start TUN listening error: configure tun interface: operation not permitted", "AnyConnect"},
		{"access-denied", "configure tun interface: Access is denied.", "Access was denied"},
		{"in-use", "configure tun interface: the adapter already exists", "already in use"},
		{"wintun", "failed to load wintun.dll", "wintun driver"},
		{"generic", "some unexpected tun failure text", "could not be brought up"},
	}
	for _, tc := range cases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			got := classifyTunFailure(tc.log)
			if !containsFold(got, tc.wantSub) {
				t.Fatalf("classifyTunFailure(%s) = %q, want substring %q", tc.name, got, tc.wantSub)
			}
		})
	}
}

func containsFold(s, sub string) bool {
	return len(sub) == 0 || (len(s) >= len(sub) && indexFold(s, sub) >= 0)
}

func indexFold(s, sub string) int {
	ls, lsub := toLowerASCII(s), toLowerASCII(sub)
	for i := 0; i+len(lsub) <= len(ls); i++ {
		if ls[i:i+len(lsub)] == lsub {
			return i
		}
	}
	return -1
}

func toLowerASCII(s string) string {
	b := []byte(s)
	for i := range b {
		if b[i] >= 'A' && b[i] <= 'Z' {
			b[i] += 'a' - 'A'
		}
	}
	return string(b)
}
