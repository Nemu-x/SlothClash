package main

import (
	"context"
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
//     (state ready).
//  3. nothing works: PASS stays selected (state unavailable) and the entry is
//     searched again after a doubling backoff.
//
// ready and not_needed entries are re-checked every 90 seconds through their
// group; a failure confirmed by two follow-ups starts again at step 1. Probes
// use desyncProbeReachable against https://<domain>/. A session ends on a
// network, connection-generation, mode or settings change (the controller
// key), which re-checks every entry from step 1.

const (
	desyncCustomCheckEvery  = 90 * time.Second
	desyncCustomBuiltinWait = 2 * time.Minute
	desyncCustomRetry       = time.Minute
	desyncCustomBackoffMax  = 30 * time.Minute
	desyncCustomBudget      = 30 * time.Second
)

// desyncCustomPlan is the worker's per-entry state within one session.
type desyncCustomPlan struct {
	due       time.Time
	phase     string // last outcome: queued, ready, not_needed, unavailable
	candidate string
	failures  int // consecutive unavailable outcomes
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

func desyncCustomWanted(entry DesyncCustomEntry) bool {
	for _, e := range enabledDesyncCustom() {
		if e.ID == entry.ID && e.Domain == entry.Domain {
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
		entry, plan := nextDesyncCustom(entries, plans, time.Now())
		if plan == nil {
			if !desyncWait(ctx, 2*time.Second) {
				return
			}
			continue
		}
		a.stepDesyncCustom(ctx, lease, network, endpoint, secret, entry, plan)
	}
}

// nextDesyncCustom picks the entry to serve: new entries first (in list
// order), then the first due one. A new entry gets a plan (state queued).
func nextDesyncCustom(entries []DesyncCustomEntry, plans map[string]*desyncCustomPlan, now time.Time) (DesyncCustomEntry, *desyncCustomPlan) {
	for _, e := range entries {
		if plans[e.ID] == nil {
			plans[e.ID] = &desyncCustomPlan{phase: "queued", due: now}
			return e, plans[e.ID]
		}
	}
	for _, e := range entries {
		if p := plans[e.ID]; !now.Before(p.due) {
			return e, p
		}
	}
	return DesyncCustomEntry{}, nil
}

func (a *App) stepDesyncCustom(ctx context.Context, lease uint64, network, endpoint, secret string, entry DesyncCustomEntry, plan *desyncCustomPlan) {
	route := desync.CustomRoute(entry.ID)
	target := desyncCustomTarget(entry.Domain)
	if plan.phase == "ready" || plan.phase == "not_needed" {
		check := func(one context.Context) error {
			probe, cancel := context.WithTimeout(one, 6*time.Second)
			defer cancel()
			_, err := probeDesyncOutboundMode(probe, endpoint, secret, route, target, desyncProbeReachable)
			return err
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

func (a *App) calibrateDesyncCustom(ctx context.Context, lease uint64, network, endpoint, secret string, entry DesyncCustomEntry, plan *desyncCustomPlan) {
	route := desync.CustomRoute(entry.ID)
	service := desync.CustomService(entry.Domain)
	targets := []string{desyncCustomTarget(entry.Domain)}
	retry := func(err error) {
		plan.phase, plan.candidate = "queued", ""
		plan.due = time.Now().Add(desyncCustomRetry)
		a.desyncCtl.setCustom(lease, entry.ID, DesyncAutoService{Phase: "queued", Code: desyncErrorCode(err)})
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

	// 2. Search, resuming across budgets; a candidate cut off by the budget
	// deadline is not counted and is tried again first.
	tried, passing := 0, false
	for tried < len(candidates) {
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
			tried++
			a.desyncCtl.advanceTCP(lease, service, startAt, i, len(candidates), e == nil)
			a.desyncCtl.setCustom(lease, entry.ID, DesyncAutoService{Phase: "testing", Candidate: c.ID, Attempt: 1 + tried, Total: total, Code: desyncErrorCode(e)})
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
		if !passing {
			// Do not leave the site on a failing candidate while searching.
			if !selectMember(desyncProfileSelection) {
				return
			}
			passing = true
		}
		if !desyncWait(ctx, time.Second) {
			return
		}
	}

	// 3. Nothing works: the profile's rules carry the site.
	if !passing && !selectMember(desyncProfileSelection) {
		return
	}
	failures := plan.failures + 1
	backoff := desyncCustomRetry << min(failures-1, 5)
	*plan = desyncCustomPlan{phase: "unavailable", candidate: "profile", failures: failures, due: time.Now().Add(min(backoff, desyncCustomBackoffMax))}
	a.desyncCtl.setCustom(lease, entry.ID, DesyncAutoService{Phase: "unavailable", Candidate: "profile", Code: "exhausted"})
	a.desyncAttempt(lease, DesyncAutoAttempt{Service: "custom", Candidate: "profile", Target: entry.Domain, OK: true, Code: "unavailable"})
}
