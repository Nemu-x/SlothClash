package main

import (
	"net/netip"
	"os"
	"path/filepath"
	"testing"

	"gopkg.in/yaml.v3"
)

// The fake-ip v6 pool is only useful if the TUN routes it. Real-world regression
// (measured 2026-08-31): the subscription's own `route-exclude-address` carries
// `fc00::/7`, which swallows our injected ULA pool `fdfe:dcba:9876::1/64`, so
// mihomo's auto-route installs no route for it. Every AAAA answer then points at
// an address the OS sends out of the physical interface, where it dies — a full
// connect timeout on the IPv6 half of every dual-stack site.
// See dropUnroutableFakeIPRange6 and architecture/ipv6.md.

// profileWithTunRouteExcludes builds the representative full profile with an
// explicit tun route-exclude list (as real subscriptions ship it).
func profileWithTunRouteExcludes(excludes []any) map[string]any {
	m := representativeFullProfile()
	tun, _ := m["tun"].(map[string]any)
	tun["route-exclude-address"] = excludes
	return m
}

func runTunPipeline(t *testing.T, m map[string]any) map[string]any {
	t.Helper()
	if err := finalizeRuntimeConfigPipeline(m, t.TempDir(), 54333, 9097, "secret", "tun", true, true); err != nil {
		t.Fatalf("pipeline error: %v", err)
	}
	return dnsMap(t, m)
}

func TestFakeIPRange6DroppedWhenTunRoutingExcludesIt(t *testing.T) {
	withConnectionPrefs(t, ConnectionSettings{}) // IPv6 default ON
	m := profileWithTunRouteExcludes([]any{
		"224.0.0.0/3", "10.0.0.0/8", "127.0.0.0/8", "192.168.0.0/16",
		"fc00::/7", "ff00::/8", "fe80::/10",
	})
	d := runTunPipeline(t, m)

	if v, present := d["fake-ip-range6"]; present {
		t.Errorf("fake-ip-range6 = %v, want it dropped: fc00::/7 is route-excluded, so the pool would be a black hole", v)
	}
	// The v4 pool and the master IPv6 switch must be untouched — real IPv6
	// destinations still have to be captured by the tunnel (no leak).
	if d["fake-ip-range"] != "198.18.0.1/16" {
		t.Errorf("fake-ip-range = %v, want the v4 pool preserved", d["fake-ip-range"])
	}
	if m["ipv6"] != true || d["ipv6"] != true {
		t.Errorf("ipv6 = %v / dns.ipv6 = %v, want both true", m["ipv6"], d["ipv6"])
	}
}

func TestFakeIPRange6KeptWhenTunRoutesIt(t *testing.T) {
	withConnectionPrefs(t, ConnectionSettings{})
	// Same list minus the ULA exclusion: the pool is reachable, keep it.
	m := profileWithTunRouteExcludes([]any{"10.0.0.0/8", "192.168.0.0/16", "ff00::/8", "fe80::/10"})
	d := runTunPipeline(t, m)

	if v, _ := d["fake-ip-range6"].(string); v == "" {
		t.Errorf("fake-ip-range6 = %v, want the pool kept when nothing excludes it", d["fake-ip-range6"])
	}
}

func TestFakeIPRange6FromSubscriptionAlsoDroppedWhenUnroutable(t *testing.T) {
	withConnectionPrefs(t, ConnectionSettings{})
	m := profileWithTunRouteExcludes([]any{"fc00::/7"})
	dnsMap(t, m)["fake-ip-range6"] = "fdfe:1234::1/64" // the subscription's own pool
	d := runTunPipeline(t, m)

	if v, present := d["fake-ip-range6"]; present {
		t.Errorf("fake-ip-range6 = %v, want a subscription-supplied pool dropped too when it is route-excluded", v)
	}
}

func TestFakeIPRange6KeptWhenTunInet6AddressCoversIt(t *testing.T) {
	withConnectionPrefs(t, ConnectionSettings{})
	m := profileWithTunRouteExcludes([]any{"fc00::/7"})
	m["tun"].(map[string]any)["inet6-address"] = []any{"fdfe:dcba:9876::1/64"}
	d := runTunPipeline(t, m)

	if v, _ := d["fake-ip-range6"].(string); v == "" {
		t.Errorf("fake-ip-range6 = %v, want it kept: an on-link tun inet6-address makes the pool reachable", d["fake-ip-range6"])
	}
}

func TestPrefixCovers(t *testing.T) {
	mk := func(s string) netip.Prefix {
		p, err := netip.ParsePrefix(s)
		if err != nil {
			t.Fatalf("bad prefix %q: %v", s, err)
		}
		return p.Masked()
	}
	cases := []struct {
		outer, inner string
		want         bool
	}{
		{"fc00::/7", "fdfe:dcba:9876::1/64", true},
		{"fe80::/10", "fdfe:dcba:9876::1/64", false},
		{"fdfe:dcba:9876::/64", "fdfe:dcba:9876::/64", true},
		{"fdfe:dcba:9876::/126", "fdfe:dcba:9876::/64", false}, // narrower can't cover wider
		{"10.0.0.0/8", "fdfe:dcba:9876::1/64", false},          // family mismatch
		{"0.0.0.0/0", "198.18.0.0/16", true},
	}
	for _, c := range cases {
		if got := prefixCovers(mk(c.outer), mk(c.inner)); got != c.want {
			t.Errorf("prefixCovers(%s, %s) = %v, want %v", c.outer, c.inner, got, c.want)
		}
	}
}

// repairRuntimeConfigDNS is the last writer of config.yaml before the core reads
// it, and its self-heal re-fills the v6 pool — so the guard must run there too or
// the pipeline's decision is silently undone on disk.
func TestRepairRuntimeConfigDNSKeepsTheGuardDecision(t *testing.T) {
	withConnectionPrefs(t, ConnectionSettings{})
	dir := t.TempDir()
	cfgPath := filepath.Join(dir, "config.yaml")
	written := `mixed-port: 7890
mode: rule
ipv6: true
tun:
  enable: true
  stack: gvisor
  auto-route: true
  route-exclude-address:
    - 192.168.0.0/16
    - fc00::/7
dns:
  enable: true
  listen: ":1053"
  ipv6: true
  enhanced-mode: fake-ip
  fake-ip-range: 198.18.0.1/16
  fake-ip-range6: fdfe:dcba:9876::1/64
rules:
  - MATCH,DIRECT
`
	if err := os.WriteFile(cfgPath, []byte(written), 0o644); err != nil {
		t.Fatalf("write config: %v", err)
	}
	if err := repairRuntimeConfigDNS(cfgPath); err != nil {
		t.Fatalf("repair: %v", err)
	}
	b, err := os.ReadFile(cfgPath)
	if err != nil {
		t.Fatalf("read back: %v", err)
	}
	var m map[string]any
	if err := yaml.Unmarshal(b, &m); err != nil {
		t.Fatalf("reparse: %v", err)
	}
	d, _ := m["dns"].(map[string]any)
	if v, present := d["fake-ip-range6"]; present {
		t.Errorf("fake-ip-range6 = %v on disk after repair, want it dropped (fc00::/7 is route-excluded)", v)
	}
	if d["fake-ip-range"] != "198.18.0.1/16" {
		t.Errorf("fake-ip-range = %v, want the v4 pool preserved", d["fake-ip-range"])
	}
}
