package runtimecfg

import (
	"testing"

	"github.com/Nemu-x/desync/presets"
)

// presetID is the catalog ID of the preset called name.
func presetID(t *testing.T, name string) string {
	t.Helper()
	for _, p := range presets.TCP() {
		if p.Name == name {
			return p.ID()
		}
	}
	for _, p := range presets.UDP() {
		if p.Name == name {
			return p.ID()
		}
	}
	t.Fatalf("no preset %s", name)
	return ""
}
