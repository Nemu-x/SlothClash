package main

import "SlothClashDesktop/internal/desync"

// DesyncStrategy describes one strategy of the catalog for the Desync window.
type DesyncStrategy struct {
	ID       string `json:"id"`
	Name     string `json:"name"`
	Protocol string `json:"protocol"`
	// Kind is a fixed vocabulary the UI translates ("tlsrec", "split",
	// "fake", ...: desync.Kind*); Variant numbers the presets of one kind
	// from 1 in catalog order.
	Kind        string `json:"kind"`
	Variant     int    `json:"variant"`
	Description string `json:"description"`
	Available   bool   `json:"available"`
	Reason      string `json:"reason,omitempty"`
	Tried       bool   `json:"tried"`
}

// DesyncCapability is one method and whether this build implements it here.
type DesyncCapability struct {
	Method    string `json:"method"`
	Supported bool   `json:"supported"`
	Reason    string `json:"reason,omitempty"`
	Inferred  bool   `json:"inferred,omitempty"`
}

// DesyncCapabilityReport is what this build of the app can do on this
// computer: the methods the library implements on this OS and the catalog's
// strategies with whether calibration tries them. It is read from the build,
// not measured: it does not say which strategy works on the current network.
type DesyncCapabilityReport struct {
	OS         string             `json:"os"`
	Methods    []DesyncCapability `json:"methods"`
	Strategies []DesyncStrategy   `json:"strategies"`
}

func desyncCapabilityReport() DesyncCapabilityReport {
	report := DesyncCapabilityReport{OS: desyncCaps.OS, Methods: []DesyncCapability{}, Strategies: []DesyncStrategy{}}
	for _, c := range desync.Capabilities(desyncCaps) {
		report.Methods = append(report.Methods, DesyncCapability(c))
	}
	for _, s := range desync.Strategies(desyncCaps) {
		report.Strategies = append(report.Strategies, DesyncStrategy(s))
	}
	return report
}

// GetDesyncCapabilities reports the methods and strategies of this build on
// this OS (see DesyncCapabilityReport). A strategy chosen by calibration
// appears in DesyncAutoService.Candidate by its ID.
func (a *App) GetDesyncCapabilities() DesyncCapabilityReport {
	return desyncCapabilityReport()
}
