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
// The 0.2.0 line is the first release carrying the IPC LPE hardening
// (core hash-pinning, log-dir validation, DACL narrowing). A user still on an
// older service is prompted to have it reinstalled.
const expectedSlothServiceVersion = "0.2.0"

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
}

// GetServiceInfo reports the privileged service status + version and whether it
// is older than this client requires. Safe to call anytime; never blocks the UI
// for long (the IPC probe is short-timeout).
func (a *App) GetServiceInfo() ServiceRuntimeInfo {
	a.mu.RLock()
	installed := a.state.Service.Installed
	running := a.state.Service.Running
	a.mu.RUnlock()

	info := ServiceRuntimeInfo{
		Installed:       installed,
		Running:         running,
		ExpectedVersion: expectedSlothServiceVersion,
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
