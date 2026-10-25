package main

// runtimeConfigPrefs is the slice of the desktop prefs the config generation
// reads. The caller captures it ONCE, when a generation starts
// (currentRuntimeConfigPrefs()), and threads it down: one generated config
// never mixes two prefs states, and the generation layer has no hidden read of
// the global prefs.
type runtimeConfigPrefs struct {
	// Connection toggles (Settings → Connection), already resolved to their
	// effective values (see the ConnectionSettings Is*Enabled accessors).
	AllowLan bool
	DNSIPv6  bool
	SmartDNS bool
	// SendHWID controls the x-hwid header on subscription requests
	// (PrivacySettings.IsHwidEnabled).
	SendHWID bool

	TUN     TunSettings
	Traffic TrafficSettings
	Reality RealitySettings
	// Desync is the effective desync config, defaults applied
	// (effectiveDesyncSettings); DesyncCache is the per-network winner cache the
	// overlay orders candidates by.
	Desync      DesyncSettings
	DesyncCache []DesyncNetworkCache
}

// currentRuntimeConfigPrefs captures the generation inputs from the live prefs.
func currentRuntimeConfigPrefs() runtimeConfigPrefs {
	return runtimeConfigPrefsFrom(currentDesktopPrefs())
}

// runtimeConfigPrefsFrom extracts the generation inputs from a prefs snapshot.
func runtimeConfigPrefsFrom(p DesktopPrefs) runtimeConfigPrefs {
	return runtimeConfigPrefs{
		AllowLan:    p.Connection.IsAllowLanEnabled(),
		DNSIPv6:     p.Connection.IsDNSIPv6Enabled(),
		SmartDNS:    p.Connection.IsSmartDNSEnabled(),
		SendHWID:    p.Privacy.IsHwidEnabled(),
		TUN:         p.TUN,
		Traffic:     p.Traffic,
		Reality:     p.Reality,
		Desync:      effectiveDesyncSettings(p.Desync),
		DesyncCache: p.DesyncCache,
	}
}
