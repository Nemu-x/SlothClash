package main

import "runtime"

// openconnectComponentSpec returns the pinned OpenConnect bundle to fetch on the
// current platform, or ok=false when the platform isn't supported.
//
// Bundles are published as release assets in the sloth-clash-service-ipc repo
// (NOT the app repo, so the app's release list stays clean) by
// .github/workflows/publish-openconnect-component.yml there. The SHA-256 that
// workflow prints is pinned below for fail-closed verification; until a bundle is
// published + its SHA pasted here, ensureComponent fails closed with a clear
// message. Bumping a bundle = bump Version + URL + SHA together.
const openconnectBundleBase = "https://github.com/Nemu-x/sloth-clash-service-ipc/releases/download/components-openconnect-9.21"

func openconnectComponentSpec() (componentSpec, bool) {
	switch {
	case runtime.GOOS == "darwin" && runtime.GOARCH == "arm64":
		return componentSpec{
			Name:    "openconnect",
			Version: "9.21-macos-arm64",
			URL:     openconnectBundleBase + "/openconnect-macos-arm64.tar.gz",
			SHA256:  "", // TODO: paste sha from the publish-openconnect-component run
			BinRel:  "openconnect",
		}, true
	case runtime.GOOS == "windows" && runtime.GOARCH == "amd64":
		return componentSpec{
			Name:    "openconnect",
			Version: "9.21-windows-amd64",
			URL:     openconnectBundleBase + "/openconnect-windows-amd64.tar.gz",
			SHA256:  "", // TODO: paste sha from the publish-openconnect-component run
			BinRel:  "openconnect.exe",
		}, true
	// Linux: the service platform backend + CI bundle are ready, but the desktop
	// Linux IPC (unix-socket client) + service install are not wired yet, so the
	// tab stays hidden there until they land — enable this case then.
	//
	// case runtime.GOOS == "linux" && runtime.GOARCH == "amd64":
	// 	return componentSpec{
	// 		Name: "openconnect", Version: "9.21-linux-amd64",
	// 		URL: openconnectBundleBase + "/openconnect-linux-amd64.tar.gz",
	// 		SHA256: "", BinRel: "openconnect",
	// 	}, true
	default:
		return componentSpec{}, false
	}
}
