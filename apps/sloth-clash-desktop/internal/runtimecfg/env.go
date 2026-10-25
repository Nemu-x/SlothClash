package runtimecfg

import "net/http"

// Env is everything a generation reads from the running app rather than from
// the profile: the prefs snapshot, the live corp-VPN split, how subscription
// requests identify themselves, and a few process facts. main captures it once
// per generation (currentRuntimeConfigEnv) and the generation never reaches
// back into the app for any of it.
type Env struct {
	Prefs Prefs
	// CorpVpn is the split the running corp-VPN sidecar reported; zero (and
	// inactive) when there is none.
	CorpVpn CorpVpnSplit
	Client  SubscriptionClient
	// AppVersion is the desktop app version handed to profile scripts.
	AppVersion string
	// DesyncNetworkKey identifies the current network for the desync winner
	// cache. Only called when desync is on; nil means no cache lookups.
	DesyncNetworkKey func() string
}

// SubscriptionClient is how this app fetches a subscription: the identity its
// requests present and what it does with a successful response's headers.
type SubscriptionClient struct {
	UserAgent  string
	AppVersion string
	// Device identity sent as x-hwid / x-ver-os / x-device-model. HWID is only
	// sent when SendHWID is set (PrivacySettings.IsHwidEnabled in main).
	HWID        string
	OSVersion   string
	DeviceModel string
	SendHWID    bool
	// OnHeaders receives the headers of every successful subscription fetch
	// (main caches the provider's brand manifest from them). May be nil.
	OnHeaders func(dataDir, subURL string, hdr http.Header)
}

// headers hands a successful fetch's headers to OnHeaders, if set.
func (c SubscriptionClient) headers(dataDir, subURL string, hdr http.Header) {
	if c.OnHeaders != nil {
		c.OnHeaders(dataDir, subURL, hdr)
	}
}
