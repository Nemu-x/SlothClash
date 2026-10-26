package runtimecfg

import "SlothClashDesktop/internal/desync"

// Prefs is the slice of the desktop prefs the config generation reads. The
// caller captures it ONCE, when a generation starts, and threads it down (as
// part of Env): one generated config never mixes two prefs states, and the
// generation layer has no hidden read of the app's global prefs.
//
// The zero value is NOT the app's default prefs: DNSIPv6 defaults to on in the
// app. main builds Prefs from its DesktopPrefs (runtimeConfigPrefsFrom), which
// resolves every toggle to its effective value.
type Prefs struct {
	// Connection toggles (Settings → Connection), already resolved to their
	// effective values.
	AllowLan bool
	DNSIPv6  bool
	SmartDNS bool

	TUN     TunSettings
	Traffic TrafficSettings
	Reality RealitySettings
	// Desync is the effective desync config (defaults applied); DesyncCache is
	// the per-network winner cache the overlay orders candidates by.
	Desync      DesyncSettings
	DesyncCache []desync.CacheEntry
}

// TunSettings is the user's Settings → TUN overlay. It has exactly the fields
// of main's (Wails-exposed) TunSettings, so main converts it directly; a zero /
// nil field means "inherit the subscription / template default".
type TunSettings struct {
	Stack               string   // "" = inherit; gvisor|system|mixed
	AutoRoute           *bool    // nil = inherit
	AutoDetectInterface *bool    // nil = inherit
	StrictRoute         *bool    // nil = inherit
	DNSHijack           []string // nil / empty = inherit
	MTU                 int      // 0 = inherit
	Device              string   // "" = inherit
}

// TrafficSettings is the user's sniffer / find-process-mode overlay, field for
// field main's TrafficSettings.
type TrafficSettings struct {
	SnifferEnabled  *bool  // nil = inherit
	FindProcessMode string // "" = inherit; off|strict|always
}

// RealitySettings is the user's REALITY client tuning, field for field main's
// RealitySettings.
type RealitySettings struct {
	MLKEM         string // "" = auto; auto|always|never
	ClientVersion string // "" = subscription value, else core default
}

// DesyncSettings is the part of the desync config the overlay reads: whether
// it is on and which routes it generates.
type DesyncSettings struct {
	Enabled    bool
	YouTube    bool
	Discord    bool
	DiscordUDP bool
	// Custom is the enabled custom-domain entries, already validated (the
	// domain goes into rules verbatim), in the user's order.
	Custom []DesyncCustomRoute
	// FakeTTL reports whether fake-TTL strategies work on this platform; it
	// selects the custom candidate set.
	FakeTTL bool
}

// DesyncCustomRoute is one enabled custom-domain entry: its stable ID (which
// names the group) and its normalized domain.
type DesyncCustomRoute struct {
	ID     string
	Domain string
}
