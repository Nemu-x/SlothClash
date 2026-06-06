package main

type AppState struct {
	Connection ConnectionState `json:"connection"`
	Mode       ModeState       `json:"mode"`
	Traffic    string          `json:"traffic"`
	Profile    ProfileState    `json:"profile"`
	Proxy      ProxyState      `json:"proxy"`
	Service    ServiceState    `json:"service"`
	Core       CoreState       `json:"core"`
	Insight    HomeInsight     `json:"insight"`
	UI         UIState         `json:"ui"`
	UpdatedAt  int64           `json:"updatedAt"`
}

// HomeInsight is a best-effort snapshot for the Home screen (latency, exit/direct geo flags, IPs for tooltips / diagnostics).
type HomeInsight struct {
	NodeLatencyMs  int    `json:"nodeLatencyMs"` // 0 = not available
	LatencyError   string `json:"latencyError,omitempty"`
	ExitIP         string `json:"exitIp,omitempty"`
	ExitLine       string `json:"exitLine,omitempty"`       // plain geo text, e.g. "Russia · Moscow" (no emoji; see ExitFlagIso2)
	ExitFlagIso2   string `json:"exitFlagIso2,omitempty"`   // ISO 3166-1 alpha-2 for flag image (WebView may render 🇷🇺 as "RU")
	DirectIP       string `json:"directIp,omitempty"`       // WAN; meaningful in rule vs tun exit (tooltip / diagnostics)
	DirectFlagIso2 string `json:"directFlagIso2,omitempty"` // geo for direct WAN when available
	DirectError    string `json:"directError,omitempty"`
	LastError      string `json:"lastError,omitempty"`
	UploadKbps     int    `json:"uploadKbps"`   // mihomo GET /traffic (kbps); always sent so UI can show 0
	DownloadKbps   int    `json:"downloadKbps"` // mihomo GET /traffic (kbps)
	TrafficError   string `json:"trafficError,omitempty"`
	UpdatedAt      int64  `json:"updatedAt,omitempty"`
}

type ConnectionState struct {
	Status      string `json:"status"`
	Health      string `json:"health,omitempty"` // ready|degraded|broken while connected (empty = warming / not classified)
	LastError   string `json:"lastError,omitempty"`
	LastWarning string `json:"lastWarning,omitempty"` // non-fatal; e.g. TUN takeover skipped
	Since       int64  `json:"since,omitempty"`
}

type ModeState struct {
	Current           string `json:"current"`
	LastNonDirectMode string `json:"lastNonDirectMode,omitempty"`
}

type Profile struct {
	ID               string `json:"id"`
	Name             string `json:"name"`
	Type             string `json:"type"`
	URL              string `json:"url,omitempty"`
	SubscriptionInfo string `json:"subscriptionInfo,omitempty"` // decoded Subscription-Userinfo header when provider exposes it
	// Optional provider metadata from subscription HTTP response headers (non-standard; see subscription.go).
	SubscriptionSupportURL    string `json:"subscriptionSupportUrl,omitempty"`
	SubscriptionAnnouncement  string `json:"subscriptionAnnouncement,omitempty"`
	LastUpdated               int64  `json:"lastUpdated,omitempty"`
	AutoUpdateEnabled         bool   `json:"autoUpdateEnabled,omitempty"`         // periodically refresh subscription metadata/content
	AutoUpdateIntervalMinutes int    `json:"autoUpdateIntervalMinutes,omitempty"` // 0 => use default backend interval
	MergeTemplate             string `json:"mergeTemplate,omitempty"`             // Extend config YAML
	RulesTemplate             string `json:"rulesTemplate,omitempty"`             // Rules editor YAML (prepend/append/delete)
	ProxyTemplate             string `json:"proxyTemplate,omitempty"`             // Proxy groups editor YAML (prepend/append/delete)
	SkipAutoConfig            bool   `json:"skipAutoConfig,omitempty"`            // after manual config.yaml edit, skip regeneration on connect
	// LastGoodGroup remembers the user's last manually picked proxy group
	// for this specific profile. It is the authoritative source for the
	// auto-select routine: if the same group still exists in /proxies when
	// the user reconnects (today, tomorrow, after an app restart), we snap
	// back to it. Only SelectProxyGroup writes to this field — auto-picked
	// fallbacks (anchor / first-safe) never touch it, so the stored value
	// always reflects genuine user intent.
	LastGoodGroup string `json:"lastGoodGroup,omitempty"`
}

// ProfilePaths exposes on-disk locations for a profile runtime directory.
type ProfilePaths struct {
	DataDir    string `json:"dataDir"`
	ConfigPath string `json:"configPath"`
}

// ProfileConfigPeek is the contents of runtime/<id>/config.yaml (when present).
type ProfileConfigPeek struct {
	Path      string `json:"path"`
	Body      string `json:"body"`
	LastError string `json:"lastError,omitempty"`
}

