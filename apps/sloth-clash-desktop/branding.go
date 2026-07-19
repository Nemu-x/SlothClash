package main

import (
	"encoding/base64"
	"encoding/json"
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"
)

// ---------------------------------------------------------------------------
// Panel-driven branding via subscription HTTP response headers.
//
// Desktop reads ONLY the `X-Brand-Desktop-*` namespace (the Android sibling
// client owns `X-Brand-*`; the two are intentionally disjoint so a provider
// tuning one platform can never restyle the other). The whole manifest is
// gated on `X-Brand-Desktop-Enabled: true` and on the subscription being
// fetched over HTTPS. Grammar (value formats, validation, caps) is shared
// with the Android implementation — see architecture/brand-headers-protocol.md.
//
// Trust boundary: a manifest may hide non-critical UI, recolor, and add
// operator links. It has no reach into connection security, updates, or the
// Settings/Logs pages.
// ---------------------------------------------------------------------------

const (
	brandHdrEnabled = "X-Brand-Desktop-Enabled"

	brandHdrName         = "X-Brand-Desktop-Name"
	brandHdrTagline      = "X-Brand-Desktop-Tagline"
	brandHdrLogoURL      = "X-Brand-Desktop-Logo-URL"
	brandHdrLogoLightURL = "X-Brand-Desktop-Logo-Light-URL"
	brandHdrAccentColor  = "X-Brand-Desktop-Accent-Color"

	brandHdrWebsiteURL  = "X-Brand-Desktop-Website-URL"
	brandHdrSupportURL  = "X-Brand-Desktop-Support-URL"
	brandHdrTelegramURL = "X-Brand-Desktop-Telegram-URL"
	brandHdrBotURL      = "X-Brand-Desktop-Bot-URL"
	brandHdrPrivacyURL  = "X-Brand-Desktop-Privacy-URL"
	brandHdrTermsURL    = "X-Brand-Desktop-Terms-URL"
	brandHdrHelpURL     = "X-Brand-Desktop-Help-URL"
	brandHdrStatusURL   = "X-Brand-Desktop-Status-URL"
	brandHdrRenewURL    = "X-Brand-Desktop-Renew-URL"
	brandHdrCabinetURL  = "X-Brand-Desktop-Cabinet-URL"

	brandHdrUserDisplayName = "X-Brand-Desktop-User-Display-Name"
	brandHdrGreeting        = "X-Brand-Desktop-Greeting"

	// Device-slot usage. No standard subscription header carries this (the
	// common Subscription-Userinfo only has traffic + expiry), so it lives in
	// our namespace: panels that track device limits can surface "3 / 5" in the
	// operator dialog instead of the user guessing.
	brandHdrDevicesUsed  = "X-Brand-Desktop-Devices-Used"
	brandHdrDevicesLimit = "X-Brand-Desktop-Devices-Limit"

	brandHdrHideGlobalMode   = "X-Brand-Desktop-Hide-Global-Mode"
	brandHdrHideProxyMode    = "X-Brand-Desktop-Hide-Proxy-Mode"
	brandHdrHideLocalConfigs = "X-Brand-Desktop-Hide-Local-Configs"
	brandHdrHideAdvanced     = "X-Brand-Desktop-Hide-Advanced"
)

// Value caps — same numbers as the shared protocol doc.
const (
	brandMaxNameLen        = 64
	brandMaxTaglineLen     = 128
	brandMaxDisplayNameLen = 96
	brandMaxGreetingLen    = 160
	brandMaxURLLen         = 2048
	brandMaxDeviceCount    = 10000
)

// BrandManifest is the validated, ready-to-render branding for one profile.
// Field values are already sanitized; the frontend applies them as-is.
type BrandManifest struct {
	Enabled bool `json:"enabled"`

	Name         string `json:"name,omitempty"`
	Tagline      string `json:"tagline,omitempty"`
	LogoURL      string `json:"logoUrl,omitempty"`
	LogoLightURL string `json:"logoLightUrl,omitempty"`
	AccentColor  string `json:"accentColor,omitempty"`

	WebsiteURL  string `json:"websiteUrl,omitempty"`
	SupportURL  string `json:"supportUrl,omitempty"`
	TelegramURL string `json:"telegramUrl,omitempty"`
	BotURL      string `json:"botUrl,omitempty"`
	PrivacyURL  string `json:"privacyUrl,omitempty"`
	TermsURL    string `json:"termsUrl,omitempty"`
	HelpURL     string `json:"helpUrl,omitempty"`
	StatusURL   string `json:"statusUrl,omitempty"`
	RenewURL    string `json:"renewUrl,omitempty"`
	CabinetURL  string `json:"cabinetUrl,omitempty"`

	UserDisplayName string `json:"userDisplayName,omitempty"`
	Greeting        string `json:"greeting,omitempty"`

	// 0 = not reported. Used alone still renders ("2 devices"); with a limit it
	// renders as a ratio.
	DevicesUsed  int `json:"devicesUsed,omitempty"`
	DevicesLimit int `json:"devicesLimit,omitempty"`

	HideGlobalMode   bool `json:"hideGlobalMode,omitempty"`
	HideProxyMode    bool `json:"hideProxyMode,omitempty"`
	HideLocalConfigs bool `json:"hideLocalConfigs,omitempty"`
	HideAdvanced     bool `json:"hideAdvanced,omitempty"`
}

