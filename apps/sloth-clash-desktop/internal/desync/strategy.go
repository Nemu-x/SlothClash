package desync

import (
	dlib "github.com/Nemu-x/desync"
	"github.com/Nemu-x/desync/presets"
)

// Strategy describes one catalog preset for the UI: what kind of
// transformation it is (Kind, a fixed vocabulary the UI translates), which
// variant of that kind (1-based, in catalog order), and whether calibration
// tries it on this computer.
type Strategy struct {
	ID       string `json:"id"`
	Name     string `json:"name"`
	Protocol string `json:"protocol"` // "tcp" or "udp"
	Kind     string `json:"kind"`
	Variant  int    `json:"variant"`
	// Description is the catalog's own (English, technical) description.
	Description string `json:"description"`
	// Available reports that this build implements every method the preset
	// needs on this OS; Reason says what is missing otherwise.
	Available bool   `json:"available"`
	Reason    string `json:"reason,omitempty"`
	// Tried reports that calibration uses the preset (Candidates).
	Tried bool `json:"tried"`
}

// Strategy kinds.
const (
	KindTLSRec       = "tlsrec"        // TLS record split at fixed points
	KindTLSRecRandom = "tlsrec-random" // TLS record split at random points
	KindSplit        = "split"         // TCP write split at fixed points
	KindSplitRandom  = "split-random"  // TCP write split at random points
	KindTLSRecSplit  = "tlsrec-split"  // both
	KindSplitWait    = "split-wait"    // split, waiting for delivery between pieces
	KindDisorder     = "disorder"      // first piece with hop limit 1
	KindOOB          = "oob"           // urgent byte after the first piece
	KindDisorderOOB  = "disorder-oob"  // both
	KindFake         = "fake"          // low-TTL decoy first
	KindFakeSplit    = "fake-split"    // decoy combined with a split or disorder
	KindNone         = "none"          // baseline, no transformation
	KindUDPZero      = "udp-zero"      // low-TTL zero-payload datagrams
	KindUDPQUIC      = "udp-quic"      // low-TTL QUIC Initial decoys
)

// TCPKind classifies a TCP transformation.
func TCPKind(o dlib.Options) string {
	splits := len(o.Split) > 0 || o.Random != nil && len(o.Random.Split) > 0
	switch {
	case !o.Enabled():
		return KindNone
	case o.Fake != nil && (splits || o.Disorder || len(o.TLSRec) > 0):
		return KindFakeSplit
	case o.Fake != nil:
		return KindFake
	case o.Disorder && o.OOB != nil:
		return KindDisorderOOB
	case o.Disorder:
		return KindDisorder
	case o.OOB != nil:
		return KindOOB
	case o.SplitWait != dlib.SplitWaitNone:
		return KindSplitWait
	case o.Random != nil && len(o.Random.TLSRec) > 0:
		return KindTLSRecRandom
	case o.Random != nil && len(o.Random.Split) > 0:
		return KindSplitRandom
	case len(o.TLSRec) > 0 && len(o.Split) > 0:
		return KindTLSRecSplit
	case len(o.TLSRec) > 0:
		return KindTLSRec
	}
	return KindSplit
}

// UDPKind classifies a UDP burst.
func UDPKind(o dlib.UDPOptions) string {
	if o.Template == dlib.UDPTemplateQUICInitial {
		return KindUDPQUIC
	}
	return KindUDPZero
}

// Strategies describes the whole catalog (TCP, then UDP, each in catalog
// order, the baseline left out) for caps.
func Strategies(caps dlib.Capabilities) []Strategy {
	tried := map[string]bool{}
	for _, service := range []string{"youtube", "voice"} {
		for _, c := range Candidates(service, caps) {
			tried[c.ID] = true
		}
	}
	reasons := map[string]string{}
	_, skipped := presets.Filter(presets.TCP(), caps)
	_, skippedUDP := presets.FilterUDP(presets.UDP(), caps)
	for _, s := range append(skipped, skippedUDP...) {
		reasons[s.Name] = s.Reason
	}
	variants := map[string]int{}
	var out []Strategy
	add := func(id, name, protocol, kind, description string) {
		variants[kind]++
		reason, missing := reasons[name]
		out = append(out, Strategy{ID: id, Name: name, Protocol: protocol, Kind: kind, Variant: variants[kind],
			Description: description, Available: !missing, Reason: reason, Tried: tried[id]})
	}
	for _, p := range presets.TCP() {
		if p.Name == presets.BaselineName {
			continue
		}
		add(p.ID(), p.Name, "tcp", TCPKind(p.Options), p.Description)
	}
	for _, p := range presets.UDP() {
		add(p.ID(), p.Name, "udp", UDPKind(p.Options), p.Description)
	}
	return out
}

// Capability is one method of the library and whether this build implements
// it on this OS. It says nothing about whether the method helps on a given
// network.
type Capability struct {
	Method    string `json:"method"`
	Supported bool   `json:"supported"`
	Reason    string `json:"reason,omitempty"`
	// Inferred: supported, but the library infers the moment it waits for
	// rather than reading it from the kernel (split-wait, disorder on some
	// systems).
	Inferred bool `json:"inferred,omitempty"`
}

// Capabilities lists the methods the catalog uses, in a fixed order. Record
// and write splits, random cuts included, are plain socket writes available
// everywhere.
func Capabilities(caps dlib.Capabilities) []Capability {
	feature := func(method string, f presets.Feature) Capability {
		ok, why := f.Check(caps)
		c := Capability{Method: method, Supported: ok}
		if !ok {
			c.Reason = why
		}
		return c
	}
	out := []Capability{
		{Method: "tlsrec", Supported: true},
		{Method: "split", Supported: true},
		feature("split-wait-sent", presets.FeatureSplitWaitSent),
		feature("split-wait-acked", presets.FeatureSplitWaitAcked),
		feature("disorder", presets.FeatureDisorder),
		feature("oob", presets.FeatureOOB),
		feature("fake-tcp", presets.FeatureFakeTCP),
		feature("fake-udp", presets.FeatureFakeUDP),
	}
	for i := range out {
		switch out[i].Method {
		case "split-wait-sent", "split-wait-acked":
			out[i].Inferred = out[i].Supported && !caps.Ack.Reliable
		case "disorder":
			out[i].Inferred = out[i].Supported && !caps.Disorder.Observed
		}
	}
	return out
}
