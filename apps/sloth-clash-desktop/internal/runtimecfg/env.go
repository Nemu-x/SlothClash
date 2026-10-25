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
	// OnBodyFetch receives the outcome of every subscription body fetch the
	// generation pipeline makes on its own (the cache-miss fetch of a first
	// Connect and the background refresh kicked after a cache hit): nil on
	// success (the cache now holds the fetched body), the failure otherwise.
	// main records it on the profile so a refresh that keeps failing is
	// visible in the UI. May be nil.
	OnBodyFetch func(dataDir string, err error)
}

// headers hands a successful fetch's headers to OnHeaders, if set.
func (c SubscriptionClient) headers(dataDir, subURL string, hdr http.Header) {
	if c.OnHeaders != nil {
		c.OnHeaders(dataDir, subURL, hdr)
	}
}

// bodyFetched hands a body fetch outcome to OnBodyFetch, if set.
func (c SubscriptionClient) bodyFetched(dataDir string, err error) {
	if c.OnBodyFetch != nil {
		c.OnBodyFetch(dataDir, err)
	}
}