var brandHexColorRe = regexp.MustCompile(`^#(?:[0-9a-fA-F]{3}|[0-9a-fA-F]{6})$`)

// brandRawValue trims whitespace and one layer of surrounding quotes, and
// repairs invalid UTF-8 (defensive; Go keeps header bytes verbatim, so a
// well-formed UTF-8 panel value needs no Latin-1 round-trip here).
func brandRawValue(v string) string {
	v = strings.TrimSpace(v)
	v = strings.Trim(v, `"'`)
	v = strings.TrimSpace(v)
	if !utf8.ValidString(v) {
		v = strings.ToValidUTF8(v, "")
	}
	return v
}

// brandParseBool accepts the shared protocol's truthy spellings; anything
// else — including absence — is false.
func brandParseBool(v string) bool {
	switch strings.ToLower(brandRawValue(v)) {
	case "true", "1", "yes", "on":
		return true
	}
	return false
}

// brandCleanHexColor normalizes to lowercase #rrggbb (mirrors the frontend
// normalizeHex); empty on invalid input.
func brandCleanHexColor(v string) string {
	v = strings.ToLower(brandRawValue(v))
	if !brandHexColorRe.MatchString(v) {
		return ""
	}
	if len(v) == 4 { // #rgb -> #rrggbb
		return "#" + strings.Repeat(string(v[1]), 2) +
			strings.Repeat(string(v[2]), 2) +
			strings.Repeat(string(v[3]), 2)
	}
	return v
}

// brandCleanURL keeps https:// (host required) and tg:// links only; every
// other scheme — including javascript:, http:, file: — drops the field.
func brandCleanURL(v string) string {
	v = brandRawValue(v)
	if v == "" || len(v) > brandMaxURLLen {
		return ""
	}
	u, err := url.Parse(v)
	if err != nil {
		return ""
	}
	switch strings.ToLower(u.Scheme) {
	case "https":
		if u.Host == "" {
			return ""
		}
		return v
	case "tg":
		return v
	}
	return ""
}

// brandCleanCount parses a non-negative device counter. Anything unparseable,
// negative or absurd (a panel bug should not render "4294967295 devices")
// degrades to 0 = not reported.
func brandCleanCount(v string) int {
	n, err := strconv.Atoi(brandRawValue(v))
	if err != nil || n < 0 || n > brandMaxDeviceCount {
		return 0
	}
	return n
}

// brandCleanString caps display strings; over-limit values drop to empty
// (absent) rather than truncating — a cap violation is a protocol violation.
func brandCleanString(v string, maxLen int) string {
	v = brandRawValue(v)
	if v == "" || len(v) > maxLen {
		return ""
	}
	return v
}

// parseBrandManifest builds a manifest from a header lookup. Returns nil when
// the branding gate is absent/false — callers treat nil as "no branding".
// Invalid individual fields degrade to absent without poisoning the rest.
func parseBrandManifest(get func(string) string) *BrandManifest {
	if !brandParseBool(get(brandHdrEnabled)) {
		return nil
	}
	return &BrandManifest{
		Enabled: true,

		Name:         brandCleanString(get(brandHdrName), brandMaxNameLen),
		Tagline:      brandCleanString(get(brandHdrTagline), brandMaxTaglineLen),
		LogoURL:      brandCleanURL(get(brandHdrLogoURL)),
		LogoLightURL: brandCleanURL(get(brandHdrLogoLightURL)),
		AccentColor:  brandCleanHexColor(get(brandHdrAccentColor)),

		WebsiteURL:  brandCleanURL(get(brandHdrWebsiteURL)),
		SupportURL:  brandCleanURL(get(brandHdrSupportURL)),
		TelegramURL: brandCleanURL(get(brandHdrTelegramURL)),
		BotURL:      brandCleanURL(get(brandHdrBotURL)),
		PrivacyURL:  brandCleanURL(get(brandHdrPrivacyURL)),
		TermsURL:    brandCleanURL(get(brandHdrTermsURL)),
		HelpURL:     brandCleanURL(get(brandHdrHelpURL)),
		StatusURL:   brandCleanURL(get(brandHdrStatusURL)),
		RenewURL:    brandCleanURL(get(brandHdrRenewURL)),
		CabinetURL:  brandCleanURL(get(brandHdrCabinetURL)),

		UserDisplayName: brandCleanString(get(brandHdrUserDisplayName), brandMaxDisplayNameLen),
		Greeting:        brandCleanString(get(brandHdrGreeting), brandMaxGreetingLen),

		DevicesUsed:  brandCleanCount(get(brandHdrDevicesUsed)),
		DevicesLimit: brandCleanCount(get(brandHdrDevicesLimit)),

		HideGlobalMode:   brandParseBool(get(brandHdrHideGlobalMode)),
		HideProxyMode:    brandParseBool(get(brandHdrHideProxyMode)),
		HideLocalConfigs: brandParseBool(get(brandHdrHideLocalConfigs)),
		HideAdvanced:     brandParseBool(get(brandHdrHideAdvanced)),
	}
}

