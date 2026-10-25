package main

import (
	"context"
	"net/http"
	"net/url"
	"strings"
	"time"
)

type desyncVoicePeer struct {
	previous               desyncUDPActivity
	stable, media, mediaTX int
	established            bool
	badSince, seen         time.Time
}

type desyncVoiceMonitor struct{ peers map[string]*desyncVoicePeer }

type desyncVoiceDecision struct {
	phase, target, affected string
	healthy, stalled        int
	currentHealthy          bool
}

// Observe every active endpoint independently. A receive-only stream and a
// two-way call are different flows; replacing one must not reset the other.
func (m *desyncVoiceMonitor) observe(now time.Time, connections []ConnectionItem, activity []desyncUDPActivity, selected ...string) desyncVoiceDecision {
	if m.peers == nil {
		m.peers = map[string]*desyncVoicePeer{}
	}
	active := map[string]bool{}
	for _, c := range connections {
		if target := desyncVoiceTarget([]ConnectionItem{c}); target != "" {
			active[target] = true
		}
	}
	d := desyncVoiceDecision{phase: "waiting"}
	observed, bad := 0, 0
	for _, p := range activity {
		if !active[p.Endpoint] {
			continue
		}
		if now.UnixMilli()-p.LastTXMS > 10000 && now.UnixMilli()-p.LastRXMS > 10000 {
			delete(active, p.Endpoint)
			continue
		}
		observed++
		peer := m.peers[p.Endpoint]
		if peer == nil || p.TXPackets < peer.previous.TXPackets || p.RXPackets < peer.previous.RXPackets {
			peer = &desyncVoicePeer{}
			m.peers[p.Endpoint] = peer
		}
		peer.seen = now
		if p.RXPackets > peer.previous.RXPackets {
			peer.stable++
		} else if now.UnixMilli()-p.LastRXMS > 15000 {
			peer.stable = 0
		}
		if p.MediaRX > peer.previous.MediaRX {
			peer.media++
		} else if now.UnixMilli()-p.LastRXMS > 10000 {
			peer.media = 0
		}
		if p.RXBytes >= 4096 && p.RXPackets > p.DiscoveryRX+8 {
			peer.established = true
		}
		if p.MediaTX > peer.previous.MediaTX {
			peer.mediaTX++
		} else if now.UnixMilli()-p.LastTXMS > 10000 {
			peer.mediaTX = 0
		}
		recentRX := now.UnixMilli()-p.LastRXMS < 10000
		phase := "observing"
		if peer.stable >= 3 && recentRX && p.RXPackets > p.DiscoveryRX+2 {
			phase = "transport"
			if peer.media >= 3 && p.MediaRX >= 3 {
				phase = "receiving"
				if p.MediaTX >= 3 && peer.mediaTX >= 3 {
					phase = "media"
				}
			}
			d.healthy++
			if len(selected) == 0 || p.Candidate == selected[0] {
				d.currentHealthy = true
			}
			if d.phase != "media" && (d.phase != "receiving" || phase == "media") {
				d.phase = phase
			}
		}
		stalled := desyncVoiceHealth(now, p, 0, 0, time.Time{}) == "stalled" && p.TXPackets > peer.previous.TXPackets
		if stalled {
			if peer.badSince.IsZero() {
				peer.badSince = now
			}
			// Require multiple separated samples, then a longer grace for a
			// connection that already carried application/media traffic.
			if now.Sub(peer.badSince) >= 8*time.Second {
				d.stalled++
				if d.affected == "" {
					d.affected = p.Endpoint
				}
				grace := 8 * time.Second
				if peer.established {
					grace = 60 * time.Second
				}
				if now.Sub(peer.badSince) >= grace {
					bad++
					if d.target == "" {
						d.target = p.Endpoint
					}
				}
			}
		} else {
			peer.badSince = time.Time{}
		}
		peer.previous = p
	}
	for target, peer := range m.peers {
		if now.Sub(peer.seen) > 2*time.Minute {
			delete(m.peers, target)
		}
	}
	for len(m.peers) > 64 {
		oldest := ""
		var seen time.Time
		for target, peer := range m.peers {
			if oldest == "" || peer.seen.Before(seen) {
				oldest, seen = target, peer.seen
			}
		}
		delete(m.peers, oldest)
	}
	if d.healthy > 0 {
		d.target = ""
		if d.stalled > 0 {
			d.phase = "partial"
		}
	} else if len(active) > 0 {
		d.phase = "observing"
		if d.stalled > 0 {
			d.phase = "interrupted"
		}
		// A missing/new peer is not evidence that the whole service failed.
		if observed != len(active) || bad != observed {
			d.target = ""
		}
	}
	return d
}

func knownDesyncVoiceCandidate(name string) bool {
	for _, c := range desyncCandidates("voice") {
		if c.name(desyncDiscordUDPRoute) == name {
			return true
		}
	}
	return false
}

