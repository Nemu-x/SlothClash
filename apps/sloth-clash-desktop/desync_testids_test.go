package main

import (
	"github.com/Nemu-x/desync/presets"

	"SlothClashDesktop/internal/desync"
)

// presetIDOf is the catalog ID of the preset called name.
func presetIDOf(name string) string {
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
	panic("no preset " + name)
}

// candID is the ID of the i-th candidate of service with the test
// capabilities (TestMain pins desyncCaps to Windows').
func candID(service string, i int) string { return desync.Candidates(service, desyncCaps)[i].ID }