// ---------------------------------------------------------------------------
// Per-profile persistence (same lifecycle as subscription.cache.yaml).
// ---------------------------------------------------------------------------

func brandManifestPath(dataDir string) string {
	return filepath.Join(dataDir, "brand.manifest.json")
}

// persistBrandManifestFromHeaders captures branding off a subscription fetch
// response. Trust rules applied here, not at read time: header values are only
// honored when the subscription URL itself is HTTPS. A response without the
// gate clears any previously cached manifest (panel turned branding off).
func persistBrandManifestFromHeaders(dataDir, subURL string, hdr http.Header) {
	if strings.TrimSpace(dataDir) == "" || hdr == nil {
		return
	}
	if u, err := url.Parse(strings.TrimSpace(subURL)); err != nil || !strings.EqualFold(u.Scheme, "https") {
		return
	}
	prev := readBrandManifest(dataDir)
	m := parseBrandManifest(hdr.Get)
	if m == nil {
		_ = os.Remove(brandManifestPath(dataDir))
		_ = os.Remove(brandLogoPath(dataDir, false))
		_ = os.Remove(brandLogoPath(dataDir, true))
		return
	}
	b, err := json.Marshal(m)
	if err != nil {
		return
	}
	_ = os.MkdirAll(dataDir, 0o755)
	_ = atomicWriteFile(brandManifestPath(dataDir), b, 0o600)
	// Logos refresh off the hot path; render always reads the disk cache.
	go refreshBrandLogos(dataDir, prev, m)
}

// readBrandManifest loads the cached manifest for a profile dataDir; nil when
// absent or unreadable (both mean stock UI).
func readBrandManifest(dataDir string) *BrandManifest {
	if strings.TrimSpace(dataDir) == "" {
		return nil
	}
	b, err := os.ReadFile(brandManifestPath(dataDir))
	if err != nil {
		return nil
	}
	var m BrandManifest
	if err := json.Unmarshal(b, &m); err != nil || !m.Enabled {
		return nil
	}
	return &m
}

// ---------------------------------------------------------------------------
// Operator logo cache: downloaded once per manifest change, rendered from
// disk only (offline restarts must not hit the network).
// ---------------------------------------------------------------------------

const brandLogoMaxBytes = 512 << 10 // 512 KB per variant

func brandLogoPath(dataDir string, light bool) string {
	if light {
		return filepath.Join(dataDir, "brand.logo.light")
	}
	return filepath.Join(dataDir, "brand.logo")
}

// refreshBrandLogos re-downloads a logo variant only when its URL changed (or
// the cache file vanished); unchanged manifests cost zero requests.
func refreshBrandLogos(dataDir string, prev, cur *BrandManifest) {
	prevURL := func(light bool) string {
		if prev == nil {
			return ""
		}
		if light {
			return prev.LogoLightURL
		}
		return prev.LogoURL
	}
	for _, v := range []struct {
		light bool
		url   string
	}{
		{false, cur.LogoURL},
		{true, cur.LogoLightURL},
	} {
		path := brandLogoPath(dataDir, v.light)
		if v.url == "" {
			_ = os.Remove(path)
			continue
		}
		if v.url == prevURL(v.light) {
			if _, err := os.Stat(path); err == nil {
				continue
			}
		}
		if b := fetchBrandLogo(v.url); b != nil {
			_ = atomicWriteFile(path, b, 0o600)
		}
	}
}

func fetchBrandLogo(rawURL string) []byte {
	client := &http.Client{Timeout: 15 * time.Second}
	resp, err := client.Get(rawURL)
	if err != nil {
		return nil
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil
	}
	b, err := io.ReadAll(io.LimitReader(resp.Body, brandLogoMaxBytes+1))
	if err != nil || len(b) == 0 || len(b) > brandLogoMaxBytes {
		return nil
	}
	if !strings.HasPrefix(http.DetectContentType(b), "image/") {
		return nil
	}
	return b
}

// readBrandLogoDataURI returns the cached logo as a data: URI (empty when no
// cache) so the webview renders it without any network or filesystem access.
func readBrandLogoDataURI(dataDir string, light bool) string {
	b, err := os.ReadFile(brandLogoPath(dataDir, light))
	if err != nil || len(b) == 0 {
		return ""
	}
	return "data:" + http.DetectContentType(b) + ";base64," + base64.StdEncoding.EncodeToString(b)
}
