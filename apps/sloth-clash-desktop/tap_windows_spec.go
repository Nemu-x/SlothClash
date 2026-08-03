package main

import "runtime"

// tapWindowsBundleBase is the release the TAP-Windows driver component is fetched
// from (same tag as the OpenConnect bundles).
const tapWindowsBundleBase = "https://github.com/Nemu-x/sloth-clash-service-ipc/releases/download/components-openconnect-9.21"

// tapWindowsComponentSpec returns the pinned TAP-Windows driver bundle to fetch
// on Windows, or ok=false where it isn't needed.
//
// On Windows OpenConnect defaults to wintun, but mihomo's core also owns a wintun
// adapter and the two collide on the single wintun.sys driver ("Failed to
// register rings: already initialized"). So corp OpenConnect runs on OpenVPN's
// TAP-Windows driver instead — a separate driver, no conflict. The bundle carries
// the OpenVPN WHQL-signed driver (OemVista.inf + tap0901.sys/.cat) plus
// tapinstall.exe; the privileged service installs it on demand before the first
// corp connect (see /corp/ensure-driver). macOS/Linux use native tun → ok=false.
//
// Bumping the bundle = bump Version + SHA together (see the service-ipc
// publish-tap-windows-component workflow).
func tapWindowsComponentSpec() (componentSpec, bool) {
	if runtime.GOOS != "windows" || runtime.GOARCH != "amd64" {
		return componentSpec{}, false
	}
	return componentSpec{
		Name:    "tap-windows",
		Version: "9.24.7-amd64",
		URL:     tapWindowsBundleBase + "/tap-windows-amd64.tar.gz",
		SHA256:  "4fc88fe526f128597a7175a45dcbf7c9809d5d7de73eac9105a9fed4bc220d49",
		BinRel:  "tapinstall.exe",
	}, true
}
