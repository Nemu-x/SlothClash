package runtimecfg

// applyUserTunOverlay writes the user's TunSettings onto the generated tun:
// block. Empty / nil fields are skipped (no override), mirroring Verge Rev's
// `revise!` approach. Called after ensureTunOverlayForTraffic has set
// tun.enable and the base template.
func applyUserTunOverlay(m map[string]any, tun TunSettings) {
	rawTun, _ := m["tun"].(map[string]any)
	if rawTun == nil {
		rawTun = map[string]any{}
	}
	if tun.Stack != "" {
		rawTun["stack"] = tun.Stack
	}
	if tun.AutoRoute != nil {
		rawTun["auto-route"] = *tun.AutoRoute
	}
	if tun.AutoDetectInterface != nil {
		rawTun["auto-detect-interface"] = *tun.AutoDetectInterface
	}
	if tun.StrictRoute != nil {
		rawTun["strict-route"] = *tun.StrictRoute
	}
	if len(tun.DNSHijack) > 0 {
		arr := make([]any, 0, len(tun.DNSHijack))
		for _, h := range tun.DNSHijack {
			arr = append(arr, h)
		}
		rawTun["dns-hijack"] = arr
	}
	if tun.MTU > 0 {
		rawTun["mtu"] = tun.MTU
	}
	if tun.Device != "" {
		rawTun["device"] = tun.Device
	}
	m["tun"] = rawTun
}

// applyUserTrafficOverlay writes the user's sniffer / find-process-mode
// preferences on top of whatever the subscription ships. A nil pointer or ""
// leaves the subscription value (or Mihomo's internal default) untouched.
func applyUserTrafficOverlay(m map[string]any, traffic TrafficSettings) {
	if traffic.SnifferEnabled != nil {
		rawSniffer, _ := m["sniffer"].(map[string]any)
		if rawSniffer == nil {
			rawSniffer = map[string]any{}
		}
		rawSniffer["enable"] = *traffic.SnifferEnabled
		m["sniffer"] = rawSniffer
	}
	if traffic.FindProcessMode != "" {
		m["find-process-mode"] = traffic.FindProcessMode
	}
}
