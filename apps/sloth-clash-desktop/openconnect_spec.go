package main

import "runtime"

// openconnectComponentSpec returns the pinned OpenConnect bundle to fetch on the
// current platform, or ok=false when the platform is not yet supported (P1 ships
// macOS-only). URL/SHA256 are filled from the CI-published release asset (see
// .github/workflows and corp-vpn-coexistence task 2); until that asset exists
// they stay empty and ensureComponent fails closed with a clear message.
//
// Version is the human-pinned tag; bumping the bundle = bump Version + URL + SHA
// together so componentInstalled invalidates the old unpack.
func openconnectComponentSpec() (componentSpec, bool) {
	switch {
	case runtime.GOOS == "darwin" && runtime.GOARCH == "arm64":
		return componentSpec{
			Name:    "openconnect",
			Version: "9.21-macos-arm64",
			URL:     "https://github.com/Nemu-x/SlothClash/releases/download/components-openconnect-9.21/openconnect-macos-arm64.tar.gz",
			SHA256:  "", // TODO(brick2): paste sha from the publish-openconnect-component run
			BinRel:  "openconnect",
		}, true
	case runtime.GOOS == "darwin" && runtime.GOARCH == "amd64":
		return componentSpec{
			Name:    "openconnect",
			Version: "9.21-macos-amd64",
			URL:     "https://github.com/Nemu-x/SlothClash/releases/download/components-openconnect-9.21/openconnect-macos-amd64.tar.gz",
			SHA256:  "", // TODO(brick2): paste sha from the publish-openconnect-component run
			BinRel:  "openconnect",
		}, true
	default:
		return componentSpec{}, false
	}
}
