package main

import (
	"context"
	"errors"
	"time"

	"SlothClashDesktop/internal/desync"
)

// Custom-domain calibration. One worker per session handles every enabled
// entry, one at a time, after the built-in services' initial search:
//
//  1. baseline: two rounds through plain DIRECT. Passing means the site is not
//     blocked: the group selects DIRECT (state not_needed).
//  2. search: the custom candidate set (cached winner first) in 30-second
//     budgets that resume where the previous one stopped; after the first
//     failed budget the group selects PASS, so traffic follows the profile
//     while the search continues. A winner passing two rounds is selected
//     (state ready). Each budget is one worker step: between two budgets the
//     worker serves whatever else is due first (the most overdue entry wins),
//     so a blocked site cannot hold back other entries' re-checks for more
//     than one budget.
//  3. nothing works: PASS stays selected (state unavailable) and the entry is
//     searched again after a doubling backoff.
//
// ready and not_needed entries are re-checked every 90 seconds through their
// group; a failure confirmed by two follow-ups starts again at step 1. Probes
// use desyncProbeReachable against https://<domain>/, or for a preset entry
// against each of its probe hosts (page and CDN), all of which must pass in
// every step: a strategy that opens the page but not its media is a failure.
// A session ends on a
// network, connection-generation, mode or settings change (the controller
// key), which re-checks every entry from step 1.

const (
	desyncCustomBuiltinWait = 2 * time.Minute
	desyncCustomRetry       = time.Minute
	desyncCustomBackoffMax  = 30 * time.Minute
)

// Variables so tests can shorten them.
var (
	desyncCustomCheckEvery = 90 * time.Second
	desyncCustomBudget     = 30 * time.Second
	// desyncCustomBudgetGap separates two search budgets of one entry.
	desyncCustomBudgetGap = time.Second
)

// desyncCustomPlan is the worker's per-entry state within one session.
type desyncCustomPlan struct {
	due time.Time
	// phase is the last outcome (queued, ready, not_needed, unavailable) or
	// searching: the baseline failed and the search is between budgets.
	phase     string
	candidate string
	failures  int // consecutive unavailable outcomes
	// Search progress (phase searching): candidates completed so far and
	// whether the group already selects PASS.
	tried   int
	passing bool
}

func (c *desyncController) setCustom(lease uint64, id string, state DesyncAutoService) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.epoch != lease {
		return
	}
	state.Updated = time.Now().Unix()
	if state.Route == "" {
		switch state.Phase {
		case "not_needed":
			state.Route = "direct"
		case "unavailable":
			state.Route = "profile"
		default:
			state.Route = "desync"
		}
	}
	if c.status.Custom == nil {
		c.status.Custom = map[string]DesyncAutoService{}
	}
	c.status.Custom[id] = state
}

// pruneCustom drops the state of entries that were removed or switched off.
func (c *desyncController) pruneCustom(lease uint64, keep map[string]bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.epoch != lease {
		return
	}
	for id := range c.status.Custom {
		if !keep[id] {
			delete(c.status.Custom, id)
		}
	}
}

// desyncBuiltinSearching reports whether an enabled built-in TCP service is
// still in its initial search.
func desyncBuiltinSearching(status DesyncAutoStatus, settings DesyncSettings) bool {
	busy := func(phase string) bool { return phase == "" || phase == "testing" }
	return (settings.YouTube && busy(status.YouTube.Phase)) || (settings.Discord && busy(status.Discord.Phase))
}

// enabledDesyncCustom returns the entries the worker serves now (the stored
// list is read on every pass, so adds/removes apply without a restart).
func enabledDesyncCustom() []DesyncCustomEntry {
	settings := currentDesyncSettings()
	if !settings.Enabled {
		return nil
	}
	var out []DesyncCustomEntry
	for _, e := range settings.Custom {
		if e.Enabled {
			out = append(out, e)
		}
	}
	return out
}

// desyncCustomTargets is the probe URLs of entry among the enabled entries:
// its domain's root, or the preset's probe hosts under the domains it routes
// now (desync.PresetOwnedDomains, the same split the config uses): a shared
// CDN another preset carries is that preset's to check.
func desyncCustomTargets(entry DesyncCustomEntry, enabled []DesyncCustomEntry) []string {
	if entry.Preset == "" {
		return []string{desyncCustomTarget(entry.Domain)}
	}
	var presets []string
	for _, e := range enabled {
		if e.Preset != "" {
			presets = append(presets, e.Preset)
		}
	}
	hosts := desync.PresetProbes(entry.Preset, desync.PresetOwnedDomains(entry.Preset, presets))
	if len(hosts) == 0 {
		// Unreachable with the catalog (its tests); never probe nothing,
		// which would pass every candidate.
		p, _ := desync.PresetByID(entry.Preset)
		hosts = p.Probes
	}
	var out []string
	for _, host := range hosts {
		out = append(out, desyncCustomTarget(host))
	}
	return out
}

