package runtimecfg

import (
	"net/http"
	"runtime"
	"strings"
)

// ApplyIdentityHeaders sets provider-facing client metadata (import / refresh subscription HTTP).
// The x-hwid header obeys c.SendHWID (PrivacySettings.HwidEnabled) — when the
// user has toggled it off in Advanced, the header is omitted and other identity
// fields (x-device-os, x-ver-os, x-device-model, x-app-version) are still sent
// so providers can route to platform-specific configs without device-unique
// identifiers.
func ApplyIdentityHeaders(req *http.Request, c SubscriptionClient) {
	if req == nil {
		return
	}
	if c.HWID != "" && c.SendHWID {
		req.Header.Set("x-hwid", c.HWID)
	}
	req.Header.Set("x-device-os", runtime.GOOS)
	if c.OSVersion != "" {
		req.Header.Set("x-ver-os", c.OSVersion)
	}
	if c.DeviceModel != "" {
		req.Header.Set("x-device-model", c.DeviceModel)
	}
	if v := strings.TrimSpace(c.AppVersion); v != "" {
		req.Header.Set("x-app-version", v)
	}
}
