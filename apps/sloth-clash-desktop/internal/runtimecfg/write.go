package runtimecfg

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"SlothClashDesktop/internal/fsutil"

	"gopkg.in/yaml.v3"
)

// tunBlockForTraffic returns the TUN configuration embedded in generated YAML.
// The enable argument is inlined verbatim into tun.enable so the YAML always
// reflects the caller's current intent. Defaults mirror clash-verge-rev's
// IClashTemp::template() + constants::tun (DEFAULT_STACK=gvisor, DNS_HIJACK=["any:53"],
// strict-route=false, auto-route=true, auto-detect-interface=true). gvisor is the
// userspace stack Mihomo defaults to and behaves consistently across Windows/macOS/Linux;
// it avoids the kernel ring-buffer stalls that `stack: system` on wintun shows under
// sustained UDP load (e.g. gaming). These are defaults only — if the subscription or
// user TUN settings provide a value the merger will honour it verbatim.
func tunBlockForTraffic(enable bool) string {
	enableStr := "false"
	if enable {
		enableStr = "true"
	}
	return `tun:
  enable: ` + enableStr + `
  stack: gvisor
  auto-route: true
  auto-detect-interface: true
  strict-route: false
  dns-hijack:
    - any:53
`
}

// Write generates the runtime config.yaml for in: the merged full
// profile when the subscription is one, the bare provider profile otherwise.
// The profile's JavaScript override (in.Script, may be empty) is threaded
// through exactly like the declarative templates beside it. scriptOut, when
// non-nil, receives what the script did.
func Write(in Input, scriptOut *ScriptResult) error {
	_ = os.MkdirAll(filepath.Join(in.DataDir, "providers"), 0o755)
	_ = os.MkdirAll(filepath.Join(in.DataDir, "ruleset"), 0o755)

	outcome, err := tryWriteMergedFullProfile(in, scriptOut)
	if outcome == pipelineOK {
		return nil
	}
	if err != nil {
		return err
	}
	// outcome is one of the "soft" cases (cache_miss_no_net / not_full_profile);
	// fall through to write the bare provider profile below.
	_ = outcome.useBareFallback() // anchor for readers — see pipelineOutcome.

	geoDir := filepath.Join(in.DataDir, "geo")
	geoIP := filepath.Join(geoDir, "geoip.dat")
	geoSite := filepath.Join(geoDir, "geosite.dat")

	var cfg strings.Builder
	fmt.Fprintf(&cfg, "mixed-port: %d\n", in.MixedPort)
	fmt.Fprintf(&cfg, "socks-port: 0\n")
	fmt.Fprintf(&cfg, "port: 0\n")
	if in.WithExternalController && in.CtrlPort > 0 {
		fmt.Fprintf(&cfg, "external-controller: 127.0.0.1:%d\n", in.CtrlPort)
	}
	fmt.Fprintf(&cfg, "secret: %q\n", in.Secret)
	// Same user pref as the subscription path (default off = localhost only);
	// the wildcard bind keeps LAN clients able to reach us when it is on.
	if in.Env.Prefs.AllowLan {
		fmt.Fprintf(&cfg, "allow-lan: true\n")
		fmt.Fprintf(&cfg, "bind-address: \"*\"\n")
	} else {
		fmt.Fprintf(&cfg, "allow-lan: false\n")
	}
	fmt.Fprintf(&cfg, "mode: rule\n")
	fmt.Fprintf(&cfg, "log-level: info\n")
	// Top-level ipv6 must agree with dns.ipv6 below (the subscription path
	// force-aligns them for the same reason): a machine with no working IPv6
	// route that gets AAAA records dials v6 and fails.
	fmt.Fprintf(&cfg, "ipv6: %t\n\n", in.Env.Prefs.DNSIPv6)

	// profile.store-selected mirrors clash-verge-rev's `use_clash` defaults:
	// preserves per-group node selections across hot reloads (we reload on
	// every Connect/Disconnect/SetTrafficMode). store-fake-ip keeps the
	// fake-IP map across reloads while TUN is on, so apps with cached
	// fake-IPs do not have to renegotiate after reconnect.
	fmt.Fprintf(&cfg, "profile:\n")
	fmt.Fprintf(&cfg, "  store-selected: true\n")
	fmt.Fprintf(&cfg, "  store-fake-ip: %t\n\n", in.EnableTun)

	if in.EnableTun {
		// Match enhance::tun::use_tun: fake-ip DNS block goes in only when TUN
		// is actually being brought up. Without it, TUN + strict-route gives
		// "connected" apps but no working resolution / routing.
		// Both flags follow the Settings → Connection toggles (same source of
		// truth as the subscription path), instead of being pinned on here.
		fmt.Fprintf(&cfg, `dns:
  enable: true
  listen: ":1053"
  ipv6: %t
  respect-rules: %t
  enhanced-mode: fake-ip`, in.Env.Prefs.DNSIPv6, in.Env.Prefs.SmartDNS)
		cfg.WriteString(`
  fake-ip-range: 198.18.0.1/16
  # Required whenever fake-ip runs with ipv6:true — without a v6 pool AAAA
  # queries never get a fake address and IPv6 resolution fails outright
  # (clash-verge-rev #7373). Same default range upstream uses.
  fake-ip-range6: fdfe:dcba:9876::1/64
  use-hosts: true
  default-nameserver:
    - 1.1.1.1
    - 8.8.8.8
  nameserver:
    - https://1.1.1.1/dns-query
    - tls://8.8.8.8:853

`)
	}

	if _, err := os.Stat(geoIP); err == nil {
		fmt.Fprintf(&cfg, "geo-auto-update: false\n")
		// geodata-mode: true selects .dat geoip database; the previous value
		// "standard" was not a valid mode (it's a value for the unrelated
		// `geodata-loader` key) — mihomo silently fell back to mmdb and our
		// bundled .dat files were never loaded.
		fmt.Fprintf(&cfg, "geodata-mode: true\n")
		fmt.Fprintf(&cfg, "geoip: %q\n", filepath.ToSlash(geoIP))
		if _, err2 := os.Stat(geoSite); err2 == nil {
			fmt.Fprintf(&cfg, "geosite: %q\n", filepath.ToSlash(geoSite))
		}
		fmt.Fprintf(&cfg, "\n")
	}

	fmt.Fprintf(&cfg, "proxy-providers:\n")
	fmt.Fprintf(&cfg, "  sub1:\n")
	fmt.Fprintf(&cfg, "    type: http\n")
	fmt.Fprintf(&cfg, "    url: %q\n", in.SubURL)
	fmt.Fprintf(&cfg, "    interval: 3600\n")
	fmt.Fprintf(&cfg, "    path: ./providers/sub1.yaml\n")
	fmt.Fprintf(&cfg, "    health-check:\n")
	fmt.Fprintf(&cfg, "      enable: true\n")
	fmt.Fprintf(&cfg, "      url: http://cp.cloudflare.com/generate_204\n")
	fmt.Fprintf(&cfg, "      interval: 600\n\n")

	// Bare fallback groups: emit a `select` group (Manual) that lists Auto
	// first followed by every proxy provider entry. MATCH routes through
	// Manual so the default selection stays "pick the fastest" (Manual →
	// Auto → url-test), but users can click an individual node in the
	// Proxies screen without hitting Mihomo's 400 "cannot change url-test
	// group selection" error. url-test groups refuse PUT /proxies/{group}
	// selection, which is why pointing MATCH directly at Auto made manual
	// node picking look broken in Rule mode.
	fmt.Fprintf(&cfg, "proxy-groups:\n")
	fmt.Fprintf(&cfg, "  - name: Auto\n")
	fmt.Fprintf(&cfg, "    type: url-test\n")
	fmt.Fprintf(&cfg, "    use:\n")
	fmt.Fprintf(&cfg, "      - sub1\n")
	fmt.Fprintf(&cfg, "    url: http://cp.cloudflare.com/generate_204\n")
	fmt.Fprintf(&cfg, "    interval: 300\n")
	fmt.Fprintf(&cfg, "    tolerance: 50\n")
	fmt.Fprintf(&cfg, "  - name: Manual\n")
	fmt.Fprintf(&cfg, "    type: select\n")
	fmt.Fprintf(&cfg, "    proxies:\n")
	fmt.Fprintf(&cfg, "      - Auto\n")
	fmt.Fprintf(&cfg, "    use:\n")
	fmt.Fprintf(&cfg, "      - sub1\n\n")

	fmt.Fprintf(&cfg, "rules:\n")
	fmt.Fprintf(&cfg, "  - MATCH,Manual\n\n")
	cfg.WriteString(tunBlockForTraffic(in.EnableTun))

	var m map[string]any
	if err := yaml.Unmarshal([]byte(cfg.String()), &m); err != nil {
		return err
	}
	if err := ApplyMergeTemplate(m, in.ExtendTemplate); err != nil {
		return err
	}
	if err := ApplyMergeTemplate(m, in.ProxyTemplate); err != nil {
		return err
	}
	if err := ApplyMergeTemplate(m, in.RulesTemplate); err != nil {
		return err
	}
	// The bare-provider fallback runs the script too: the user's declarative
	// templates are applied on this path (just above), so silently skipping only
	// the script would be the surprising half-measure.
	if err := finalizeRuntimeConfigPipelineWithScript(
		m,
		in.DataDir,
		in.MixedPort,
		in.CtrlPort,
		in.Secret,
		in.Traffic,
		in.WithExternalController,
		in.EnableTun,
		in.Script,
		scriptOut,
		in.Env,
	); err != nil {
		return err
	}

	out, err := MarshalYAML(m)
	if err != nil {
		return err
	}
	cfgPath := filepath.Join(in.DataDir, "config.yaml")
	return fsutil.AtomicWriteFile(cfgPath, out, 0o644)
}

