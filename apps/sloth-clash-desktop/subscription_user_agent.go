package main

import "strings"

// subscriptionUserAgent builds the User-Agent sent on subscription requests.
//
// Shape: `clash.meta/v<core> SlothClash/<app>`, e.g.
// `clash.meta/v1.19.32 SlothClash/0.9.4`.
//
// Both tokens are load-bearing for the panels users point us at:
//   - The first token must stay `clash.meta/…`: Marzban / Remnawave pick the
//     clash-meta output format by matching the UA prefix against
//     `^(clash-verge|clash[-.]?meta|flclash|mihomo)`; anything else gets legacy
//     clash YAML or a base64 list.
//   - The version after the slash must be the real core version: mikan only
//     emits newer protocols (mieru, sudoku, trusttunnel, shadowquic, Hysteria2
//     Gecko, …) when `(mihomo|clash[.-]?meta)/v?X.Y.Z` names a core that
//     supports them. A fake or missing version silently downgrades the config.
//
// When the core version is unknown (build without core-version.txt) we send
// `clash.meta/mihomo SlothClash/<app>` — still format-selecting, never a made-up
// number. The app version is the real AppVersion, not a hard-coded "1.0".
func subscriptionUserAgent(coreVersion, appVersion string) string {
	core := strings.TrimSpace(coreVersion)
	if core != "" && !strings.HasPrefix(core, "v") {
		core = "v" + core
	}
	if core == "" {
		core = "mihomo"
	}
	app := strings.TrimSpace(appVersion)
	if app == "" {
		app = "0.0.0"
	}
	return "clash.meta/" + core + " SlothClash/" + app
}

// subscriptionUserAgentCurrent is the UA for this build: embedded core pin +
// AppVersion. Single source for every subscription request path.
func subscriptionUserAgentCurrent() string {
	return subscriptionUserAgent(embeddedCoreVersion(), AppVersion)
}

// subscriptionProbeUserAgents is the ordered list the subscription peek tries
// when a provider rejects the primary UA. The real UA goes first; the legacy
// fallbacks exist only for panels that allowlist specific client names.
func subscriptionProbeUserAgents() []string {
	return []string{
		subscriptionUserAgentCurrent(),
		"ClashMeta/2.10.1.Meta-Alpha",
		"ClashForWindows/0.20.39",
		"SlothClash/" + strings.TrimSpace(AppVersion) + " (compatible; mihomo-like-client)",
	}
}
