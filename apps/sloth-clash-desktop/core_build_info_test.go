package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"testing/fstest"
)

func TestCoreBuildInfoFromFS(t *testing.T) {
	bundle := fstest.MapFS{
		"build/sidecar/core-version.txt": {Data: []byte("v1.19.32\n")},
		"build/sidecar/core-build.json": {Data: []byte(`{"version":"v1.19.32","source":"patched","ref":"9a09ac799e9c63ed170cbbc039670be65311afff",
			"patches":["0001-a.patch","0002-b.patch"],"features":["xdrive","chain","bad token","chain"],"sha256":"x"}`)},
	}
	got := coreBuildInfoFromFS(bundle)
	if got.Version != "v1.19.32" || got.Source != "patched" || len(got.Patches) != 2 {
		t.Fatalf("parsed %+v", got)
	}
	if strings.Join(got.Features, ",") != "xdrive,chain" {
		t.Errorf("features %q, want the well-formed distinct tokens in order", got.Features)
	}
	if got.PatchesURL != slothRepoURL+"/tree/9a09ac799e9c63ed170cbbc039670be65311afff/core/patches/mihomo" {
		t.Errorf("patches url %q", got.PatchesURL)
	}

	// A build without the file (dev, or before prebuild) is "unknown", never a guess.
	bare := coreBuildInfoFromFS(fstest.MapFS{"build/sidecar/core-version.txt": {Data: []byte("v1.19.32")}})
	if bare.Features == nil || len(bare.Features) != 0 {
		t.Errorf("bare bundle features = %#v, want empty (non-nil): no file, no claims", bare.Features)
	}
	if bare.Source != "" || len(bare.Patches) != 0 || bare.Patches == nil {
		t.Errorf("bare bundle = %+v, want unknown source and an empty (non-nil) patch list", bare)
	}
	if bare.PatchesURL != slothRepoURL+"/tree/main/core/patches/mihomo" {
		t.Errorf("bare patches url %q", bare.PatchesURL)
	}
}

// TestManifestFeatureTokens checks the patch -> token mapping the subscription
// User-Agent is built from (core/mihomo/manifest.json; prebuild writes the
// tokens of the built series, in scripts/core-build.mjs FEATURE_ORDER, into
// core-build.json). A token without its patch, a malformed token or a token
// declared twice would make the UA claim a feature the core does not have.
func TestManifestFeatureTokens(t *testing.T) {
	b, err := os.ReadFile(filepath.Join("..", "..", "core", "mihomo", "manifest.json"))
	if err != nil {
		t.Fatal(err)
	}
	var m struct {
		Patches []struct {
			File    string `json:"file"`
			Feature string `json:"feature"`
		} `json:"patches"`
	}
	if err := json.Unmarshal(b, &m); err != nil {
		t.Fatal(err)
	}
	byFeature := map[string]string{}
	for _, p := range m.Patches {
		if p.Feature == "" {
			continue
		}
		if got := validCoreFeatures([]string{p.Feature}); len(got) != 1 {
			t.Errorf("patch %s: feature %q is not a valid UA token", p.File, p.Feature)
		}
		if prev, dup := byFeature[p.Feature]; dup {
			t.Errorf("feature %q declared by %s and %s", p.Feature, prev, p.File)
		}
		byFeature[p.Feature] = p.File
		if _, err := os.Stat(filepath.Join("..", "..", "core", "patches", "mihomo", p.File)); err != nil {
			t.Errorf("feature %q points at a missing patch: %v", p.Feature, err)
		}
	}
	want := map[string]string{
		"xdrive":   "0004-vless-xdrive.patch",
		"chain":    "0006-chain-group.patch",
		"mldsa65":  "0007-reality-mldsa65-verify.patch",
		"pin-list": "0009-tls-pin-and-name-lists.patch",
		"ech-xray": "0010-ech-xray-forms.patch",
	}
	for f, file := range want {
		if byFeature[f] != file {
			t.Errorf("feature %q: manifest maps it to %q, want %q", f, byFeature[f], file)
		}
	}
	if len(byFeature) != len(want) {
		t.Errorf("manifest declares %d features, this test knows %d: update both together", len(byFeature), len(want))
	}
}
