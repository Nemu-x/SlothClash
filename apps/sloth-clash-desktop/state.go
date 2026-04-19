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

// HomeInsight is a best-effort snapshot for the Home screen (latency, exit IP, optional direct IP in rule mode).
type HomeInsight struct {
	NodeLatencyMs int    `json:"nodeLatencyMs"` // 0 = not available
	LatencyError  string `json:"latencyError,omitempty"`
	ExitIP        string `json:"exitIp,omitempty"`
	ExitLine      string `json:"exitLine,omitempty"` // plain geo text, e.g. "Russia · Moscow" (no emoji; see ExitFlagIso2)
	ExitFlagIso2  string `json:"exitFlagIso2,omitempty"` // ISO 3166-1 alpha-2 for flag image (WebView may render 🇷🇺 as "RU")
	DirectIP      string `json:"directIp,omitempty"` // WAN; meaningful in rule vs tun exit
	DirectError   string `json:"directError,omitempty"`
	LastError     string `json:"lastError,omitempty"`
	UploadKbps    int    `json:"uploadKbps"`   // mihomo GET /traffic (kbps); always sent so UI can show 0
	DownloadKbps  int    `json:"downloadKbps"` // mihomo GET /traffic (kbps)
	TrafficError  string `json:"trafficError,omitempty"`
	UpdatedAt     int64  `json:"updatedAt,omitempty"`
}

type ConnectionState struct {
	Status      string `json:"status"`
	LastError   string `json:"lastError,omitempty"`
	LastWarning string `json:"lastWarning,omitempty"` // non-fatal; e.g. TUN takeover skipped
	Since       int64  `json:"since,omitempty"`
}

type ModeState struct {
	Current           string `json:"current"`
	LastNonDirectMode string `json:"lastNonDirectMode,omitempty"`
}

type Profile struct {
	ID             string `json:"id"`
	Name           string `json:"name"`
	Type           string `json:"type"`
	URL            string `json:"url,omitempty"`
	SubscriptionInfo string `json:"subscriptionInfo,omitempty"` // decoded Subscription-Userinfo header when provider exposes it
	LastUpdated    int64  `json:"lastUpdated,omitempty"`
	AutoUpdateEnabled bool `json:"autoUpdateEnabled,omitempty"` // periodically refresh subscription metadata/content
	AutoUpdateIntervalMinutes int `json:"autoUpdateIntervalMinutes,omitempty"` // 0 => use default backend interval
	MergeTemplate  string `json:"mergeTemplate,omitempty"`       // Extend config YAML
	RulesTemplate  string `json:"rulesTemplate,omitempty"`       // Rules editor YAML (prepend/append/delete)
	ProxyTemplate  string `json:"proxyTemplate,omitempty"`       // Proxy groups editor YAML (prepend/append/delete)
	SkipAutoConfig bool   `json:"skipAutoConfig,omitempty"` // after manual config.yaml edit, skip regeneration on connect
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
	Groups        []ProxyGroup `json:"groups"`
	ActiveGroup   string       `json:"activeGroup,omitempty"`
	LastGoodGroup string       `json:"lastGoodGroup,omitempty"`
}

type ServiceState struct {
	Installed bool   `json:"installed"`
	Running   bool   `json:"running"`
	LastError string `json:"lastError,omitempty"`
}

type CoreState struct {
	Running        bool   `json:"running"`
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
	Channel       string `json:"channel"`
	HasUpdate     bool   `json:"hasUpdate"`
	LastCheckedAt int64  `json:"lastCheckedAt,omitempty"`
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
