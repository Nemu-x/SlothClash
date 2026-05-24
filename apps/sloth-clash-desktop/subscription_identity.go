package main

import (
	"crypto/sha256"
	"encoding/hex"
	"net/http"
	"os"
	"runtime"
	"strings"
	"sync"
)

// SubscriptionDeviceIdentityPublic matches subscription HTTP headers (x-hwid, x-device-os, …) for UI copy-paste.
type SubscriptionDeviceIdentityPublic struct {
	HWID        string `json:"hwid"`
	DeviceOS    string `json:"deviceOs"`
	OSVersion   string `json:"osVersion"`
	DeviceModel string `json:"deviceModel"`
	AppVersion  string `json:"appVersion"`
}

// Opaque stable device id for subscription providers (SHA-256 hex); not reversible to raw machine secrets.
type subscriptionDeviceIdentity struct {
	HWID        string
	OSVersion   string
	DeviceModel string
}

var (
	subscriptionIdentityOnce sync.Once
	subscriptionIdentity     subscriptionDeviceIdentity
)

func subscriptionDeviceIdentityCurrent() subscriptionDeviceIdentity {
	subscriptionIdentityOnce.Do(initSubscriptionDeviceIdentity)
	return subscriptionIdentity
}

func initSubscriptionDeviceIdentity() {
	raw := strings.TrimSpace(rawStableMachineID())
	if raw == "" {
		raw = hostnameFallbackRaw()
	}
	sum := sha256.Sum256([]byte(raw))
	subscriptionIdentity = subscriptionDeviceIdentity{
		HWID:        hex.EncodeToString(sum[:]),
		OSVersion:   truncateHeaderValue(hostOSVersionLabel(), 120),
		DeviceModel: truncateHeaderValue(hostDeviceModelLabel(), 160),
	}
}

func hostnameFallbackRaw() string {
	h, err := os.Hostname()
	if err != nil || strings.TrimSpace(h) == "" {
		return "unknown-host"
	}
	return "host:" + strings.TrimSpace(h)
}

func truncateHeaderValue(s string, maxRunes int) string {
	s = strings.TrimSpace(s)
	if s == "" {
		return ""
	}
	r := []rune(s)
	if len(r) <= maxRunes {
		return s
	}
	if maxRunes < 4 {
		return string(r[:maxRunes])
	}
	return string(r[:maxRunes-1]) + "…"
}

// applySubscriptionIdentityHeaders sets provider-facing client metadata (import / refresh subscription HTTP).
// The x-hwid header obeys PrivacySettings.HwidEnabled — when the user has
// toggled it off in Advanced, the header is omitted and other identity fields
// (x-device-os, x-ver-os, x-device-model, x-app-version) are still sent so
// providers can route to platform-specific configs without device-unique
// identifiers.
func applySubscriptionIdentityHeaders(req *http.Request) {
	if req == nil {
		return
	}
	id := subscriptionDeviceIdentityCurrent()
	if id.HWID != "" && currentDesktopPrefs().Privacy.IsHwidEnabled() {
		req.Header.Set("x-hwid", id.HWID)
	}
	req.Header.Set("x-device-os", runtime.GOOS)
	if id.OSVersion != "" {
		req.Header.Set("x-ver-os", id.OSVersion)
	}
	if id.DeviceModel != "" {
		req.Header.Set("x-device-model", id.DeviceModel)
	}
	if v := strings.TrimSpace(AppVersion); v != "" {
		req.Header.Set("x-app-version", v)
	}
}

// rawStableMachineID returns a platform-specific stable string before hashing (see per-OS files).
func rawStableMachineID() string { return rawStableMachineIDPlatform() }

func hostOSVersionLabel() string { return hostOSVersionLabelPlatform() }

func hostDeviceModelLabel() string { return hostDeviceModelLabelPlatform() }

// GetSubscriptionDeviceIdentity returns device metadata sent with subscription requests (support / diagnostics).
func (a *App) GetSubscriptionDeviceIdentity() SubscriptionDeviceIdentityPublic {
	_ = a
	id := subscriptionDeviceIdentityCurrent()
	return SubscriptionDeviceIdentityPublic{
		HWID:        id.HWID,
		DeviceOS:    runtime.GOOS,
		OSVersion:   id.OSVersion,
		DeviceModel: id.DeviceModel,
		AppVersion:  strings.TrimSpace(AppVersion),
	}
}
