package main

import (
	"fmt"
	"strings"

	"gopkg.in/yaml.v3"
)

const tunDefaultDNSYAML = `dns:
  enable: true
  listen: ":1053"
  ipv6: true
  respect-rules: true
  enhanced-mode: fake-ip
  fake-ip-range: 198.18.0.1/16
  use-hosts: true
  default-nameserver:
    - 1.1.1.1
    - 8.8.8.8
  nameserver:
    - https://1.1.1.1/dns-query
    - tls://8.8.8.8:853
`

func mergeTunFromYAMLString(m map[string]any, fragment string) {
	var wrap map[string]any
	if err := yaml.Unmarshal([]byte(fragment), &wrap); err != nil {
		return
	}
	if t, ok := wrap["tun"].(map[string]any); ok {
		m["tun"] = t
	}
}

func ensureDefaultDNSForTun(m map[string]any) {
	raw, hasDNS := m["dns"]
	var dns map[string]any
	if hasDNS {
		if dm, ok := raw.(map[string]any); ok {
			dns = dm
		}
	}
	if dns == nil {
		var wrap map[string]any
		if err := yaml.Unmarshal([]byte(tunDefaultDNSYAML), &wrap); err != nil {
			return
		}
		if d, ok := wrap["dns"].(map[string]any); ok {
			m["dns"] = d
		}
		return
	}

	// Clash Verge-like behavior: keep user DNS settings, only fill missing TUN-critical keys.
	if _, ok := dns["enable"]; !ok {
		dns["enable"] = true
	}
	// DNS listener default: `:1053` (all interfaces, a REAL fixed port) — matches
	// clash-verge-rev, which works reliably in the field. Hard requirements learned
	// from real users:
	//   - NOT loopback-only (`127.0.0.1`): unreachable for TUN `dns-hijack: any:53`
	//     on Windows → DNS dies under TUN while system-proxy keeps working.
	//   - A REAL fixed port, NOT `:0`/ephemeral: a `0.0.0.0:0` listen did NOT work
	//     for a user (no resolution), while verge's `:1053` worked instantly.
	// We only fill this DEFAULT when the subscription/extended config did not set
	// its own `dns.listen`. An explicit value (e.g. the subscription's own `:1053`)
	// is honoured, never clobbered — same as verge.
	if v, ok := dns["listen"].(string); !ok || strings.TrimSpace(v) == "" {
		dns["listen"] = ":1053"
	}
	if _, ok := dns["enhanced-mode"]; !ok {
		dns["enhanced-mode"] = "fake-ip"
	}
	if mode, _ := dns["enhanced-mode"].(string); strings.TrimSpace(strings.ToLower(mode)) == "fake-ip" {
		if _, ok := dns["fake-ip-range"]; !ok {
			dns["fake-ip-range"] = "198.18.0.1/16"
		}
	}
	if _, ok := dns["ipv6"]; !ok {
		if topIPv6, has := m["ipv6"].(bool); has {
			dns["ipv6"] = topIPv6
		} else {
			dns["ipv6"] = true
		}
	}
	// Mihomo requires proxy-server-nameserver when respect-rules is enabled.
	// Keep Verge-like non-destructive behavior: only fill it when missing/empty.
	respectRules := false
	switch v := dns["respect-rules"].(type) {
	case bool:
		respectRules = v
	case string:
		s := strings.ToLower(strings.TrimSpace(v))
		respectRules = s == "true" || s == "1" || s == "yes" || s == "on"
	}
	if respectRules {
		repair := true
		switch vv := dns["proxy-server-nameserver"].(type) {
		case []any:
			repair = len(vv) == 0
		case []string:
			repair = len(vv) == 0
		case string:
			repair = strings.TrimSpace(vv) == ""
		}
		if repair {
			if dnsDefault, ok := dns["default-nameserver"].([]any); ok && len(dnsDefault) > 0 {
				dns["proxy-server-nameserver"] = append([]any(nil), dnsDefault...)
			} else if dnsDefaultS, ok := dns["default-nameserver"].([]string); ok && len(dnsDefaultS) > 0 {
				out := make([]any, 0, len(dnsDefaultS))
				for _, s := range dnsDefaultS {
					out = append(out, s)
				}
				dns["proxy-server-nameserver"] = out
			} else {
				dns["proxy-server-nameserver"] = []any{"1.1.1.1", "8.8.8.8"}
			}
		}
	}
	m["dns"] = dns
}

