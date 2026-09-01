package main

import (
	"fmt"
	"runtime"
	"strings"

	"gopkg.in/yaml.v3"
)

// tunWindowsDeviceName is the deterministic wintun adapter name used on Windows.
// Kept distinct from mihomo's default "Meta" so a fresh adapter never collides
// with a stale one and so the interface is recognisably ours. See its use in
// ensureTunOverlayForTraffic and the service-side removal that matches wintun by
// driver (so it also clears the historical "Meta" name).
const tunWindowsDeviceName = "SlothClash"

const tunDefaultDNSYAML = `dns:
  enable: true
  listen: ":1053"
  # ipv6 here is always overwritten by the master toggle in ensureDefaultDNSForTun
  # (default ON — verge parity). Kept true so the raw template is coherent on its own.
  ipv6: true
  respect-rules: false
  enhanced-mode: fake-ip
  fake-ip-range: 198.18.0.1/16
  fake-ip-range6: fdfe:dcba:9876::1/64
  fake-ip-filter-mode: blacklist
  use-hosts: true
  default-nameserver:
    - system
    - 1.1.1.1
    - 8.8.8.8
  nameserver:
    - 8.8.8.8
    - 1.1.1.1
    - https://1.1.1.1/dns-query
`

// defaultFakeIPFilter mirrors clash-verge-rev's default fake-ip blacklist.
// In fake-ip mode every resolved name gets a synthetic 198.18.x address; these
// names MUST resolve for real or the OS breaks in ways users blame on the VPN:
//   - captive-portal / connectivity probes (msftncsi, msftconnecttest) → Windows
//     shows "No internet" and may loop a captive-portal sign-in page;
//   - NTP (time.*, ntp.*) → clock never syncs;
//   - *.lan / *.local / *.arpa → local network + mDNS/rDNS lookups break.
// Most subscriptions ship their own list; we only fill it when absent.
var defaultFakeIPFilter = []any{
	"*.lan",
	"*.local",
	"*.arpa",
	"time.*.com",
	"ntp.*.com",
	"+.market.xiaomi.com",
	"localhost.ptlogin2.qq.com",
	"*.msftncsi.com",
	"www.msftconnecttest.com",
}