func desyncCustomWanted(entry DesyncCustomEntry) bool {
	for _, e := range enabledDesyncCustom() {
		if e == entry {
			return true
		}
	}
	return false
}

func (a *App) runDesyncCustomService(ctx context.Context, lease uint64, network, endpoint, secret string, settings DesyncSettings) {
	for start := time.Now(); desyncBuiltinSearching(a.desyncCtl.snapshot(), settings) && time.Since(start) < desyncCustomBuiltinWait; {
		if !desyncWait(ctx, time.Second) {
			return
		}
	}
	plans := map[string]*desyncCustomPlan{}
	for ctx.Err() == nil {
		entries := enabledDesyncCustom()
		keep := map[string]bool{}
		for _, e := range entries {
			keep[e.ID] = true
		}
		for id := range plans {
			if !keep[id] {
				delete(plans, id)
			}
		}
		a.desyncCtl.pruneCustom(lease, keep)
		now := time.Now()
		entry, plan := nextDesyncCustom(entries, plans, now)
		if plan == nil {
			// Poll for new entries; wake earlier for the next due one (a
			// search between budgets).
			idle := 2 * time.Second
			for _, p := range plans {
				idle = min(idle, max(p.due.Sub(now), time.Millisecond))
			}
			if !desyncWait(ctx, idle) {
				return
			}
			continue
		}
		a.stepDesyncCustom(ctx, lease, network, endpoint, secret, entry, plan)
	}
}

// nextDesyncCustom picks the entry to serve: new entries first (in list
// order), then the most overdue one (list order on ties). A new entry gets a
// plan (state queued).
func nextDesyncCustom(entries []DesyncCustomEntry, plans map[string]*desyncCustomPlan, now time.Time) (DesyncCustomEntry, *desyncCustomPlan) {
	for _, e := range entries {
		if plans[e.ID] == nil {
			plans[e.ID] = &desyncCustomPlan{phase: "queued", due: now}
			return e, plans[e.ID]
		}
	}
	var pick DesyncCustomEntry
	var plan *desyncCustomPlan
	for _, e := range entries {
		if p := plans[e.ID]; !now.Before(p.due) && (plan == nil || p.due.Before(plan.due)) {
			pick, plan = e, p
		}
	}
	return pick, plan
}

func (a *App) stepDesyncCustom(ctx context.Context, lease uint64, network, endpoint, secret string, entry DesyncCustomEntry, plan *desyncCustomPlan) {
	route := desync.CustomRoute(entry.ID)
	targets := desyncCustomTargets(entry, enabledDesyncCustom())
	if plan.phase == "ready" || plan.phase == "not_needed" {
		check := func(one context.Context) error {
			for _, target := range targets {
				probe, cancel := context.WithTimeout(one, 6*time.Second)
				_, err := probeDesyncOutboundMode(probe, endpoint, secret, route, target, desyncProbeReachable)
				cancel()
				if err != nil {
					return err
				}
			}
			return nil
		}
		err := check(ctx)
		if err == nil {
			plan.due = time.Now().Add(desyncCustomCheckEvery)
			return
		}
		a.desyncCtl.setCustom(lease, entry.ID, DesyncAutoService{Phase: "checking", Candidate: plan.candidate, Code: desyncErrorCode(err)})
		if !confirmDesyncTCPFailure(ctx, check, func(c context.Context) bool { return desyncWait(c, 3*time.Second) }) {
			if ctx.Err() == nil {
				a.desyncCtl.setCustom(lease, entry.ID, DesyncAutoService{Phase: plan.phase, Candidate: plan.candidate})
			}
			plan.due = time.Now().Add(desyncCustomCheckEvery)
			return
		}
	}
	a.calibrateDesyncCustom(ctx, lease, network, endpoint, secret, entry, plan)
}

