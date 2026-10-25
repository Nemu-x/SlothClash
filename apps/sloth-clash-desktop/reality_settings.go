package main

import "SlothClashDesktop/internal/runtimecfg"

// RealitySettings is the REALITY handshake policy. It reaches the core as two
// top-level config keys read by core patch 0003 (docs/core-patches.md):
//
//	reality-mlkem: auto | on | off      — offer the X25519MLKEM768 key share
//	reality-client-version: "x.y.z"     — client version Xray minClientVer/maxClientVer gate on
//
// Why it exists: Xray servers before 2025 drop a hybrid ClientHello, from
// 26.9.8 on they reject a classic one, and none announce which they are. Auto
// lets the core learn per server; Always/Never are manual overrides for a
// network where learning is not wanted. An unpatched core ignores both keys.
type RealitySettings struct {
	MLKEM         string `json:"mlkem,omitempty"`         // "" = auto; auto|always|never
	ClientVersion string `json:"clientVersion,omitempty"` // "" = subscription value, else core default
}

// SetRealitySettings stores the policy and reloads the running core, so new
// connections use it right away (open connections keep their handshake).
func (a *App) SetRealitySettings(next RealitySettings) (DesktopPrefs, error) {
	version, err := runtimecfg.ParseRealityClientVersion(next.ClientVersion)
	if err != nil {
		return currentDesktopPrefs(), err
	}
	next.ClientVersion = version
	next.MLKEM = runtimecfg.NormalizeRealityMLKEM(next.MLKEM)
	if next.MLKEM == "auto" {
		next.MLKEM = ""
	}

	prefsMu.Lock()
	prefsCurrent.Reality = next
	snapshot := prefsCurrent
	savePrefsBestEffort(snapshot)
	prefsMu.Unlock()

	a.triggerRuntimeReloadForPrefs()
	return snapshot, nil
}
