package main

import (
	"context"
	"errors"
	"strconv"
	"strings"
	"time"
)

var errIPCServiceVersionUnavailable = errors.New("service version unavailable")

// expectedSlothServiceVersion is the minimum privileged-service version this
// client build requires. Bump it IN LOCKSTEP with the pinned service release
// (scripts/prebuild.mjs SERVICE_DOWNLOAD_TAG) whenever the service ships a
// security-relevant change the client must not run against an older build.
//
// 2.3.0 was the first release carrying the IPC LPE hardening (core hash-pinning,
// log-dir validation, DACL narrowing).
//
// 2.4.0 moves the service binary itself out of the per-user temp dir it was
// registered from into an admin-only location: on Windows a `service` folder
// next to the app (%ProgramFiles%\Nemu-x\Sloth Clash\service — the client passes
// --install-dir, see installServiceElevatedWindows), /usr/local/lib/sloth-clash
// on Linux; macOS already used /Library/PrivilegedHelperTools. Before that, a non-admin could swap the
// binary the SYSTEM/root service executes — the exact Layer-1 boundary the
// service is meant to hold. Requiring 2.4.0 makes existing installs surface the
// reinstall banner, which performs the migration.
//
// 2.4.2 adds the DELETE /tun/remove endpoint: the SYSTEM service force-removes a
// stale wintun adapter left registered by a force-killed core, which is what
// makes the next TUN create fail with "access is denied" (a state that survives
// a reboot and a service reinstall). The client's on-demand recovery
// (maybeRecoverStuckTunAdapter) and startup cleanup call this endpoint; an older
// service answers 404 and the stuck adapter can never be cleared automatically.
// Requiring 2.4.2 makes existing installs surface the reinstall banner so every
// user gets the recovery path.
//
// 2.7.0 adds the corporate-VPN sidecar endpoints (/corp/start|stop|status): the
// SYSTEM/root service owns the OpenConnect process, installs the corp routes onto
// the tunnel interface, and owns the split-DNS overlay. An older service has no
// /corp/* routes and answers 404, so the corp tab could never function; requiring
// 2.7.0 makes existing installs surface the reinstall banner (the "update service"
// nudge) that migrates them to the corp-capable build.
//
// ⚠️ SEQUENCING: only ship this bump once the 2.7.0 service binaries are actually
// published (Release CI → rolling per-triple tags) and pulled by prebuild —
// otherwise the banner can never clear.
const expectedSlothServiceVersion = "2.7.0"

// ServiceRuntimeInfo is the health/version snapshot the UI uses to decide
// whether to nudge the user to update the privileged helper service.
type ServiceRuntimeInfo struct {
	Installed       bool   `json:"installed"`
	Running         bool   `json:"running"`
	Reachable       bool   `json:"reachable"`
	Version         string `json:"version,omitempty"`
	ExpectedVersion string `json:"expectedVersion"`
	// UpdateRequired is true only when we positively read a version older than
	// expected. An unreachable/unknown service does NOT set it — we never nag on
	// a mere probe failure.
	UpdateRequired bool `json:"updateRequired"`
	// CorePinMismatch mirrors ServiceState: the service refused the current
	// core because its pinned hash is stale (core bump on upgrade). Like
	// UpdateRequired it asks the user to reinstall the service, but for a
	// different reason. See core_pin_mismatch.go.
	CorePinMismatch bool `json:"corePinMismatch"`
	// Unreachable mirrors ServiceState: installed but not reachable after a
	// start attempt (reaped temp binary / crash after upgrade). Same reinstall
	// remedy as CorePinMismatch, different banner message.
	Unreachable bool `json:"unreachable"`
}

// GetServiceInfo reports the privileged service status + version and whether it
// is older than this client requires. Safe to call anytime; never blocks the UI
// for long (the IPC probe is short-timeout).
func (a *App) GetServiceInfo() ServiceRuntimeInfo {
	a.mu.RLock()
	installed := a.state.Service.Installed
	running := a.state.Service.Running
	pinMismatch := a.state.Service.CorePinMismatch
	unreachable := a.state.Service.Unreachable
	a.mu.RUnlock()

	info := ServiceRuntimeInfo{
		Installed:       installed,
		Running:         running,
		ExpectedVersion: expectedSlothServiceVersion,
		CorePinMismatch: pinMismatch,
		Unreachable:     unreachable,
	}
	if !installed {
		return info
	}

	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	version, err := ipcSlothServiceVersion(ctx)
	if err != nil {
		return info
	}
	info.Reachable = true
	info.Version = version
	if compareServiceVersions(version, expectedSlothServiceVersion) < 0 {
		info.UpdateRequired = true
	}
	return info
}

// compareServiceVersions compares dotted numeric versions (semver core, any
// pre-release/build suffix ignored). Returns -1 if a<b, 0 if equal, 1 if a>b.
// Unparseable segments are treated as 0 so a malformed version never falsely
// reports "newer than expected".
func compareServiceVersions(a, b string) int {
	pa := parseVersionCore(a)
	pb := parseVersionCore(b)
	for i := 0; i < 3; i++ {
		if pa[i] < pb[i] {
			return -1
		}
		if pa[i] > pb[i] {
			return 1
		}
	}
	return 0
}

func parseVersionCore(v string) [3]int {
	v = strings.TrimSpace(v)
	v = strings.TrimPrefix(v, "v")
	// Drop any -prerelease / +build suffix.
	if i := strings.IndexAny(v, "-+"); i >= 0 {
		v = v[:i]
	}
	var out [3]int
	for i, seg := range strings.SplitN(v, ".", 3) {
		if i > 2 {
			break
		}
		n, err := strconv.Atoi(strings.TrimSpace(seg))
		if err != nil {
			n = 0
		}
		out[i] = n
	}
	return out
}