// calibrateDesyncCustom runs one bounded step of an entry's calibration: the
// baseline, or one search budget. An unfinished search leaves the plan in
// phase searching, due after desyncCustomBudgetGap.
func (a *App) calibrateDesyncCustom(ctx context.Context, lease uint64, network, endpoint, secret string, entry DesyncCustomEntry, plan *desyncCustomPlan) {
	route := desync.CustomRoute(entry.ID)
	service := desync.CustomService(entry.key())
	// Every target must pass, for the baseline and for a candidate
	// (chooseDesyncTCP).
	targets := desyncCustomTargets(entry, enabledDesyncCustom())
	retry := func(err error) {
		plan.phase, plan.candidate = "queued", ""
		plan.due = time.Now().Add(desyncCustomRetry)
		a.desyncCtl.setCustom(lease, entry.ID, DesyncAutoService{Phase: "queued", Code: desyncErrorCode(err)})
	}
	if len(targets) == 0 {
		retry(errors.New("no probe target"))
		return
	}
	if err := waitDesyncGroup(ctx, endpoint, secret, route); err != nil {
		if ctx.Err() == nil {
			retry(err)
		}
		return
	}
	candidates := desync.OrderedCustom(desyncFakeTTLSupported, desyncCachedSelection(network, service))
	total := len(candidates) + 1 // + the DIRECT baseline
	probe := func(one context.Context, c desync.Candidate, target string) error {
		name := c.Name(route)
		if c.ID == desync.DirectID {
			name = "DIRECT"
		}
		_, err := probeDesyncOutboundMode(one, endpoint, secret, name, target, desyncProbeReachable)
		return err
	}
	attempt := func(c desync.Candidate, e error, d time.Duration) {
		a.desyncAttempt(lease, DesyncAutoAttempt{Service: "custom", Candidate: c.ID, Target: entry.Domain, OK: e == nil, Code: desyncErrorCode(e), DurationMS: d.Milliseconds()})
	}
	selectMember := func(name string) bool {
		if err := a.applyDesyncCandidate(ctx, lease, endpoint, secret, route, name); err != nil {
			if ctx.Err() == nil {
				retry(err)
			}
			return false
		}
		return true
	}

	// 1. Baseline.
	if plan.phase != "searching" {
		a.desyncCtl.setCustom(lease, entry.ID, DesyncAutoService{Phase: "testing", Candidate: desync.DirectID, Attempt: 1, Total: total})
		baseline, cancel := context.WithTimeout(ctx, desyncCustomBudget)
		_, err := chooseDesyncTCP(baseline, targets, []desync.Candidate{{ID: desync.DirectID}}, probe, func(c desync.Candidate, _ int, e error, d time.Duration) { attempt(c, e, d) })
		cancel()
		if ctx.Err() != nil {
			return
		}
		if err == nil {
			if !selectMember("DIRECT") {
				return
			}
			saveDesyncSelection(network, service, desync.DirectID)
			*plan = desyncCustomPlan{phase: "not_needed", candidate: desync.DirectID, due: time.Now().Add(desyncCustomCheckEvery)}
			a.desyncCtl.setCustom(lease, entry.ID, DesyncAutoService{Phase: "not_needed", Candidate: desync.DirectID})
			return
		}
		plan.phase, plan.candidate, plan.tried, plan.passing = "searching", "", 0, false
		plan.due = time.Now()
		return
	}

	// 2. Search: one budget, resuming where the previous one stopped; a
	// candidate cut off by the budget deadline is not counted and is tried
	// again first.
	if !desyncCustomWanted(entry) {
		return // removed or switched off meanwhile; the next pass prunes it
	}
	startAt := a.desyncCtl.tcpStart(service, len(candidates))
	rotated := append(append([]desync.Candidate{}, candidates[startAt:]...), candidates[:startAt]...)
	trial, cancel := context.WithTimeout(ctx, desyncCustomBudget)
	winner, err := chooseDesyncTCP(trial, targets, rotated, probe, func(c desync.Candidate, i int, e error, d time.Duration) {
		if trial.Err() != nil {
			return
		}
		plan.tried++
		a.desyncCtl.advanceTCP(lease, service, startAt, i, len(candidates), e == nil)
		a.desyncCtl.setCustom(lease, entry.ID, DesyncAutoService{Phase: "testing", Candidate: c.ID, Attempt: 1 + plan.tried, Total: total, Code: desyncErrorCode(e)})
		attempt(c, e, d)
	})
	cancel()
	if ctx.Err() != nil {
		return
	}
	if err == nil {
		if !selectMember(winner.Name(route)) {
			return
		}
		saveDesyncSelection(network, service, winner.ID)
		a.desyncCtl.resetTCP(lease, service)
		*plan = desyncCustomPlan{phase: "ready", candidate: winner.ID, due: time.Now().Add(desyncCustomCheckEvery)}
		a.desyncCtl.setCustom(lease, entry.ID, DesyncAutoService{Phase: "ready", Candidate: winner.ID})
		return
	}
	if !plan.passing {
		// Do not leave the site on a failing candidate while searching.
		if !selectMember(desyncProfileSelection) {
			return
		}
		plan.passing = true
	}
	if plan.tried < len(candidates) {
		plan.due = time.Now().Add(desyncCustomBudgetGap)
		return
	}

	// 3. Nothing works: the profile's rules (PASS, selected above) carry the
	// site.
	failures := plan.failures + 1
	backoff := desyncCustomRetry << min(failures-1, 5)
	*plan = desyncCustomPlan{phase: "unavailable", candidate: "profile", failures: failures, due: time.Now().Add(min(backoff, desyncCustomBackoffMax))}
	a.desyncCtl.setCustom(lease, entry.ID, DesyncAutoService{Phase: "unavailable", Candidate: "profile", Code: "exhausted"})
	a.desyncAttempt(lease, DesyncAutoAttempt{Service: "custom", Candidate: "profile", Target: entry.Domain, OK: true, Code: "unavailable"})
}
