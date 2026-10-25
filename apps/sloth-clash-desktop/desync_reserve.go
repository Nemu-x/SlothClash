package main

import (
	"context"
	"net/http"
	"net/url"
	"time"

	"SlothClashDesktop/internal/desync"
)

const desyncProfileSelection = "PASS"

type desyncProfileProbe struct {
	OK         bool   `json:"ok"`
	Route      string `json:"route"`
	Code       string `json:"code"`
	DurationMS int64  `json:"durationMS"`
	desyncUDPProbe
}

// Recovery requires separated successes of the same candidate and a minimum
// dwell. An intervening failure or another candidate starts a new sequence.
type desyncReserveRecovery struct {
	since, last time.Time
	candidate   string
	passed      int
}

func (r *desyncReserveRecovery) observe(now time.Time, candidate string, ok bool) bool {
	if !r.last.IsZero() && now.Sub(r.last) > 90*time.Second {
		r.passed = 0
		r.last = time.Time{}
	}
	if !ok || candidate == "" {
		r.candidate = ""
		r.passed = 0
		r.last = time.Time{}
		return false
	}
	if r.candidate != candidate {
		r.candidate = candidate
		r.passed = 0
		r.last = time.Time{}
	}
	if r.last.IsZero() || now.Sub(r.last) >= 20*time.Second {
		r.passed++
		r.last = now
	}
	return r.ready(now)
}

func (r *desyncReserveRecovery) ready(now time.Time) bool {
	return r.passed >= 3 && now.Sub(r.since) >= 3*time.Minute && !r.last.IsZero() && now.Sub(r.last) <= 90*time.Second
}

func desyncWait(ctx context.Context, delay time.Duration) bool {
	timer := time.NewTimer(delay)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return false
	case <-timer.C:
		return true
	}
}

func probeDesyncProfile(ctx context.Context, endpoint, secret, route, target string) (desyncProfileProbe, error) {
	var result desyncProfileProbe
	err := autoCoreJSON(ctx, endpoint, secret, http.MethodPost, "/proxies/"+url.PathEscape(route)+"/desync/profile-probe", map[string]string{"target": target}, &result)
	if err == nil && (!result.OK || result.Route != "remote") {
		code := result.Code
		switch code {
		case "reserve_direct", "reserve_blocked", "reserve_no_udp", "reserve_unavailable", "profile_unavailable", "endpoint_changed", "busy", "reserve_socket", "reserve_udp_loss", "reserve_https":
		default:
			code = "reserve_unavailable"
		}
		err = &desyncControllerError{Status: 503, Code: code}
	}
	return result, err
}

func (a *App) confirmDesyncProfile(ctx context.Context, lease uint64, endpoint, secret, service, route, target string) error {
	rounds := 2
	if service == "voice" {
		rounds = 1
	} // twelve independently validated UDP replies
	for i := 0; i < rounds; i++ {
		p, err := probeDesyncProfile(ctx, endpoint, secret, route, target)
		a.desyncAttempt(lease, DesyncAutoAttempt{Service: service, Candidate: "profile", Target: target, OK: err == nil, Code: desyncErrorCode(err), DurationMS: p.DurationMS, Sent: p.Sent, Received: p.Received})
		if err != nil {
			return err
		}
		if i+1 < rounds && !desyncWait(ctx, time.Second) {
			return ctx.Err()
		}
	}
	return ctx.Err()
}

func desyncTCPCheck(ctx context.Context, endpoint, secret, service, proxy string) error {
	for _, target := range desyncTargets(service) {
		one, cancel := context.WithTimeout(ctx, 6*time.Second)
		_, err := probeDesyncOutbound(one, endpoint, secret, proxy, target)
		cancel()
		if err != nil {
			return err
		}
	}
	return nil
}

// One failed scheduled check never changes a route. Follow-up checks use fresh
// connections, are separated in time and remain cancellation bounded.
func confirmDesyncTCPFailure(ctx context.Context, check func(context.Context) error, pause func(context.Context) bool) bool {
	for i := 0; i < 2; i++ {
		if !pause(ctx) || ctx.Err() != nil || check(ctx) == nil {
			return false
		}
	}
	return ctx.Err() == nil
}

