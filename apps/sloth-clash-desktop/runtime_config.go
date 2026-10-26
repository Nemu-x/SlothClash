package main

import "SlothClashDesktop/internal/runtimecfg"

// main's side of the internal/runtimecfg seam: everything a config generation
// reads from the running app is captured here, once per generation, and handed
// over in runtimecfg.Env.

func init() {
	runtimecfg.SetLogSinks(debugLog, traceEvent)
}

// currentRuntimeConfigEnv captures the app state one generation reads: the
// prefs snapshot, the live corp-VPN split and the subscription client, all from
// the same moment.
func currentRuntimeConfigEnv() runtimecfg.Env {
	prefs := currentDesktopPrefs()
	return runtimecfg.Env{
		Prefs:            runtimeConfigPrefsFrom(prefs),
		CorpVpn:          currentCorpVpnSplit(),
		Client:           subscriptionClientFor(prefs),
		AppVersion:       AppVersion,
		DesyncNetworkKey: desyncNetworkKey,
	}
}

// currentRuntimeConfigPrefs captures just the prefs part of the env.
func currentRuntimeConfigPrefs() runtimecfg.Prefs {
	return runtimeConfigPrefsFrom(currentDesktopPrefs())
}

// runtimeConfigPrefsFrom extracts the generation inputs from a prefs snapshot,
// every toggle resolved to its effective value.
func runtimeConfigPrefsFrom(p DesktopPrefs) runtimecfg.Prefs {
	d := effectiveDesyncSettings(p.Desync)
	return runtimecfg.Prefs{
		AllowLan:    p.Connection.IsAllowLanEnabled(),
		DNSIPv6:     p.Connection.IsDNSIPv6Enabled(),
		SmartDNS:    p.Connection.IsSmartDNSEnabled(),
		TUN:         runtimecfg.TunSettings(p.TUN),
		Traffic:     runtimecfg.TrafficSettings(p.Traffic),
		Reality:     runtimecfg.RealitySettings(p.Reality),
		Desync:      runtimecfg.DesyncSettings{Enabled: d.Enabled, YouTube: d.YouTube, Discord: d.Discord, DiscordUDP: d.DiscordUDP, Custom: desyncCustomRoutes(d.Custom), FakeTTL: desyncFakeTTLSupported},
		DesyncCache: desyncCacheEntries(p.DesyncCache),
	}
}

// desyncCustomRoutes is the enabled entries of an (already sanitized) list.
func desyncCustomRoutes(entries []DesyncCustomEntry) []runtimecfg.DesyncCustomRoute {
	var out []runtimecfg.DesyncCustomRoute
	for _, e := range entries {
		if e.Enabled {
			out = append(out, runtimecfg.DesyncCustomRoute{ID: e.ID, Domain: e.Domain})
		}
	}
	return out
}

// currentSubscriptionClient is how a subscription request made right now
// identifies itself.
func currentSubscriptionClient() runtimecfg.SubscriptionClient {
	return subscriptionClientFor(currentDesktopPrefs())
}

func subscriptionClientFor(p DesktopPrefs) runtimecfg.SubscriptionClient {
	id := subscriptionDeviceIdentityCurrent()
	return runtimecfg.SubscriptionClient{
		UserAgent:   subscriptionUserAgentCurrent(),
		AppVersion:  AppVersion,
		HWID:        id.HWID,
		OSVersion:   id.OSVersion,
		DeviceModel: id.DeviceModel,
		SendHWID:    p.Privacy.IsHwidEnabled(),
		OnHeaders:   persistBrandManifestFromHeaders,
		OnBodyFetch: recordPipelineBodyFetch,
	}
}

// scriptResult is runtimecfg.ScriptResult as the Wails bindings expose it (the
// frontend model is main.scriptResult); same fields, see there for what they
// mean.
type scriptResult struct {
	Ran              bool     `json:"ran"`
	Applied          bool     `json:"applied"`
	Err              string   `json:"error,omitempty"`
	Line             int      `json:"line,omitempty"`
	Column           int      `json:"column,omitempty"`
	Console          []string `json:"console,omitempty"`
	ConsoleTruncated bool     `json:"consoleTruncated,omitempty"`
	DurationMS       int64    `json:"durationMs,omitempty"`
}
