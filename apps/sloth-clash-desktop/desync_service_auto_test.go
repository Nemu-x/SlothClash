package main

import (
	"context"
	"encoding/hex"
	"reflect"
	"strings"
	"testing"
	"time"

	"SlothClashDesktop/internal/desync"
)

var serviceEntry = DesyncCustomEntry{ID: "fb01fb01", Service: "facebook", Enabled: true}

const fbPage, fbStatic, fbMedia = "https://www.facebook.com/", "https://static.xx.fbcdn.net/", "https://scontent.xx.fbcdn.net/"

// probedTargets is the distinct targets of a probe log, in first-seen order.
func probedTargets(probes []string) []string {
	var out []string
	seen := map[string]bool{}
	for _, p := range probes {
		target := strings.Fields(p)[1]
		if !seen[target] {
			seen[target] = true
			out = append(out, target)
		}
	}
	return out
}

// The owner's failure: the page opens through plain DIRECT and through every
// strategy, the media CDN only through one. Neither "not needed" nor the
// first page-only strategy may be accepted.
func TestDesyncServiceNeedsPageAndCDN(t *testing.T) {
	core := newCustomFakeCore(t, func(name, target string) bool {
		return target != fbMedia || strings.HasSuffix(name, "-"+presetIDOf("split-1s"))
	})
	a, network := customTestApp(t, core, serviceEntry)
	plan := &desyncCustomPlan{phase: "queued"}
	calibrateCustomToEnd(a, 1, network, a.core.listen, "", serviceEntry, plan)
	route := desync.CustomRoute(serviceEntry.ID)
	if got := core.selections(route); got[len(got)-1] != route+"-"+presetIDOf("split-1s") {
		t.Fatalf("selections: %v", got)
	}
	state := a.GetDesyncAutoStatus().Custom[serviceEntry.ID]
	if state.Phase != "ready" || state.Candidate != presetIDOf("split-1s") {
		t.Fatalf("state %+v", state)
	}
	if got := probedTargets(core.probeLog()); !reflect.DeepEqual(got, []string{fbPage, fbStatic, fbMedia}) {
		t.Fatalf("targets: %v", got)
	}
	// Cached under the preset key, not under a domain.
	if desyncCachedSelection(network, "custom:preset:facebook") != presetIDOf("split-1s") || desyncCachedSelection(network, desync.CustomService("")) != "" {
		t.Fatalf("cache: %+v", currentDesktopPrefs().DesyncCache)
	}
}

func TestDesyncServicePageOnlyIsUnavailable(t *testing.T) {
	core := newCustomFakeCore(t, func(_, target string) bool { return target != fbMedia })
	a, network := customTestApp(t, core, serviceEntry)
	plan := &desyncCustomPlan{phase: "queued"}
	calibrateCustomToEnd(a, 1, network, a.core.listen, "", serviceEntry, plan)
	if state := a.GetDesyncAutoStatus().Custom[serviceEntry.ID]; state.Phase != "unavailable" || plan.phase != "unavailable" {
		t.Fatalf("state %+v plan %+v", state, plan)
	}
	if got := desyncCachedSelection(network, "custom:preset:facebook"); got != "" {
		t.Fatalf("page-only strategy cached: %q", got)
	}
}

// A working preset whose CDN starts failing is searched again, even though
// the page still opens.
func TestDesyncServiceRecheckCoversCDN(t *testing.T) {
	core := newCustomFakeCore(t, func(_, target string) bool { return target != fbMedia })
	a, network := customTestApp(t, core, serviceEntry)
	plan := &desyncCustomPlan{phase: "not_needed", candidate: desync.DirectID}
	a.stepDesyncCustom(context.Background(), 1, network, a.core.listen, "", serviceEntry, plan)
	if plan.phase == "not_needed" {
		t.Fatalf("CDN failure ignored by the re-check: %+v", plan)
	}
	route := desync.CustomRoute(serviceEntry.ID)
	var recheckMedia bool
	for _, p := range core.probeLog() {
		recheckMedia = recheckMedia || strings.HasPrefix(p, route+" "+fbMedia)
	}
	if !recheckMedia {
		t.Fatalf("re-check did not probe the CDN through the group: %v", core.probeLog())
	}
}

