package main

import (
	"testing"
	"testing/fstest"
)

func TestCoreBuildInfoFromFS(t *testing.T) {
	bundle := fstest.MapFS{
		"build/sidecar/core-version.txt": {Data: []byte("v1.19.32\n")},
		"build/sidecar/core-build.json": {Data: []byte(`{"version":"v1.19.32","source":"patched","release":"core-v1.19.32-sloth.1",
			"patches":["0001-a.patch","0002-b.patch"],"sha256":"x"}`)},
	}
	got := coreBuildInfoFromFS(bundle)
	if got.Version != "v1.19.32" || got.Source != "patched" || len(got.Patches) != 2 {
		t.Fatalf("parsed %+v", got)
	}
	if got.PatchesURL != slothRepoURL+"/tree/core-v1.19.32-sloth.1/core/patches/mihomo" {
		t.Errorf("patches url %q", got.PatchesURL)
	}

	// A build without the file (dev, or before prebuild) is "unknown", never a guess.
	bare := coreBuildInfoFromFS(fstest.MapFS{"build/sidecar/core-version.txt": {Data: []byte("v1.19.32")}})
	if bare.Source != "" || len(bare.Patches) != 0 || bare.Patches == nil {
		t.Errorf("bare bundle = %+v, want unknown source and an empty (non-nil) patch list", bare)
	}
	if bare.PatchesURL != slothRepoURL+"/tree/main/core/patches/mihomo" {
		t.Errorf("bare patches url %q", bare.PatchesURL)
	}
}
