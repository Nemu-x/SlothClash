package runtimecfg

import (
	"runtime"
	"testing"

	"SlothClashDesktop/internal/runtimecfg/runtimecfgtest"
)

// Parity invariants for a subscription that ships its own dns/tun blocks, run
// through the real finalizer pipeline (TUN mode). These lock the reconciled
// verge-parity behavior so a future change can't silently drift.
func TestConfigParitySubscriptionBlocksPreserved(t *testing.T) {
	m := runtimecfgtest.RepresentativeFullProfile()
	if err := finalizeRuntimeConfigPipeline(m, t.TempDir(), 54333, 9097, "secret", "tun", true, true); err != nil {
		t.Fatalf("pipeline error: %v", err)
	}

	d := runtimecfgtest.DNSMap(t, m)

	// DNS listen: default :1053 (all interfaces, real fixed port — verge parity).
	// Loopback breaks TUN dns-hijack; an ephemeral :0 listen did not work for users.
	if d["listen"] != ":1053" {
		t.Errorf("dns.listen = %v, want :1053", d["listen"])
	}
	// Subscription DNS fields preserved (NOT clobbered by our overlay).
	if _, ok := d["proxy-server-nameserver"]; !ok {
		t.Error("proxy-server-nameserver dropped (must be preserved from subscription)")
	}
	if _, ok := d["nameserver-policy"]; !ok {
		t.Error("nameserver-policy dropped (must be preserved)")
	}
	if ff, ok := d["fake-ip-filter"].([]any); !ok || len(ff) == 0 {
		t.Error("fake-ip-filter dropped (must be preserved)")
	}
	if d["enhanced-mode"] != "fake-ip" {
		t.Errorf("enhanced-mode = %v, want fake-ip (preserved)", d["enhanced-mode"])
	}
	if d["respect-rules"] != true {
		t.Errorf("respect-rules = %v, want true (preserved)", d["respect-rules"])
	}

	// TUN: we force the hardened tun base OVER the subscription. The input ships
	// stack: mixed + strict-route: true; both must be overridden. strict-route
	// MUST be false — a subscription strict-route: true loops the core's own
	// DNS/proxy traffic back into the TUN under the system service and kills
	// proxy-node resolution (only DIRECT survives). Locked here so it can never
	// regress.
	tun, ok := m["tun"].(map[string]any)
	if !ok {
		t.Fatalf("tun block missing: %#v", m["tun"])
	}
	if tun["strict-route"] != false {
		t.Errorf("tun.strict-route = %v, want false (forced over subscription)", tun["strict-route"])
	}
	if tun["stack"] != "gvisor" {
		t.Errorf("tun.stack = %v, want gvisor (forced over subscription)", tun["stack"])
	}
	if tun["enable"] != true {
		t.Errorf("tun.enable = %v, want true (overwritten to traffic intent)", tun["enable"])
	}

	// geo cadence from subscription preserved (we only default when ABSENT).
	if m["geo-auto-update"] != true {
		t.Errorf("geo-auto-update = %v, want true (subscription-provided, preserved)", m["geo-auto-update"])
	}

	// Sloth runtime overlay (intentional, documented divergences from verge):
	if m["mixed-port"] != 54333 {
		t.Errorf("mixed-port = %v, want 54333 (we use a random free port by design)", m["mixed-port"])
	}
	if m["socks-port"] != 0 || m["port"] != 0 {
		t.Errorf("socks-port/port = %v/%v, want 0/0 (disabled)", m["socks-port"], m["port"])
	}
	if m["allow-lan"] != false {
		t.Errorf("allow-lan = %v, want false", m["allow-lan"])
	}
	prof, ok := m["profile"].(map[string]any)
	if !ok || prof["store-selected"] != true {
		t.Errorf("profile.store-selected missing/false: %#v", m["profile"])
	}

	// Routing payload intact.
	if px, ok := m["proxies"].([]any); !ok || len(px) != 1 {
		t.Errorf("proxies not preserved: %#v", m["proxies"])
	}
}

// On Windows the wintun adapter is given the deterministic name "SlothClash" so
// a fresh adapter never collides with a stale "Meta" corpse (the access-denied
// trigger) and the interface is recognisably ours. It is a DEFAULT: a device the
// subscription already ships must survive. Off Windows we must NOT set it —
// mihomo requires a utunN-style name on macOS and a custom one breaks bring-up.
func TestTunDeviceNameWindowsDefault(t *testing.T) {
	// No device in the subscription tun block -> our default applies (Windows).
	m := runtimecfgtest.RepresentativeFullProfile()
	if err := finalizeRuntimeConfigPipeline(m, t.TempDir(), 54333, 9097, "secret", "tun", true, true); err != nil {
		t.Fatalf("pipeline error: %v", err)
	}
	tun, ok := m["tun"].(map[string]any)
	if !ok {
		t.Fatalf("tun block missing: %#v", m["tun"])
	}
	if runtime.GOOS == "windows" {
		if tun["device"] != "SlothClash" {
			t.Errorf("tun.device = %v, want SlothClash (Windows default)", tun["device"])
		}
	} else {
		if _, present := tun["device"]; present {
			t.Errorf("tun.device = %v, want unset off Windows (mihomo needs a utunN name)", tun["device"])
		}
	}

	// A subscription-provided device must be preserved, not overwritten, on every
	// platform (user/subscription intent wins over our default).
	m2 := runtimecfgtest.RepresentativeFullProfile()
	m2["tun"].(map[string]any)["device"] = "CustomTun0"
	if err := finalizeRuntimeConfigPipeline(m2, t.TempDir(), 54333, 9097, "secret", "tun", true, true); err != nil {
		t.Fatalf("pipeline error: %v", err)
	}
	if got := m2["tun"].(map[string]any)["device"]; got != "CustomTun0" {
		t.Errorf("tun.device = %v, want CustomTun0 (subscription value preserved)", got)
	}
}