// fakeIPFilterIsEmpty reports whether the parsed dns.fake-ip-filter carries no
// entries (missing, wrong type, or an empty sequence).
func fakeIPFilterIsEmpty(v any) bool {
	switch f := v.(type) {
	case nil:
		return true
	case []any:
		return len(f) == 0
	case []string:
		return len(f) == 0
	default:
		return true
	}
}

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
	// The master IPv6 switch (default ON — verge parity) drives BOTH top-level
	// `ipv6` and `dns.ipv6`; align them in EVERY branch so the no-dns-block path
	// below can't ship the incoherent state that once caused the assets.msn.com
	// dial failure (architecture/ipv6.md).
	topIPv6 := currentDesktopPrefs().Connection.IsDNSIPv6Enabled()
	m["ipv6"] = topIPv6

	if dns == nil {
		var wrap map[string]any
		if err := yaml.Unmarshal([]byte(tunDefaultDNSYAML), &wrap); err != nil {
			return
		}
		if d, ok := wrap["dns"].(map[string]any); ok {
			d["fake-ip-filter"] = append([]any(nil), defaultFakeIPFilter...)
			d["ipv6"] = topIPv6
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
		if v, ok := dns["fake-ip-range"].(string); !ok || strings.TrimSpace(v) == "" {
			dns["fake-ip-range"] = "198.18.0.1/16"
		}
		// A v6 pool is mandatory once fake-ip runs with ipv6:true — otherwise
		// AAAA queries never get a fake address and IPv6 resolution fails
		// (clash-verge-rev #7373). Filled unconditionally so a profile that
		// flips ipv6 on later is still correct. Empty string counts as missing
		// so a hand-edited YAML gets repaired.
		// The pool is only KEPT if the final tun block actually routes it —
		// dropUnroutableFakeIPRange6 has the last word at the end of the
		// pipeline (a black-holed pool stalls every dual-stack site).
		if v, ok := dns["fake-ip-range6"].(string); !ok || strings.TrimSpace(v) == "" {
			dns["fake-ip-range6"] = "fdfe:dcba:9876::1/64"
		}
		// Without a filter, captive-portal probes / NTP / *.lan get fake IPs and
		// the OS looks broken. Verge parity: fill the default list when absent.
		if raw, ok := dns["fake-ip-filter"]; !ok || fakeIPFilterIsEmpty(raw) {
			dns["fake-ip-filter"] = append([]any(nil), defaultFakeIPFilter...)
		}
		if v, ok := dns["fake-ip-filter-mode"].(string); !ok || strings.TrimSpace(v) == "" {
			dns["fake-ip-filter-mode"] = "blacklist"
		}
	}
	// verge parity (enhance/tun.rs): dns.ipv6 mirrors the top-level `ipv6` flag,
	// which we default ON (verge's template ships `ipv6: true`). With it OFF,
	// mihomo does not process IPv6, the TUN gets no inet6-address / v6 route, and
	// native IPv6 bypasses the tunnel — blocked sites leak over real v6. With it
	// ON, fake-ip returns a fake v6 (fake-ip-range6) routed into the tunnel, so
	// the node needs no real v6 and nothing leaks (architecture/ipv6.md).
	//
	// The Settings toggle overrides both in either direction: a broken-IPv6
	// machine can switch it OFF even when the subscription enables it, and vice
	// versa. topIPv6 + m["ipv6"] were already set at the top of this function.
	dns["ipv6"] = topIPv6
	// "Smart DNS fallback" in Settings maps to dns.respect-rules: proxied domains
	// resolve through the proxy (no leak / ISP poisoning for them), direct ones
	// stay local. Opt-in only — when the toggle is off we leave whatever the
	// subscription shipped, because the config-parity guard requires
	// subscription DNS blocks to survive untouched.
	if currentDesktopPrefs().Connection.IsSmartDNSEnabled() {
		dns["respect-rules"] = true
	}

	// proxy-server-nameserver is required whenever respect-rules is on, and is
	// needed under TUN regardless: the proxy server's own hostname must resolve
	// OUTSIDE the tunnel, otherwise it depends on the tunnel it is meant to
	// establish. This function only runs when TUN is being brought up, so fill
	// it unconditionally — it used to be a side effect of respect-rules being
	// hardcoded true, which broke once that became a user toggle.
	// Keep Verge-like non-destructive behavior: only fill it when missing/empty.
	{
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

	// Windows only: give the wintun adapter a deterministic, branded name instead
	// of mihomo's default "Meta". Two payoffs. (1) A fresh install creates
	// "SlothClash", which never collides with a stale "Meta" adapter left by an
	// older build or a co-installed clash-verge — the exact name collision that
	// makes WintunCreateAdapter fail with "access is denied". (2) Combined with
	// the service-side removal, recovery is unambiguous. A DEFAULT, not a force:
	// a subscription or Settings → TUN device stays honoured (applyUserTunOverlay
	// runs after this). macOS/Linux keep the empty/auto name — mihomo requires a
	// utunN-style name there and a custom one would break bring-up.
	if runtime.GOOS == "windows" {
		if dev, ok := rawTun["device"].(string); !ok || strings.TrimSpace(dev) == "" {
			rawTun["device"] = tunWindowsDeviceName
		}
	}
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

	// LAN exposure is a user decision (default off = localhost only). When it
	// is on, a loopback `bind-address` inherited from the profile would silently
	// defeat it — the core would still listen on localhost only — so rewrite it
	// to the wildcard, matching clash-verge-rev's fix.
	allowLan := currentDesktopPrefs().Connection.IsAllowLanEnabled()
	m["allow-lan"] = allowLan
	if allowLan {
		if v, ok := m["bind-address"].(string); !ok || isLoopbackBindAddress(v) {
			m["bind-address"] = "*"
		}
	}

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

	// Corp-VPN coexistence LAST: corp route-exclude / split-DNS are mandatory for
	// no-conflict and must survive any user/subscription overlay. A strict no-op
	// when no corp sidecar is active, so config parity is unaffected when off.
	if enableTun {
		applyCorpVpnOverlay(m, currentCorpVpnSplit())
	}

}
