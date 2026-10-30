package desync

import (
	"strings"
	"time"

	dlib "github.com/Nemu-x/desync"
	"github.com/Nemu-x/desync/presets"
)

// Platform is what the desync library implements on this OS. The app pins the
// library version core patch 0023 pins, so a candidate generated from these
// capabilities is one the core accepts: a method the OS lacks is never
// written, since the core rejects the whole config with it.
var Platform = dlib.PlatformCapabilities()

// Allowed is caps with the methods listed in Conditional counted as
// available: the library implements them but decides per socket (Android
// only), and the core accepts them in a config. Such a strategy may fail at
// runtime, which calibration observes like any other failure.
func Allowed(caps dlib.Capabilities) dlib.Capabilities {
	if len(caps.Conditional) == 0 {
		return caps
	}
	out := caps
	out.Unsupported = map[string]string{}
	for k, v := range caps.Unsupported {
		if _, conditional := caps.Conditional[k]; !conditional {
			out.Unsupported[k] = v
		}
	}
	for k := range caps.Conditional {
		switch k {
		case "fake-tcp":
			out.FakeTCP = true
		case "split-wait-sent":
			out.Ack.Sent = true
		case "disorder":
			out.Disorder.Supported = true
		}
	}
	return out
}

// Candidate is one strategy a route can use: a preset of the desync library's
// catalog. ID is the catalog's stable ID (persisted in the winner cache);
// Name(route) is the generated outbound implementing it on route.
type Candidate struct {
	ID string
	// Preset is the catalog name ("tlsrec-3s"), for logs and tests.
	Preset string
	// Exactly one of TCP and UDP is set.
	TCP *dlib.Options
	UDP *dlib.UDPOptions
}

// Name is the generated proxy name of c on route.
func (c Candidate) Name(route string) string { return route + "-" + c.ID }

// DirectID is the cached selection of a custom domain that is not blocked:
// the plain DIRECT member of its group, with no transform.
const DirectID = "direct"

// youtubeLead is tried first for YouTube when nothing is cached: TLS record
// fragmentation 3 bytes into the server name, the proven default of the
// hand-written list this catalog replaced.
const youtubeLead = "tlsrec-3s"

// maxVoiceRepeats bounds the UDP burst of voice candidates: the catalog's
// heavy preset (12 decoys before each of the first datagrams) is left out,
// in line with the conservative bursts voice always used.
const maxVoiceRepeats = 6

// Candidates returns the strategies tried for service, in probe order, with
// the presets caps cannot run left out. "voice" (Discord UDP) gets the
// catalog's zero-payload UDP bursts; QUIC Initial decoys only act before QUIC
// Initials and would leave voice untouched. Every other service ("youtube",
// "discord", a custom entry) gets the TCP presets in the catalog's ranked
// order (cheapest and least risky first), without the baseline: plain DIRECT
// is not a strategy here (custom entries probe it separately, DirectID).
func Candidates(service string, caps dlib.Capabilities) []Candidate {
	caps = Allowed(caps)
	var out []Candidate
	if service == "voice" {
		usable, _ := presets.FilterUDP(presets.UDP(), caps)
		for _, p := range presets.RankedUDP(usable) {
			o := p.Options
			if o.Template != dlib.UDPTemplateZero || o.QUICOnly || udpRepeats(o) > maxVoiceRepeats || !udpExpressible(o) {
				continue
			}
			out = append(out, Candidate{ID: p.ID(), Preset: p.Name, UDP: &o})
		}
		return out
	}
	usable, _ := presets.Filter(presets.TCP(), caps)
	for _, p := range presets.Ranked(usable) {
		o := p.Options
		if !o.Enabled() || !tcpExpressible(o) {
			continue
		}
		c := Candidate{ID: p.ID(), Preset: p.Name, TCP: &o}
		if service == "youtube" && p.Name == youtubeLead {
			out = append([]Candidate{c}, out...)
			continue
		}
		out = append(out, c)
	}
	return out
}

// Ordered is Candidates(service, caps) with the cached winner, if any, moved
// first.
func Ordered(service string, caps dlib.Capabilities, cached string) []Candidate {
	return moveFirst(Candidates(service, caps), cached)
}

func moveFirst(candidates []Candidate, cached string) []Candidate {
	for i, c := range candidates {
		if c.ID == cached {
			return append([]Candidate{c}, append(candidates[:i:i], candidates[i+1:]...)...)
		}
	}
	return candidates
}

func udpRepeats(o dlib.UDPOptions) int {
	if o.Repeats == 0 {
		return 6
	}
	return o.Repeats
}

// tcpExpressible reports whether every field o sets has a key in the desync
// outbound. The catalog sets none of the others; a future preset that does
// is skipped rather than generated without them.
func tcpExpressible(o dlib.Options) bool {
	return o.ClientHello == nil
}

func udpExpressible(o dlib.UDPOptions) bool {
	return len(o.Payload) == 0
}

// Proxy is the generated "type: desync" outbound implementing c on route
// (core patch 0023; the same keys as Citrus' desync outbound). dns-fallback,
// reject-quic and udp.stable-binding are the SlothClash-only keys: every
// candidate retries an empty direct DNS answer through the profile resolver;
// TCP candidates reject QUIC so the browser falls back to TCP, where the
// transform applies; voice candidates keep one native socket per Discord
// voice association.
func (c Candidate) Proxy(route string) map[string]any {
	p := map[string]any{"name": c.Name(route), "type": "desync", ManagedKey: true, "dns-fallback": true}
	if c.TCP != nil {
		addTCPOptions(p, *c.TCP)
		p["reject-quic"] = true
	}
	if c.UDP != nil {
		u := udpOptions(*c.UDP)
		u["stable-binding"] = true
		p["udp"] = u
	}
	return p
}

