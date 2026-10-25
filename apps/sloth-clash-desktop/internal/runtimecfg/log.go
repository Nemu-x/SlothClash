// Package runtimecfg generates the mihomo runtime config.yaml for a profile:
// it fetches (or reads the cached) subscription body, parses it, applies the
// profile's merge templates, the app's runtime overlay and the profile's
// JavaScript override, validates the result and writes it.
//
// Everything it reads from the running app arrives in Input.Env, captured once
// by the caller; the package never reaches back into package main.
package runtimecfg

import "time"

// Logging sinks. The generation logs through these; main points them at the
// app's debug JSON log at init (SetLogSinks). Unset — as in this package's own
// tests — they drop everything.
var (
	debugLogSink func(runID, hypothesisID, location, message string, data map[string]any)
	traceSink    func(stage, outcome string, dur time.Duration, fields map[string]any)
)

// SetLogSinks routes the package's debug lines and pipeline trace events. Call
// it once, before any generation runs (main does it from init).
func SetLogSinks(
	debugLog func(runID, hypothesisID, location, message string, data map[string]any),
	trace func(stage, outcome string, dur time.Duration, fields map[string]any),
) {
	debugLogSink = debugLog
	traceSink = trace
}

func debugLog(runID, hypothesisID, location, message string, data map[string]any) {
	if debugLogSink != nil {
		debugLogSink(runID, hypothesisID, location, message, data)
	}
}

func traceEvent(stage, outcome string, dur time.Duration, fields map[string]any) {
	if traceSink != nil {
		traceSink(stage, outcome, dur, fields)
	}
}