// ensureTunOverlayForTraffic installs and hardens the TUN block in the generated
// runtime config. The enableTun argument is written verbatim to tun.enable, so
// callers are expected to pass the effective user intent (connected && traffic=="tun").
//
// We OVERWRITE a hardened base set (stack=gvisor, auto-route, auto-detect-interface,
// strict-route=false, dns-hijack=[any:53]) on top of whatever the subscription
// ships — we do NOT trust the subscription's tun verbatim. A subscription can ship
// tun options that break routing/egress; most importantly strict-route=true, which
// forces the core's own DNS/proxy traffic back into the TUN under the system-service
// core and kills proxy-node resolution (only DIRECT survives — proven in the field).
// Keys the subscription adds beyond the base (route-exclude-address, mtu, device)
// are preserved, and the user can still override any field via Settings → TUN
// (applyUserTunOverlay runs after this). tun.enable is set every time so
// PUT /configs?force=true stays idempotent across hot reloads.
//
// History: this used to trust the subscription's tun verbatim and overwrite only
// tun.enable, which let strict-route=true through and caused the dead-proxy bug;
// and before that it forced stack=system + tcp://any:53, which hurt UDP-heavy
// traffic on wintun. Both are gone.
func ensureTunOverlayForTraffic(m map[string]any, enableTun bool) {
	rawTun, has := m["tun"].(map[string]any)
	if !has || rawTun == nil {
		mergeTunFromYAMLString(m, tunBlockForTraffic(enableTun))
		rawTun, _ = m["tun"].(map[string]any)
		if rawTun == nil {
			rawTun = map[string]any{}
		}
	}

	// Overwrite the whole hardened tun base over whatever the subscription ships,
	// so no subscription can ship a tun option that breaks routing/egress. Most
	// critically strict-route MUST stay false: a subscription shipping
	// strict-route: true forces the core's OWN outbound (upstream DNS + proxy-node
	// dials) back into the TUN when the core runs under our SYSTEM/session-0
	// service, so proxy nodes never resolve ("couldn't find ip") and only DIRECT
	// works — proven on a real user machine. stack=gvisor is the reliable
	// userspace stack and our default; system/mixed can stall on wintun under UDP
	// load. Extra keys the subscription adds (route-exclude-address, mtu, device)
	// are preserved. Users can still override any of these via Settings → TUN
	// (applyUserTunOverlay runs after this and wins).
	rawTun["stack"] = "gvisor"
	rawTun["auto-route"] = true
	rawTun["auto-detect-interface"] = true
	rawTun["strict-route"] = false
	rawTun["dns-hijack"] = []string{"any:53"}
	rawTun["enable"] = enableTun
	m["tun"] = rawTun
}

// ensureRealtimeRoutingDefaults used to force `sniffer.enable=true` and
// `find-process-mode: strict` on every generated config. clash-verge-rev does
// neither (enhance::use_tun only touches DNS; IClashTemp::template() never
// sets sniffer or find-process-mode), so forcing them was a measurable
// regression vs Verge Rev for UDP-heavy traffic like games: sniffing every
// QUIC packet adds latency and can drop packets under load, and strict
// per-connection PID lookup on Windows adds overhead per UDP session.
//
// Post-alignment this function is intentionally a no-op — if the subscription
// or extended config ships a sniffer / find-process-mode block it is left
// verbatim, otherwise Mihomo falls back to its own defaults (sniffer off,
// find-process-mode off) which matches Verge Rev behaviour.
func ensureRealtimeRoutingDefaults(m map[string]any) {
	_ = m
}

func overlaySlothRuntimeOnMap(m map[string]any, mixedPort, ctrlPort int, secret, traffic string, withExternalController bool, enableTun bool) {
	m["mixed-port"] = mixedPort
	m["socks-port"] = 0
	m["port"] = 0

	if withExternalController && ctrlPort > 0 {
		m["external-controller"] = fmt.Sprintf("127.0.0.1:%d", ctrlPort)
	} else {
		delete(m, "external-controller")
	}
	m["secret"] = secret
	m["allow-lan"] = false

	// profile.store-selected / store-fake-ip mirrors clash-verge-rev's
	// `use_clash` defaults. Without store-selected, mihomo forgets the
	// user's pick inside each `select` group on every hot reload (and we
	// reload on every Connect/Disconnect/SetTrafficMode) — our sticky-group
	// code only restores the ACTIVE group, not per-group node picks, so
	// without this flag a user who picked a specific node in two different
	// groups would see them reset half the time. store-fake-ip keeps the
	// fake-IP map across reloads while TUN is enabled, so apps that have
	// cached fake-IPs do not have to renegotiate after a reconnect.
	//
	// We only set fields the user has not explicitly defined: subscription
	// profiles that ship their own `profile:` block win (matches verge-rev
	// merge order: user/subscription overrides our defaults).
	if _, has := m["profile"]; !has {
		m["profile"] = map[string]any{}
	}
	if prof, ok := m["profile"].(map[string]any); ok {
		if _, has := prof["store-selected"]; !has {
			prof["store-selected"] = true
		}
		if _, has := prof["store-fake-ip"]; !has {
			prof["store-fake-ip"] = enableTun
		}
	}

	// Match clash-verge-rev enhance::tun::use_tun: only harden DNS (fake-ip
	// invariants) when TUN is actually being brought up. With TUN off we leave
	// DNS alone so Mihomo falls back to system DNS for proxied traffic.
	if enableTun {
		ensureDefaultDNSForTun(m)
	}

	ensureTunOverlayForTraffic(m, enableTun)
	_ = traffic
	ensureRealtimeRoutingDefaults(m)

	// User-controlled overlays last: Settings → TUN / Traffic preferences
	// (Verge-Rev-style `tun-viewer.tsx` + sniffer / find-process-mode fields)
	// must win over subscription defaults, matching Verge Rev's merge order
	// where the verge config patches are applied after the profile's own YAML.
	prefs := currentDesktopPrefs()
	applyUserTunOverlay(m, prefs.TUN)
	applyUserTrafficOverlay(m, prefs.Traffic)
}
