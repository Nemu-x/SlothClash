package main

import "testing"

func TestCurrentBootLogIgnoresStaleFailure(t *testing.T) {
	t.Parallel()
	// A stale failure from a previous boot, then a fresh boot that succeeds.
	// currentBootLog must drop everything before the last boot marker so the
	// old "start tun listening error" can't produce a false failure verdict.
	log := "Start initial configuration in progress\n" +
		"Start TUN listening error: configure tun interface: operation not permitted\n" +
		"Start initial configuration in progress\n" +
		"[TUN] Tun adapter listening at: utun5([198.18.0.1/30],[]), mtu: 9000\n"
	if got := scanTunBringUpLog(currentBootLog(log)); got != tunVerifyUp {
		t.Fatalf("scoped scan = %v, want tunVerifyUp (stale failure must be ignored)", got)
	}
	// And a genuine failure in the current boot is still caught.
	failLog := "Start initial configuration in progress\n" +
		"Start TUN listening error: configure tun interface: operation not permitted\n"
	if got := scanTunBringUpLog(currentBootLog(failLog)); got != tunVerifyFailed {
		t.Fatalf("current-boot failure = %v, want tunVerifyFailed", got)
	}
}

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
		name     string
		log      string
		recovery tunRecoverOutcome
		wantSub  string
	}{
		{"anyconnect", "Start TUN listening error: configure tun interface: operation not permitted", tunRecoverNotAttempted, "AnyConnect"},
		{"access-denied-removed", "configure tun interface: Access is denied.", tunRecoverRemoved, "asked the service to remove it"},
		{"access-denied-outdated", "configure tun interface: Access is denied.", tunRecoverServiceOutdated, "too old to clear it"},
		{"access-denied-failed", "configure tun interface: Access is denied.", tunRecoverFailed, "didn't go through"},
		{"in-use", "configure tun interface: the adapter already exists", tunRecoverNotAttempted, "already in use"},
		{"wintun", "failed to load wintun.dll", tunRecoverNotAttempted, "wintun driver"},
		{"generic", "some unexpected tun failure text", tunRecoverNotAttempted, "could not be brought up"},
	}
	for _, tc := range cases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			got := classifyTunFailure(tc.log, tc.recovery)
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
