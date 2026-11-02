package runtimecfg

import "strings"

// XDrive storage services the shipped core is allowed to run. Exact strings,
// as the core's xdrive Config.Validate switches on them (no trimming, no case
// folding). `local` and `template` exist in the core but are refused by
// decision; anything else (`s3` from newer Xray, a typo, a missing service)
// is unknown to the core.
var allowedXDriveServices = map[string]bool{
	"google-drive": true,
	"Google Drive": true, // the core's alias of google-drive
}

// applyXDriveServiceAllowList turns every XDrive node whose storage service is
// not allowed into `type: reject` with the same name, in `proxies` and inline
// proxy-provider payloads.
//
// Why here and not in the core: mihomo fails the whole config when one
// top-level proxy does not parse (config.parseProxies), and the whole provider
// when one of its proxies does not. A subscription with one `service: s3` node
// would otherwise not start at all. As REJECT the node keeps its name, so the
// groups and rules that use it stay valid, and it shows as disabled.
//
// Proxy-providers the core fetches itself (http / file) are not visible here;
// for those the core's own error still applies to that provider.
//
// Runs next to applyXDriveHardening, in the overlay and again after the
// profile's JavaScript override.
func applyXDriveServiceAllowList(m map[string]any) {
	nodes := 0
	if proxies, ok := m["proxies"].([]any); ok {
		nodes += rejectUnsupportedXDriveProxies(proxies)
	}
	if providers, ok := m["proxy-providers"].(map[string]any); ok {
		for _, raw := range providers {
			pm, ok := raw.(map[string]any)
			if !ok {
				continue
			}
			for k, v := range pm {
				if !mihomoKeyMatches(k, "payload") {
					continue
				}
				if payload, ok := v.([]any); ok {
					nodes += rejectUnsupportedXDriveProxies(payload)
				}
			}
		}
	}
	if nodes > 0 {
		debugLog("config", "XDRIVE-2", "xdrive_unknown_service.go:applyXDriveServiceAllowList",
			"rejected XDrive nodes with an unsupported storage service",
			map[string]any{"nodesRejected": nodes})
	}
}

// rejectUnsupportedXDriveProxies replaces, in place, every XDrive proxy whose
// service is not allowed with a REJECT proxy of the same name. Returns how
// many it replaced.
func rejectUnsupportedXDriveProxies(proxies []any) int {
	n := 0
	for i, it := range proxies {
		pm, ok := it.(map[string]any)
		if !ok || !isXDriveProxy(pm) || xdriveServiceAllowed(pm) {
			continue
		}
		proxies[i] = map[string]any{"name": mihomoStringOf(pm, "name"), "type": "reject"}
		n++
	}
	return n
}

// isXDriveProxy over-matches like isLocalXDriveProxy: any `network` spelling
// the decoder could read, case-insensitive.
func isXDriveProxy(pm map[string]any) bool {
	for _, v := range mihomoValuesOf(pm, "network") {
		if s, ok := v.(string); ok && strings.EqualFold(strings.TrimSpace(s), "xdrive") {
			return true
		}
	}
	return false
}

// xdriveServiceAllowed reports whether the node names an allowed service in
// every spelling the decoder could pick: at least one `xdrive-opts` mapping,
// at least one `service`, and every one of them allowed. Missing or malformed
// options count as not allowed (the core refuses those too).
func xdriveServiceAllowed(pm map[string]any) bool {
	optsList := mihomoValuesOf(pm, "xdrive-opts")
	if len(optsList) == 0 {
		return false
	}
	for _, v := range optsList {
		opts, ok := v.(map[string]any)
		if !ok {
			return false
		}
		services := mihomoValuesOf(opts, "service")
		if len(services) == 0 {
			return false
		}
		for _, s := range services {
			str, ok := s.(string)
			if !ok || !allowedXDriveServices[str] {
				return false
			}
		}
	}
	return true
}
