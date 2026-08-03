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
			Version: "9.12-macos-arm64",
			URL:     openconnectBundleBase + "/openconnect-macos-arm64.tar.gz",
			SHA256:  "99bff9c0c0d06f3bf73a8e6be7a5d09db8461412c80dcd9500d309a6aa3af839",
			BinRel:  "openconnect",
		}, true
	case runtime.GOOS == "darwin" && runtime.GOARCH == "amd64":
		return componentSpec{
			Name:    "openconnect",
			Version: "9.12-macos-amd64",
			URL:     openconnectBundleBase + "/openconnect-macos-amd64.tar.gz",
			SHA256:  "", // TODO: paste sha once the macos-intel job publishes
			BinRel:  "openconnect",
		}, true
	case runtime.GOOS == "windows" && runtime.GOARCH == "amd64":
		return componentSpec{
			Name:    "openconnect",
			Version: "9.12-windows-amd64",
			URL:     openconnectBundleBase + "/openconnect-windows-amd64.tar.gz",
			SHA256:  "aa8f41d56f2ccf0329b0ddd6975c737c9b7aa8802f943f0d4ccd73e0187f92fb",
			BinRel:  "openconnect.exe",
		}, true
	case runtime.GOOS == "windows" && runtime.GOARCH == "arm64":
		return componentSpec{
			Name:    "openconnect",
			Version: "9.12-windows-arm64",
			URL:     openconnectBundleBase + "/openconnect-windows-arm64.tar.gz",
			SHA256:  "", // TODO: paste sha once the windows-arm64 job publishes
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
	// 		SHA256: "5dee1c4d88ee09763756d91242c9eb75ccccf1b0b11352a61f6ffb0da5a08f44", BinRel: "openconnect",
	// 	}, true
	default:
		return componentSpec{}, false
	}
}