func (a *App) runDesyncTCPService(ctx context.Context, lease uint64, network, endpoint, secret, service, route string) {
	var group desyncUDPProxy
	for {
		err := waitDesyncGroup(ctx, endpoint, secret, route)
		if err == nil {
			err = autoCoreJSON(ctx, endpoint, secret, http.MethodGet, "/proxies/"+route, nil, &group)
		}
		if err == nil {
			break
		}
		a.desyncService(lease, service, DesyncAutoService{Phase: "failed", Code: desyncErrorCode(err)})
		if !desyncWait(ctx, time.Minute) {
			return
		}
	}
	reserve := group.Now == desyncProfileSelection
	recovery := desyncReserveRecovery{since: time.Now()}
	if reserve {
		a.desyncService(lease, service, DesyncAutoService{Phase: "profile", Candidate: "profile"})
	}
	for ctx.Err() == nil {
		if !reserve {
			err := a.calibrateDesyncTCP(ctx, lease, network, endpoint, secret, service, route)
			if ctx.Err() != nil {
				return
			}
			if err != nil {
				if fallbackErr := a.confirmDesyncProfile(ctx, lease, endpoint, secret, service, route, ""); fallbackErr == nil {
					if a.applyDesyncCandidate(ctx, lease, endpoint, secret, route, desyncProfileSelection) == nil {
						reserve = true
						recovery = desyncReserveRecovery{since: time.Now()}
						a.desyncService(lease, service, DesyncAutoService{Phase: "profile", Candidate: "profile"})
						a.desyncAttempt(lease, DesyncAutoAttempt{Service: service, Candidate: "profile", OK: true, Code: "fallback_applied"})
					}
				} else {
					a.desyncService(lease, service, DesyncAutoService{Phase: "failed", Code: desyncErrorCode(fallbackErr)})
				}
				if !reserve {
					if !desyncWait(ctx, time.Minute) {
						return
					}
					continue
				}
			} else {
				for desyncWait(ctx, 90*time.Second) {
					check := func(one context.Context) error { return desyncTCPCheck(one, endpoint, secret, service, route) }
					err = check(ctx)
					if err == nil {
						continue
					}
					a.desyncService(lease, service, DesyncAutoService{Phase: "checking", Code: desyncErrorCode(err)})
					if confirmDesyncTCPFailure(ctx, check, func(c context.Context) bool { return desyncWait(c, 3*time.Second) }) {
						break
					}
					if ctx.Err() != nil {
						return
					}
					a.desyncService(lease, service, DesyncAutoService{Phase: "ready"})
				}
				continue
			}
		}
		// Prospective candidate probes do not disturb the working profile route.
		if !desyncWait(ctx, 45*time.Second) {
			return
		}
		winner, err := a.findDesyncTCP(ctx, lease, network, endpoint, secret, service, route, true)
		if ctx.Err() != nil {
			return
		}
		if recovery.observe(time.Now(), winner.ID, err == nil) {
			if a.applyDesyncCandidate(ctx, lease, endpoint, secret, route, winner.Name(route)) == nil {
				saveDesyncSelection(network, service, winner.ID)
				reserve = false
				a.desyncAttempt(lease, DesyncAutoAttempt{Service: service, Candidate: winner.ID, OK: true, Code: "desync_restored"})
				continue
			}
		}
		p, e := probeDesyncProfile(ctx, endpoint, secret, route, "")
		phase := "profile"
		if e != nil {
			phase = "profile_failed"
		}
		a.desyncService(lease, service, DesyncAutoService{Phase: phase, Candidate: "profile", Code: desyncErrorCode(e)})
		a.desyncAttempt(lease, DesyncAutoAttempt{Service: service, Candidate: "profile", OK: e == nil, Code: desyncErrorCode(e), DurationMS: p.DurationMS})
	}
}

// Recheck all relevant owned peers immediately before committing the reserve.
// A recovered peer, a new endpoint or missing telemetry prevents a mutation.
func desyncAllPeersStillStalled(now time.Time, connections []ConnectionItem, activity []desyncUDPActivity) bool {
	active := map[string]bool{}
	for _, c := range connections {
		if target := desyncVoiceTarget([]ConnectionItem{c}); target != "" {
			active[target] = true
		}
	}
	if len(active) == 0 {
		return false
	}
	for _, p := range activity {
		if !active[p.Endpoint] {
			continue
		}
		if now.UnixMilli()-p.FirstMS < 22000 || now.UnixMilli()-p.LastRXMS < 20000 || now.UnixMilli()-p.LastTXMS > 10000 {
			return false
		}
		delete(active, p.Endpoint)
	}
	return len(active) == 0
}

func (a *App) tryDesyncVoiceReserve(ctx context.Context, lease uint64, endpoint, secret, target, selected string) bool {
	ctx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	var overview struct {
		Connections []ConnectionItem `json:"connections"`
	}
	if autoCoreJSON(ctx, endpoint, secret, http.MethodGet, "/connections", nil, &overview) != nil {
		return false
	}
	verified := map[string]bool{}
	for _, c := range overview.Connections {
		if peer := desyncVoiceTarget([]ConnectionItem{c}); peer != "" {
			verified[peer] = true
		}
	}
	if !verified[target] || len(verified) > 8 {
		return false
	}
	for peer := range verified {
		if a.confirmDesyncProfile(ctx, lease, endpoint, secret, "voice", desync.DiscordUDPRoute, peer) != nil {
			return false
		}
	}
	if autoCoreJSON(ctx, endpoint, secret, http.MethodGet, "/connections", nil, &overview) != nil {
		return false
	}
	for _, c := range overview.Connections {
		if peer := desyncVoiceTarget([]ConnectionItem{c}); peer != "" && !verified[peer] {
			return false
		}
	}
	activity, err := readDesyncVoiceActivity(ctx, endpoint, secret, selected, overview.Connections)
	if err != nil || !desyncAllPeersStillStalled(time.Now(), overview.Connections, activity) {
		return false
	}
	if a.applyDesyncCandidate(ctx, lease, endpoint, secret, desync.DiscordUDPRoute, desyncProfileSelection) != nil {
		return false
	}
	a.desyncAttempt(lease, DesyncAutoAttempt{Service: "voice", Candidate: "profile", OK: true, Code: "fallback_applied"})
	for _, c := range overview.Connections {
		if peer := desyncVoiceTarget([]ConnectionItem{c}); peer != "" {
			closeStaleDesyncVoice(ctx, endpoint, secret, peer, desyncProfileSelection, []ConnectionItem{c})
		}
	}
	return true
}

