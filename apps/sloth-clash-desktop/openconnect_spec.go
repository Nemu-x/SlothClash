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
			SHA256:  "22fad90065f52ec97930078567f700fabc9aa4f071686724ffa53f2177dd3527",
			BinRel:  "openconnect",
		}, true
	case runtime.GOOS == "darwin" && runtime.GOARCH == "amd64":
		return componentSpec{
			Name:    "openconnect",
			Version: "9.12-macos-amd64",
			URL:     openconnectBundleBase + "/openconnect-macos-amd64.tar.gz",
			SHA256:  "0c2914d6e88d66369506a75e02cdf141a5824e3872853aad526ba985fb345c7b",
			BinRel:  "openconnect",
		}, true
	case runtime.GOOS == "windows" && runtime.GOARCH == "amd64":
		return componentSpec{
			Name:    "openconnect",
			Version: "9.12-windows-amd64-r3", // r3: dropped wintun.dll → OpenConnect uses TAP-Windows (coexists with mihomo's wintun)
			URL:     openconnectBundleBase + "/openconnect-windows-amd64.tar.gz",
			SHA256:  "510f2246edd5ae4063e0283950585885ba88315743223c43bbb6cb25cc83358a",
			BinRel:  "openconnect.exe",
		}, true
	// windows/arm64 is intentionally unsupported: there's no arm64 desktop build,
	// and the from-source arm64 OpenConnect CI job routinely hangs for hours. Re-add
	// the spec case + the windows-arm64 CI job if a Windows-arm64 app ever ships.
	// Linux: the service platform backend + CI bundle are ready, but the desktop
	// Linux IPC (unix-socket client) + service install are not wired yet, so the
	// tab stays hidden there until they land — enable this case then.
	//
	// case runtime.GOOS == "linux" && runtime.GOARCH == "amd64":
	// 	return componentSpec{
	// 		Name: "openconnect", Version: "9.12-linux-amd64",
	// 		URL: openconnectBundleBase + "/openconnect-linux-amd64.tar.gz",
	// 		SHA256: "408dd642c96db515e6593985a720bd71d76952c15b980807fc51e9a52ecc52ff", BinRel: "openconnect",
	// 	}, true
	// case runtime.GOOS == "linux" && runtime.GOARCH == "arm64":
	// 	return componentSpec{
	// 		Name: "openconnect", Version: "9.12-linux-arm64",
	// 		URL: openconnectBundleBase + "/openconnect-linux-arm64.tar.gz",
	// 		SHA256: "b01e126d6946bc7afdeea9cd7d4afaa157a215967fde8bd6fbcd1fde72add324", BinRel: "openconnect",
	// 	}, true
	default:
		return componentSpec{}, false
	}
}
