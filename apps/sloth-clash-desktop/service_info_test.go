package main

import "testing"

func TestCompareServiceVersions(t *testing.T) {
	cases := []struct {
		a, b string
		want int
	}{
		{"0.1.0", "0.2.0", -1}, // old service vs required → update
		{"2.2.0", "2.3.0", -1}, // real case: pre-fix service vs LPE-hardened release
		{"2.3.0", "2.3.0", 0},  // real case: on the fixed release → no nag
		{"0.2.0", "0.2.0", 0},
		{"0.2.1", "0.2.0", 1},
		{"1.0.0", "0.2.0", 1},
		{"v0.2.0", "0.2.0", 0},          // leading v tolerated
		{"0.2.0-rc1", "0.2.0", 0},       // prerelease suffix ignored (core equal)
		{"0.2.0+build.5", "0.2.0", 0},   // build metadata ignored
		{"0.2", "0.2.0", 0},             // short form → missing segment = 0
		{"garbage", "0.2.0", -1},        // unparseable → 0.0.0 < expected
		{"", "0.2.0", -1},
	}
	for _, c := range cases {
		if got := compareServiceVersions(c.a, c.b); got != c.want {
			t.Errorf("compareServiceVersions(%q,%q) = %d, want %d", c.a, c.b, got, c.want)
		}
	}
}

// A malformed/garbage version must never read as NEWER than expected (which
// would suppress the update prompt).
func TestCompareServiceVersions_GarbageNeverNewer(t *testing.T) {
	if compareServiceVersions("not-a-version", expectedSlothServiceVersion) > 0 {
		t.Fatal("garbage version must not compare newer than expected")
	}
}
