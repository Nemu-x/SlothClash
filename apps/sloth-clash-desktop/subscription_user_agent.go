package main

import "strings"

// subscriptionUserAgent builds the User-Agent sent on subscription requests.
//
// Shape: `clash.meta/v<core> citrus/<citrus> SlothClash/<app>`, e.g.
// `clash.meta/v1.19.33 citrus/v1.0.0-beta.2 SlothClash/0.10.1`.
//
// Every token is load-bearing for the panels users point us at:
//   - The first token must stay `clash.meta/…`: Marzban / Remnawave pick the
//     clash-meta output format by matching the UA prefix against
//     `^(clash-verge|clash[-.]?meta|flclash|mihomo)`; anything else gets legacy
//     clash YAML or a base64 list.
//   - The version after the slash must be the real core version: mikan only
//     emits newer protocols (mieru, sudoku, trusttunnel, shadowquic, Hysteria2
//     Gecko, …) when `(mihomo|clash[.-]?meta)/v?X.Y.Z` names a core that
//     supports them. A fake or missing version silently downgrades the config.
//   - `citrus/<tag>` is the second product token, written the way a Citrus core
//     writes its own (`clash.meta/<v> citrus/<v>`): panels tell a Citrus client
//     from a stock mihomo by `\bcitrus/(\S+)` and only then serve the Citrus
//     surface (XDrive, chain groups, mldsa65-verify, …). The tag is the latest
//     Citrus release whose client surface our compat suite covers
//     (core/mihomo/manifest.json citrusCompat -> core-build.json), not our core
//     version. It is bumped together with a Citrus release sync.
//   - `SlothClash/<app>` names the app (mikan's catalog matches `slothclash`).
//
// When the core version is unknown (build without core-version.txt) we send
// `clash.meta/mihomo …` — still format-selecting, never a made-up number. When
// the Citrus release is unknown (stock core, SLOTH_CORE_FILE binary, or no
// citrusCompat in the manifest) the citrus token is left out entirely. The app
// version is the real AppVersion, not a hard-coded "1.0".
func subscriptionUserAgent(coreVersion, citrusRelease, appVersion string) string {
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
	ua := "clash.meta/" + core
	if citrus := validCitrusRelease(citrusRelease); citrus != "" {
		ua += " citrus/" + citrus
	}
	return ua + " SlothClash/" + app
}

// subscriptionUserAgentCurrent is the UA for this build: embedded core pin +
// the Citrus release its series covers + AppVersion. Single source for every
// subscription request path.
func subscriptionUserAgentCurrent() string {
	return subscriptionUserAgent(embeddedCoreVersion(), embeddedCoreCitrusCompat(), AppVersion)
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
