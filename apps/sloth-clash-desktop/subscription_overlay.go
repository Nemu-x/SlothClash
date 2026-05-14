package main

import (
	"fmt"
	"strings"

	"gopkg.in/yaml.v3"
)

const tunDefaultDNSYAML = `dns:
  enable: true
  listen: 127.0.0.1:0
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
	// Force a random free port for the internal DNS listener regardless of what the
	// subscription / merge template prescribed. A hard-coded port (e.g. `:1053`) is
	// routinely taken by other Clash-family clients / ad blockers / corporate DNS
	// proxies on user machines, which leaves mihomo without a DNS server and breaks
	// fake-ip resolution silently ("VPN works but websites fail"). The port is internal
	// to mihomo (dns-hijack redirects TUN :53 to it), so any free port works.
	dns["listen"] = "127.0.0.1:0"
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

// ensureTunOverlayForTraffic installs or hardens the TUN block in the generated
// runtime config. The enableTun argument is written verbatim to tun.enable, so
// callers are expected to pass the effective user intent (connected && traffic=="tun").
// This mirrors clash-verge-rev's enhance::tun::use_tun + IClashTemp::template():
//
//   - If the subscription or extended config already ships a `tun:` block we
//     trust its stack / auto-route / dns-hijack / mtu values verbatim (same as
//     Verge Rev's `append!` semantics in the template).
//   - If no `tun:` block is present we install the default template (gvisor,
//     auto-route, strict-route=false, dns-hijack=[any:53]).
//   - Only `tun.enable` is overwritten every time, so PUT /configs?force=true
//     stays idempotent across hot reloads.
//
// Previously this function forced stack=system and added tcp://any:53 on
// Windows. That hurt UDP-heavy traffic (games) because Mihomo's kernel stack
// is more sensitive to wintun ring-buffer pressure than gvisor, and the extra
// TCP hijack caused double-handling of DNS. Both have been removed to track
// upstream verbatim — users who need a different stack can override through
// the subscription / extended config / Settings → TUN UI.
func ensureTunOverlayForTraffic(m map[string]any, enableTun bool) {
	rawTun, has := m["tun"].(map[string]any)
	if !has || rawTun == nil {
		mergeTunFromYAMLString(m, tunBlockForTraffic(enableTun))
		rawTun, _ = m["tun"].(map[string]any)
		if rawTun == nil {
			rawTun = map[string]any{}
		}
	}

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
