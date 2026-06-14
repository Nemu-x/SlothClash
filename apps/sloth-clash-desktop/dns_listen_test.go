package main

import "testing"

// The DNS listener must default to all-interfaces port 1053 (verge parity), and
// NEVER loopback-only — a 127.0.0.1 bind is unreachable for TUN dns-hijack on
// many Windows setups (DNS silently dies under TUN). These lock all three code
// paths that set a default listen.

func TestEnsureDefaultDNSForTunListenDefault(t *testing.T) {
	m := map[string]any{"dns": map[string]any{"enable": true}}
	ensureDefaultDNSForTun(m)
	dns := m["dns"].(map[string]any)
	// Default :1053 (all interfaces, real fixed port — verge parity). Loopback
	// breaks TUN dns-hijack; an ephemeral :0 listen did not work for users.
	if dns["listen"] != ":1053" {
		t.Fatalf("listen default = %v, want :1053", dns["listen"])
	}
}

func TestEnsureDefaultDNSForTunRespectsExplicitListen(t *testing.T) {
	m := map[string]any{"dns": map[string]any{"enable": true, "listen": "127.0.0.1:5353"}}
	ensureDefaultDNSForTun(m)
	dns := m["dns"].(map[string]any)
	if dns["listen"] != "127.0.0.1:5353" {
		t.Fatalf("explicit listen overwritten: %v (must be honoured)", dns["listen"])
	}
}

// When the config ships no dns block at all, the default template must also
// bind all-interfaces (the const path used to be loopback-only).
func TestEnsureDefaultDNSForTunNoBlockBindsAllInterfaces(t *testing.T) {
	m := map[string]any{}
	ensureDefaultDNSForTun(m)
	dns, _ := m["dns"].(map[string]any)
	if dns == nil {
		t.Fatal("dns block not created")
	}
	listen, _ := dns["listen"].(string)
	if listen == "" || listen[:3] == "127" {
		t.Fatalf("no-dns-block listen = %q, must be all-interfaces (not loopback)", listen)
	}
}