// Instagram probes the shared fbcdn.net only while it routes it.
func TestDesyncServiceTargetsFollowOwnership(t *testing.T) {
	insta := DesyncCustomEntry{ID: "ig01ig01", Service: "instagram", Enabled: true}
	alone := desyncCustomTargets(insta, []DesyncCustomEntry{insta})
	both := desyncCustomTargets(insta, []DesyncCustomEntry{insta, serviceEntry})
	if !reflect.DeepEqual(alone, []string{"https://www.instagram.com/", "https://static.cdninstagram.com/", "https://scontent.cdninstagram.com/", fbMedia}) {
		t.Fatalf("alone: %v", alone)
	}
	if !reflect.DeepEqual(both, alone[:3]) {
		t.Fatalf("with facebook: %v", both)
	}
	if got := desyncCustomTargets(customEntry, nil); !reflect.DeepEqual(got, []string{"https://example.org/"}) {
		t.Fatalf("plain: %v", got)
	}
}

// One calibration step gets desyncCustomBudget per probed host; a plain
// entry (one host) keeps exactly the base budget.
func TestDesyncCustomStepBudgetScalesWithTargets(t *testing.T) {
	for targets, want := range map[int]time.Duration{0: desyncCustomBudget, 1: desyncCustomBudget, 3: 3 * desyncCustomBudget, 4: 4 * desyncCustomBudget} {
		if got := desyncCustomStepBudget(targets); got != want {
			t.Fatalf("budget(%d) = %v, want %v", targets, got, want)
		}
	}
}

// On a slow network the passing candidate of a preset (2 rounds x page and
// CDN hosts) takes longer than one plain-entry budget. The step budget scales
// with the hosts, so it completes instead of being cut off at the deadline
// and retried forever.
func TestDesyncServiceSlowPassingCandidateFitsTheBudget(t *testing.T) {
	const perProbe = 30 * time.Millisecond
	// A plain budget fits fewer than the 6 probes of the winner (3 hosts x
	// 2 rounds); three of them fit it with room to spare.
	withShortCustomTimers(t, 4*perProbe, time.Hour, time.Millisecond)
	core := newCustomFakeCore(t, func(name, _ string) bool { return strings.HasSuffix(name, "-"+presetIDOf("split-1s")) })
	core.delay.Store(int64(perProbe))
	a, network := customTestApp(t, core, serviceEntry)
	if n := len(desyncCustomTargets(serviceEntry, enabledDesyncCustom())); n != 3 {
		t.Fatalf("facebook probes %d hosts, the test assumes 3", n)
	}
	plan := &desyncCustomPlan{phase: "queued"}
	for range 30 {
		a.calibrateDesyncCustom(context.Background(), 1, network, a.core.listen, "", serviceEntry, plan)
		if plan.phase != "searching" {
			break
		}
	}
	state := a.GetDesyncAutoStatus().Custom[serviceEntry.ID]
	if plan.phase != "ready" || state.Phase != "ready" || state.Candidate != presetIDOf("split-1s") {
		t.Fatalf("plan %+v state %+v: the passing candidate never completed within a step", plan, state)
	}
}

func TestDesyncReportNamesPresetsAndHashesUserDomains(t *testing.T) {
	core := newCustomFakeCore(t, func(name, _ string) bool { return name == "DIRECT" })
	a, network := customTestApp(t, core, customEntry, serviceEntry)
	secret := strings.Repeat("ab", 32)
	withDesyncReportKey(t, secret)
	calibrateCustomToEnd(a, 1, network, a.core.listen, "", customEntry, &desyncCustomPlan{})
	calibrateCustomToEnd(a, 1, network, a.core.listen, "", serviceEntry, &desyncCustomPlan{})
	a.state.Connection.Status = "disconnected"
	text := a.GetDesyncDiagnosticReport()
	if strings.Contains(text, "example.org") {
		t.Fatalf("user domain leaked into the report: %s", text)
	}
	key, _ := hex.DecodeString(secret)
	report := parseDesyncReport(t, text)
	want := []map[string]any{
		{"id": customEntry.ID, "domain": desyncReportDigest(key, "example.org"), "enabled": true},
		{"id": serviceEntry.ID, "preset": "facebook", "enabled": true},
	}
	if !reflect.DeepEqual(report.Custom, want) {
		t.Fatalf("custom entries: %v", report.Custom)
	}
	presetAttempts := 0
	for _, attempt := range report.Automatic.Attempts {
		if attempt.Preset == "facebook" {
			presetAttempts++
			if attempt.Target != "" || attempt.Service != "custom" {
				t.Fatalf("preset attempt: %+v", attempt)
			}
		} else if attempt.Service == "custom" && attempt.Target != desyncReportDigest(key, "example.org") {
			t.Fatalf("plain attempt: %+v", attempt)
		}
	}
	if presetAttempts == 0 || report.Automatic.Custom[serviceEntry.ID].Phase != "not_needed" {
		t.Fatalf("preset attempts missing: %+v", report.Automatic)
	}
}