// ProfileRulesBaseline is the list of `rules:` produced by the subscription
// (plus any extend/proxy merge templates) before the rules editor applies
// its own prepend/append/delete overlay. The UI renders this list read-only
// so the user can see where a given rule came from and mark subscription
// rules for deletion without editing them.
type ProfileRulesBaseline struct {
	Rules         []string `json:"rules"`
	IsFullProfile bool     `json:"isFullProfile"`
	LastError     string   `json:"lastError,omitempty"`
}

// ProfileProxyGroupsBaseline is the list of `proxy-groups:` produced by the
// subscription (plus any extend merge template) before the proxy-groups
// editor applies its own prepend/append/delete overlay. The UI renders this
// list read-only so the user can see which groups come from the subscription
// / extended config and mark them for deletion without editing them.
type ProfileProxyGroupsBaseline struct {
	Groups        []map[string]any `json:"groups"`
	IsFullProfile bool             `json:"isFullProfile"`
	LastError     string           `json:"lastError,omitempty"`
}

type ProfileState struct {
	ActiveProfileID string    `json:"activeProfileId,omitempty"`
	Profiles        []Profile `json:"profiles"`
}

type ProxyGroup struct {
	Name     string   `json:"name"`
	Type     string   `json:"type"`
	Proxies  []string `json:"proxies"`
	Selected string   `json:"selected,omitempty"`
}

type ProxyState struct {
	Groups []ProxyGroup `json:"groups"`
	// ActiveGroup is the proxy group the UI currently highlights. It is
	// set explicitly by `SelectProxyGroup` (user click) and on mode
	// switches ("GLOBAL" in global mode). On Connect we also run
	// `restoreStickyGroupLocked` which copies the active profile's
	// LastGoodGroup into ActiveGroup — that's the only automatic write.
	// There is no anchor derivation and no first-safe fallback.
	ActiveGroup string `json:"activeGroup,omitempty"`
	// LastGoodGroup mirrors the active profile's persisted sticky pick
	// (see `Profile.LastGoodGroup`) for the frontend. Written by
	// `SelectProxyGroup`, hydrated on profile load / switch / delete.
	LastGoodGroup string `json:"lastGoodGroup,omitempty"`
}

type ServiceState struct {
	Installed bool   `json:"installed"`
	Running   bool   `json:"running"`
	LastError string `json:"lastError,omitempty"`
}

type CoreState struct {
	Running        bool   `json:"running"`
	Lifecycle      string `json:"lifecycle,omitempty"` // starting|running|stopping|stopped|degraded
	Version        string `json:"version,omitempty"`
	ControllerAddr string `json:"controllerAddr,omitempty"`
	MixedPort      int    `json:"mixedPort,omitempty"`
	LastError      string `json:"lastError,omitempty"`
}

type UIState struct {
	IsLoading    bool   `json:"isLoading"`
	ActiveModal  string `json:"activeModal,omitempty"`
	ActiveScreen string `json:"activeScreen"`
}

type UpdateState struct {
	Channel          string `json:"channel"`
	HasUpdate        bool   `json:"hasUpdate"`
	LastCheckedAt    int64  `json:"lastCheckedAt,omitempty"`
	CurrentVersion   string `json:"currentVersion,omitempty"`
	LatestVersion    string `json:"latestVersion,omitempty"`
	ReleaseURL       string `json:"releaseUrl,omitempty"`
	ReleaseNotes     string `json:"releaseNotes,omitempty"`
	AssetName        string `json:"assetName,omitempty"`
	AssetDownloadURL string `json:"assetDownloadUrl,omitempty"`
	LastError        string `json:"lastError,omitempty"`
}

type TunSetupResult struct {
	Success       bool   `json:"success"`
	Message       string `json:"message"`
	InstallAction bool   `json:"installAction"`
}

// RulesOverview is a best-effort snapshot from a running mihomo external-controller.
// Point SLOTH_CLASH_CONTROLLER at the listen address (e.g. 127.0.0.1:9090) and
// SLOTH_CLASH_SECRET at the API secret if configured.
type RulesOverview struct {
	Controller        string `json:"controller"`
	Reachable         bool   `json:"reachable"`
	LastError         string `json:"lastError,omitempty"`
	RulesBody         string `json:"rulesBody,omitempty"`
	RuleProvidersBody string `json:"ruleProvidersBody,omitempty"`
}

// ServiceLogPeek is a tail of logs/service_latest.log for the active profile runtime dir.
type ServiceLogPeek struct {
	Path      string `json:"path"`
	Text      string `json:"text"`
	Truncated bool   `json:"truncated"`
	LastError string `json:"lastError,omitempty"`
}

// RuntimeDiagEvent is a small, privacy-safe timeline for diagnostics export.
// Categories use dotted names (for example "core.reload", "connection.degraded").
type RuntimeDiagEvent struct {
	TsUnixMs int64  `json:"ts"`
	Category string `json:"category"`
	Message  string `json:"message,omitempty"`
}