// repairConfigDNS is the last-mile DNS hardening run on config.yaml right
// before the core's preflight: it re-applies the DNS self-heal to whatever is on
// disk (generated or hand-edited) and rewrites the file.
func repairConfigDNS(cfgPath string, p Prefs) error {
	b, err := os.ReadFile(cfgPath)
	if err != nil {
		return err
	}
	var m map[string]any
	if err := yaml.Unmarshal(b, &m); err != nil {
		return err
	}
	if len(m) == 0 {
		return nil
	}
	// A `dns:` key that is not a mapping means the file is broken (usually a
	// hand-edited merge template). Healing it here would REPLACE it with our
	// default block and hand the core a config that parses — masking the real
	// error instead of showing it. Leave it alone so the `-t` preflight below
	// reports what the core actually rejects.
	if raw, has := m["dns"]; has {
		if _, isMap := raw.(map[string]any); !isMap && raw != nil {
			return nil
		}
	}
	ensureDefaultDNSForTun(m, p)
	// The self-heal above fills dns.fake-ip-range6 unconditionally, so the guard
	// has to run here too — this is the LAST writer of config.yaml before the
	// core reads it, and without this it would silently undo the pipeline's
	// decision (see dropUnroutableFakeIPRange6).
	dropUnroutableFakeIPRange6(m)
	out, err := MarshalYAML(m)
	if err != nil {
		return err
	}
	return fsutil.AtomicWriteFile(cfgPath, out, 0o644)
}
