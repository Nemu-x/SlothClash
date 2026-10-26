package main

import (
	"context"
	"reflect"
	"strings"
	"testing"

	"SlothClashDesktop/internal/desync"
)

var presetEntry = DesyncCustomEntry{ID: "fb01fb01", Preset: "facebook", Enabled: true}

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
func TestDesyncPresetNeedsPageAndCDN(t *testing.T) {
	core := newCustomFakeCore(t, func(name, target string) bool {
		return target != fbMedia || strings.HasSuffix(name, "-split")
	})
	a, network := customTestApp(t, core, presetEntry)
	plan := &desyncCustomPlan{phase: "queued"}
	calibrateCustomToEnd(a, 1, network, a.core.listen, "", presetEntry, plan)
	route := desync.CustomRoute(presetEntry.ID)
	if got := core.selections(route); got[len(got)-1] != route+"-split" {
		t.Fatalf("selections: %v", got)
	}
	state := a.GetDesyncAutoStatus().Custom[presetEntry.ID]
	if state.Phase != "ready" || state.Candidate != "split" {
		t.Fatalf("state %+v", state)
	}
	if got := probedTargets(core.probeLog()); !reflect.DeepEqual(got, []string{fbPage, fbStatic, fbMedia}) {
		t.Fatalf("targets: %v", got)
	}
	// Cached under the preset key, not under a domain.
	if desyncCachedSelection(network, "custom:preset:facebook") != "split" || desyncCachedSelection(network, desync.CustomService("")) != "" {
		t.Fatalf("cache: %+v", currentDesktopPrefs().DesyncCache)
	}
}

func TestDesyncPresetPageOnlyIsUnavailable(t *testing.T) {
	core := newCustomFakeCore(t, func(_, target string) bool { return target != fbMedia })
	a, network := customTestApp(t, core, presetEntry)
	plan := &desyncCustomPlan{phase: "queued"}
	calibrateCustomToEnd(a, 1, network, a.core.listen, "", presetEntry, plan)
	if state := a.GetDesyncAutoStatus().Custom[presetEntry.ID]; state.Phase != "unavailable" || plan.phase != "unavailable" {
		t.Fatalf("state %+v plan %+v", state, plan)
	}
	if got := desyncCachedSelection(network, "custom:preset:facebook"); got != "" {
		t.Fatalf("page-only strategy cached: %q", got)
	}
}

// A working preset whose CDN starts failing is searched again, even though
// the page still opens.
func TestDesyncPresetRecheckCoversCDN(t *testing.T) {
	core := newCustomFakeCore(t, func(_, target string) bool { return target != fbMedia })
	a, network := customTestApp(t, core, presetEntry)
	plan := &desyncCustomPlan{phase: "not_needed", candidate: desync.DirectID}
	a.stepDesyncCustom(context.Background(), 1, network, a.core.listen, "", presetEntry, plan)
	if plan.phase == "not_needed" {
		t.Fatalf("CDN failure ignored by the re-check: %+v", plan)
	}
	route := desync.CustomRoute(presetEntry.ID)
	var recheckMedia bool
	for _, p := range core.probeLog() {
		recheckMedia = recheckMedia || strings.HasPrefix(p, route+" "+fbMedia)
	}
	if !recheckMedia {
		t.Fatalf("re-check did not probe the CDN through the group: %v", core.probeLog())
	}
}

// Instagram probes the shared fbcdn.net only while it routes it.
func TestDesyncPresetTargetsFollowOwnership(t *testing.T) {
	insta := DesyncCustomEntry{ID: "ig01ig01", Preset: "instagram", Enabled: true}
	alone := desyncCustomTargets(insta, []DesyncCustomEntry{insta})
	both := desyncCustomTargets(insta, []DesyncCustomEntry{insta, presetEntry})
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
