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
			SHA256:  "94bd8f6ad912611cf0e4f5985dc257b1ee15201d83b5f31fc16e90a7c54cc317",
			BinRel:  "openconnect",
		}, true
	case runtime.GOOS == "darwin" && runtime.GOARCH == "amd64":
		return componentSpec{
			Name:    "openconnect",
			Version: "9.12-macos-amd64",
			URL:     openconnectBundleBase + "/openconnect-macos-amd64.tar.gz",
			SHA256:  "0c963112e50b80471cc715b9502bee7d28f92002ea0b0a7a78edfba9e9a502d4",
			BinRel:  "openconnect",
		}, true
	case runtime.GOOS == "windows" && runtime.GOARCH == "amd64":
		return componentSpec{
			Name:    "openconnect",
			Version: "9.12-windows-amd64",
			URL:     openconnectBundleBase + "/openconnect-windows-amd64.tar.gz",
			SHA256:  "266c86ac94fa13ed8d7af8921bdd56526d2c89a087e59f398fdb8ea1f6d12849",
			BinRel:  "openconnect.exe",
		}, true
	case runtime.GOOS == "windows" && runtime.GOARCH == "arm64":
		return componentSpec{
			Name:    "openconnect",
			Version: "9.12-windows-arm64",
			URL:     openconnectBundleBase + "/openconnect-windows-arm64.tar.gz",
			SHA256:  "09122fc5948876b68c8c0ebcfcabbc5bfdb409c9f68126be734c4b3de01cf629",
			BinRel:  "openconnect.exe",
		}, true
	// Linux: the service platform backend + CI bundle are ready, but the desktop
	// Linux IPC (unix-socket client) + service install are not wired yet, so the
	// tab stays hidden there until they land — enable this case then.
	//
	// case runtime.GOOS == "linux" && runtime.GOARCH == "amd64":
	// 	return componentSpec{
	// 		Name: "openconnect", Version: "9.12-linux-amd64",
	// 		URL: openconnectBundleBase + "/openconnect-linux-amd64.tar.gz",
	// 		SHA256: "17e5037cfc01bcc411eaa866a2793cacefa5b8ebe845cea29041dd39eeaffcb2", BinRel: "openconnect",
	// 	}, true
	// case runtime.GOOS == "linux" && runtime.GOARCH == "arm64":
	// 	return componentSpec{
	// 		Name: "openconnect", Version: "9.12-linux-arm64",
	// 		URL: openconnectBundleBase + "/openconnect-linux-arm64.tar.gz",
	// 		SHA256: "4a8bd7fb9811a7c7a513ba3ec2c7fa263d86e9bd4cc8395c4c429aae2add4c2e", BinRel: "openconnect",
	// 	}, true
	default:
		return componentSpec{}, false
	}
}