func desyncProfileVoiceTarget(connections []ConnectionItem) (target string, active bool) {
	for _, c := range connections {
		if !isDiscordUDPConnection(c) || c.Metadata.DestinationPort == "443" {
			continue
		}
		// Eligibility, public endpoint and high-port checks stay identical to the
		// Desync monitor; only the synthetic ownership marker differs here.
		copy := c
		copy.Chains = []string{desync.DiscordUDPRoute}
		if peer := desyncVoiceTarget([]ConnectionItem{copy}); peer != "" {
			target = peer
			active = true
		}
	}
	return
}

// Active reserve calls stay on their current sockets. Positive synthetic
// discovery only permits restoring the selector after an idle boundary; it
// never causes a live call/stream to be closed or replayed.
func (a *App) runDesyncVoiceReserve(ctx context.Context, lease uint64, network, endpoint, secret string) {
	recovery := desyncReserveRecovery{since: time.Now()}
	lastProgress := time.Now()
	previous := map[string]int64{}
	nextProbe := time.Now()
	candidates := desync.Ordered("voice", desyncCachedSelection(network, "voice"))
	nextCandidate := 0
	for ctx.Err() == nil {
		var overview struct {
			Connections []ConnectionItem `json:"connections"`
		}
		var group desyncUDPProxy
		err := autoCoreJSON(ctx, endpoint, secret, http.MethodGet, "/connections", nil, &overview)
		if err == nil {
			err = autoCoreJSON(ctx, endpoint, secret, http.MethodGet, "/proxies/"+desync.DiscordUDPRoute, nil, &group)
		}
		if err != nil {
			lastProgress = time.Now() // an observation gap never establishes idleness
			a.desyncService(lease, "voice", DesyncAutoService{Phase: "profile_failed", Candidate: "profile", Code: desyncErrorCode(err)})
			if !desyncWait(ctx, 4*time.Second) {
				return
			}
			continue
		}
		if group.Now != desyncProfileSelection {
			return
		}
		target, _ := desyncProfileVoiceTarget(overview.Connections)
		current := map[string]int64{}
		for _, c := range overview.Connections {
			if _, active := desyncProfileVoiceTarget([]ConnectionItem{c}); !active {
				continue
			}
			current[c.ID] = c.Upload + c.Download
			if total, ok := previous[c.ID]; !ok || total != current[c.ID] {
				lastProgress = time.Now()
			}
		}
		previous = current
		candidate := candidates[nextCandidate]
		if target != "" && time.Now().After(nextProbe) {
			var result desyncUDPProbe
			start := time.Now()
			err = autoCoreJSON(ctx, endpoint, secret, http.MethodPost, "/proxies/"+candidate.Name(desync.DiscordUDPRoute)+"/desync/udp-probe", map[string]string{"target": target}, &result)
			passed := err == nil && result.passed()
			code := desyncErrorCode(err)
			if err == nil && !passed {
				code = "udp_loss"
			}
			a.desyncAttempt(lease, DesyncAutoAttempt{Service: "voice", Candidate: candidate.ID, Target: target, OK: passed, Code: code, DurationMS: time.Since(start).Milliseconds(), Sent: result.Sent, Received: result.Received})
			recovery.observe(time.Now(), candidate.ID, passed)
			if !passed {
				nextCandidate = (nextCandidate + 1) % len(candidates)
			}
			nextProbe = time.Now().Add(time.Minute)
		}
		if recovery.ready(time.Now()) && time.Since(lastProgress) >= 60*time.Second {
			if a.applyDesyncCandidate(ctx, lease, endpoint, secret, desync.DiscordUDPRoute, candidate.Name(desync.DiscordUDPRoute)) == nil {
				a.desyncAttempt(lease, DesyncAutoAttempt{Service: "voice", Candidate: candidate.ID, OK: true, Code: "desync_restored_idle"})
				return
			}
		}
		a.desyncService(lease, "voice", DesyncAutoService{Phase: "profile", Candidate: "profile", Code: "sticky_until_idle"})
		if !desyncWait(ctx, 4*time.Second) {
			return
		}
	}
}