func readDesyncVoiceActivity(ctx context.Context, endpoint, secret, selected string, connections []ConnectionItem) ([]desyncUDPActivity, error) {
	names := []string{selected}
	seen := map[string]bool{selected: true}
	for _, c := range connections {
		if desyncVoiceTarget([]ConnectionItem{c}) == "" {
			continue
		}
		for _, name := range c.Chains {
			if !seen[name] && knownDesyncVoiceCandidate(name) && len(names) < 8 {
				names = append(names, name)
				seen[name] = true
			}
		}
	}
	peers := map[string]desyncUDPActivity{}
	for _, name := range names {
		if !knownDesyncVoiceCandidate(name) {
			continue
		}
		var proxy desyncUDPProxy
		if err := autoCoreJSON(ctx, endpoint, secret, http.MethodGet, "/proxies/"+url.PathEscape(name), nil, &proxy); err != nil {
			return nil, err
		}
		if proxy.UDP == nil {
			continue
		}
		for _, p := range proxy.UDP.Activity {
			p.Candidate = name
			if old, ok := peers[p.Endpoint]; !ok || p.LastRXMS > old.LastRXMS {
				peers[p.Endpoint] = p
			}
		}
	}
	all := make([]desyncUDPActivity, 0, len(peers))
	for _, p := range peers {
		all = append(all, p)
	}
	return all, nil
}

func (a *App) runDesyncVoiceService(ctx context.Context, lease uint64, network, endpoint, secret string) {
	monitor := desyncVoiceMonitor{}
	rejected := map[string]bool{}
	target, previousSelected, validated, lastDegraded := "", "", "", ""
	var retryAt, graceUntil time.Time
	ticker := time.NewTicker(4 * time.Second)
	defer ticker.Stop()
	for {
		one, cancel := context.WithTimeout(ctx, 5*time.Second)
		var overview struct {
			Connections []ConnectionItem `json:"connections"`
		}
		var group desyncUDPProxy
		err := autoCoreJSON(one, endpoint, secret, http.MethodGet, "/connections", nil, &overview)
		if err == nil {
			err = autoCoreJSON(one, endpoint, secret, http.MethodGet, "/proxies/"+desyncDiscordUDPRoute, nil, &group)
		}
		if err == nil && !knownDesyncVoiceCandidate(group.Now) {
			err = &desyncControllerError{Status: 503, Code: "candidate_pending"}
		}
		var activity []desyncUDPActivity
		if err == nil {
			activity, err = readDesyncVoiceActivity(one, endpoint, secret, group.Now, overview.Connections)
		}
		cancel()
		if ctx.Err() != nil {
			return
		}
		if err != nil {
			a.desyncService(lease, "voice", DesyncAutoService{Phase: "observing", Code: desyncErrorCode(err)})
		} else {
			id := strings.TrimPrefix(group.Now, desyncDiscordUDPRoute+"-")
			if previousSelected != "" && previousSelected != group.Now {
				monitor = desyncVoiceMonitor{}
				validated = ""
			}
			previousSelected = group.Now
			d := monitor.observe(time.Now(), overview.Connections, activity, group.Now)
			a.desyncService(lease, "voice", DesyncAutoService{Phase: d.phase, Candidate: id})
			if d.healthy > 0 {
				if d.currentHealthy && validated != id {
					validated = id
					if desyncCachedSelection(network, "voice") != id {
						saveDesyncSelection(network, "voice", id)
					}
					a.desyncAttempt(lease, DesyncAutoAttempt{Service: "voice", Candidate: id, OK: true, Code: "application_traffic"})
				}
				if d.stalled > 0 && lastDegraded != id {
					lastDegraded = id
					a.desyncAttempt(lease, DesyncAutoAttempt{Service: "voice", Candidate: id, Target: d.affected, Code: "peer_stalled_others_healthy"})
				} else if d.stalled == 0 {
					lastDegraded = ""
				}
			} else {
				validated = ""
			}
			if d.target != "" && time.Now().After(retryAt) && time.Now().After(graceUntil) {
				if target != d.target {
					target = d.target
					rejected = map[string]bool{}
				}
				rejected[id] = true
				a.desyncAttempt(lease, DesyncAutoAttempt{Service: "voice", Candidate: id, Target: target, Code: "udp_stalled"})
				chosen, changed, err := a.calibrateDesyncUDP(ctx, lease, network, endpoint, secret, target, rejected)
				if ctx.Err() != nil {
					return
				}
				retryAt = time.Now().Add(time.Minute)
				if err != nil {
					a.desyncService(lease, "voice", DesyncAutoService{Phase: "interrupted", Candidate: id, Code: desyncErrorCode(err)})
				} else if changed {
					closeStaleDesyncVoice(ctx, endpoint, secret, target, chosen, overview.Connections)
					graceUntil = time.Now().Add(75 * time.Second)
				}
				if len(rejected) >= len(desyncCandidates("voice")) {
					rejected = map[string]bool{}
				}
			}
		}
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
		}
	}
}