func positions(ps []dlib.Pos) string {
	parts := make([]string, len(ps))
	for i, p := range ps {
		parts[i] = p.String()
	}
	return strings.Join(parts, ",")
}

func randomCuts(cuts []dlib.RandomCut) []any {
	out := make([]any, 0, len(cuts))
	for _, c := range cuts {
		cut := map[string]any{"from": c.From.String(), "to": c.To.String()}
		if c.Count != 0 {
			cut["count"] = c.Count
		}
		if c.MinPiece != 0 {
			cut["min-piece"] = c.MinPiece
		}
		out = append(out, cut)
	}
	return out
}

// addTCPOptions writes the keys of o into p, omitting zero values (the
// outbound's defaults are the library's).
func addTCPOptions(p map[string]any, o dlib.Options) {
	if len(o.TLSRec) > 0 {
		p["tlsrec"] = positions(o.TLSRec)
	}
	if len(o.Split) > 0 {
		p["split"] = positions(o.Split)
	}
	if o.SplitDelay > 0 {
		p["split-delay"] = int(o.SplitDelay.Milliseconds())
	}
	if o.SplitWait != dlib.SplitWaitNone {
		p["split-wait"] = o.SplitWait.String()
	}
	if o.SplitWaitTimeout > 0 {
		p["split-wait-timeout"] = int(o.SplitWaitTimeout.Milliseconds())
	}
	if o.Disorder {
		p["disorder"] = true
	}
	if o.OOB != nil {
		p["oob"] = true
		if o.OOB.Byte != 0 {
			p["oob-byte"] = string(rune(o.OOB.Byte))
		}
	}
	if r := o.Random; r != nil {
		random := map[string]any{}
		if len(r.TLSRec) > 0 {
			random["tlsrec"] = randomCuts(r.TLSRec)
		}
		if len(r.Split) > 0 {
			random["split"] = randomCuts(r.Split)
		}
		if r.Seed != 0 {
			random["seed"] = r.Seed
		}
		p["random"] = random
	}
	if f := o.Fake; f != nil {
		fake := map[string]any{}
		if f.TTL != 0 {
			fake["ttl"] = f.TTL
		}
		if a := f.AutoTTL; a != nil {
			auto := map[string]any{}
			for key, v := range map[string]int{"delta": a.Delta, "min": a.Min, "max": a.Max} {
				if v != 0 {
					auto[key] = v
				}
			}
			fake["auto-ttl"] = auto
		}
		if f.Prefix != 0 {
			fake["prefix"] = f.Prefix
		}
		if f.Timeout > 0 {
			fake["timeout"] = int(f.Timeout.Milliseconds())
		}
		if len(f.SNI) > 0 {
			sni := make([]any, len(f.SNI))
			for i, s := range f.SNI {
				sni[i] = s
			}
			fake["sni"] = sni
		}
		if f.Randomize {
			fake["randomize"] = true
		}
		if f.FromOriginal {
			fake["from-original"] = true
		}
		if f.Offset != 0 {
			fake["offset"] = f.Offset
		}
		p["fake"] = fake
	}
}

func udpOptions(o dlib.UDPOptions) map[string]any {
	u := map[string]any{"ttl": o.TTL}
	if o.Repeats != 0 {
		u["repeats"] = o.Repeats
	}
	if o.Packets != 0 {
		u["packets"] = o.Packets
	}
	if o.Template == dlib.UDPTemplateQUICInitial {
		u["template"] = "quic"
	}
	if o.SNI != "" {
		u["sni"] = o.SNI
	}
	if o.QUICOnly {
		u["quic-only"] = true
	}
	return u
}

// legacyIDs maps the IDs of the hand-written candidate list that preceded the
// catalog to the catalog preset with the same transformation, so a winner
// cached by an earlier build is still tried first. The others (TLS record
// plus a fake, split with a delay, single-decoy UDP bursts) have no catalog
// equivalent; their cached winners are ignored and the search starts from
// the catalog order.
var legacyIDs = func() map[string]string {
	ids := map[string]string{}
	for _, p := range presets.TCP() {
		switch p.Name {
		case "tlsrec-1s":
			ids["r1"] = p.ID()
			ids["record"] = p.ID()
		case "tlsrec-3s":
			ids["r3"] = p.ID()
		}
	}
	return ids
}()

// cachedID returns the catalog ID of a cached winner: a catalog ID or
// DirectID unchanged, a legacy ID mapped, anything else "" (not cached).
func cachedID(id string) string {
	if id == "" || id == DirectID {
		return id
	}
	if mapped, ok := legacyIDs[id]; ok {
		return mapped
	}
	if _, ok := presets.ByID(id); ok {
		return id
	}
	return ""
}

// CachedSelection returns the candidate ID cached as the winner for service on
// network, or "" when there is none, it is older than a day, or it names no
// strategy of the current catalog.
func CachedSelection(cache []CacheEntry, network, service string) string {
	for _, c := range cache {
		if c.Network != network || time.Now().Unix()-c.Updated > 86400 {
			continue
		}
		if domain, ok := CustomServiceDomain(service); ok {
			return cachedID(c.Custom[domain])
		}
		switch service {
		case "youtube":
			return cachedID(c.YouTube)
		case "discord":
			return cachedID(c.Discord)
		case "voice":
			return cachedID(c.Voice)
		}
	}
	return ""
}

// CacheEntry is one network's remembered winners (the desktop prefs persist
// them as DesyncNetworkCache, which has the same fields).
type CacheEntry struct {
	Network string
	YouTube string
	Discord string
	Voice   string
	// Custom maps a custom entry key (CustomEntryKey) to its winner (a
	// candidate ID or DirectID).
	Custom  map[string]string
	Updated int64
}
