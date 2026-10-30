package desync_test

import (
	"fmt"
	"reflect"
	"runtime"
	"strings"
	"testing"
	"time"

	dlib "github.com/Nemu-x/desync"
	"github.com/Nemu-x/desync/presets"

	"SlothClashDesktop/internal/desync"
	"SlothClashDesktop/internal/desync/desynctest"
)

func ids(cs []desync.Candidate) []string {
	var out []string
	for _, c := range cs {
		out = append(out, c.ID)
	}
	return out
}

func presetNames(cs []desync.Candidate) []string {
	var out []string
	for _, c := range cs {
		out = append(out, c.Preset)
	}
	return out
}

func presetID(t *testing.T, name string) string {
	t.Helper()
	for _, p := range presets.TCP() {
		if p.Name == name {
			return p.ID()
		}
	}
	for _, p := range presets.UDP() {
		if p.Name == name {
			return p.ID()
		}
	}
	t.Fatalf("no preset %s", name)
	return ""
}

var fakeTCP = []string{"fake-ttl3", "fake-ttl5", "fake-ttl8", "fake-ttl5-sni", "split-1s-fake-ttl5", "disorder-1-fake-ttl5"}

// The candidates are the catalog's, ranked, without the baseline and without
// the presets the OS cannot run; YouTube leads with its proven default.
func TestCandidatesFollowCatalogPerOS(t *testing.T) {
	for _, tc := range []struct {
		os      string
		caps    dlib.Capabilities
		missing []string
	}{
		{"windows", desynctest.Windows(), nil},
		{"linux", desynctest.Linux(), nil},
		{"darwin", desynctest.Darwin(), append([]string{"split-1s-wait-sent"}, fakeTCP...)},
	} {
		usable, _ := presets.Filter(presets.TCP(), tc.caps)
		var want []string
		for _, p := range presets.Ranked(usable) {
			if p.Name != presets.BaselineName {
				want = append(want, p.Name)
			}
		}
		discord := desync.Candidates("discord", tc.caps)
		if got := presetNames(discord); !reflect.DeepEqual(got, want) {
			t.Fatalf("%s discord = %v, want %v", tc.os, got, want)
		}
		if got := presetNames(desync.Candidates("custom", tc.caps)); !reflect.DeepEqual(got, want) {
			t.Fatalf("%s custom = %v", tc.os, got)
		}
		youtube := presetNames(desync.Candidates("youtube", tc.caps))
		if youtube[0] != "tlsrec-3s" || len(youtube) != len(want) {
			t.Fatalf("%s youtube = %v", tc.os, youtube)
		}
		if len(want)+len(tc.missing) != 22 {
			t.Fatalf("%s: %d TCP candidates, %d skipped; the catalog has 22 transformations", tc.os, len(want), len(tc.missing))
		}
		for _, name := range tc.missing {
			for _, have := range want {
				if have == name {
					t.Fatalf("%s: %s generated without its platform feature", tc.os, name)
				}
			}
		}
		voice := presetNames(desync.Candidates("voice", tc.caps))
		if !reflect.DeepEqual(voice, []string{"udp-zero-ttl5-r2", "udp-zero-ttl3", "udp-zero-ttl5", "udp-zero-ttl8"}) {
			t.Fatalf("%s voice = %v", tc.os, voice)
		}
	}
	none := dlib.Capabilities{OS: "plan9", Unsupported: map[string]string{}}
	if got := desync.Candidates("voice", none); len(got) != 0 {
		t.Fatalf("voice without the UDP burst = %v", presetNames(got))
	}
	for _, c := range desync.Candidates("discord", none) {
		if c.TCP.Fake != nil || c.TCP.Disorder || c.TCP.OOB != nil || c.TCP.SplitWait != dlib.SplitWaitNone {
			t.Fatalf("plan9 got %s", c.Preset)
		}
	}
}

func TestOrderedMovesCachedWinnerFirst(t *testing.T) {
	caps := desynctest.Windows()
	fake := presetID(t, "fake-ttl5")
	got := desync.Ordered("custom", caps, fake)
	if got[0].ID != fake || len(got) != len(desync.Candidates("custom", caps)) {
		t.Fatal("cached winner not first or alternatives lost")
	}
	if got := desync.Ordered("custom", desynctest.Darwin(), fake); got[0].Preset != "tlsrec-1s" {
		t.Fatalf("unsupported cached fake promoted: %s", got[0].Preset)
	}
	if got := desync.Ordered("voice", caps, fake); got[0].UDP == nil {
		t.Fatal("a TCP winner moved into the voice set")
	}
}

func TestCachedSelectionMapsLegacyIDsAndDropsTheRest(t *testing.T) {
	now := time.Now().Unix()
	tlsrec1, tlsrec3 := presetID(t, "tlsrec-1s"), presetID(t, "tlsrec-3s")
	cache := []desync.CacheEntry{{Network: "n", YouTube: "r3", Discord: "f4", Voice: "f4-r2", Custom: map[string]string{
		"example.org": "record", "example.net": desync.DirectID, "example.com": "split", "example.io": presetID(t, "oob-1"),
		"example.de": "s0123456789", "example.fr": "r1",
	}, Updated: now}}
	for service, want := range map[string]string{
		"youtube": tlsrec3, "discord": "", "voice": "",
		desync.CustomService("example.org"): tlsrec1,
		desync.CustomService("example.net"): desync.DirectID,
		desync.CustomService("example.com"): "", // split had a 5 ms delay: no catalog equivalent
		desync.CustomService("example.io"):  presetID(t, "oob-1"),
		desync.CustomService("example.de"):  "", // well-formed, not in this catalog
		desync.CustomService("example.fr"):  tlsrec1,
		desync.CustomService("other.org"):   "",
	} {
		if got := desync.CachedSelection(cache, "n", service); got != want {
			t.Errorf("%s: %q, want %q", service, got, want)
		}
	}
	if desync.CachedSelection(cache, "other", "youtube") != "" {
		t.Fatal("winner crossed networks")
	}
	cache[0].Updated = now - 86401
	if desync.CachedSelection(cache, "n", "youtube") != "" {
		t.Fatal("expired winner used")
	}
}

func TestDesyncBuiltinDomainCoverage(t *testing.T) {
	for domain, want := range map[string]string{"youtube.com": "youtube", "m.youtube.com": "youtube", "rr1.googlevideo.com": "youtube", "cdn.discordapp.com": "discord", "googleapis.com": "", "notyoutube.com": "", "example.org": ""} {
		if got := desync.BuiltinDomainFor(domain); got != want {
			t.Errorf("%s: %q, want %q", domain, got, want)
		}
	}
}

// Proxy writes "type: desync" with the SlothClash-only keys where they
// belong: dns-fallback everywhere, reject-quic on TCP, stable-binding on
// voice.
func TestProxyIsDesyncOutbound(t *testing.T) {
	caps := desynctest.Windows()
	for _, service := range []string{"youtube", "discord", "custom", "voice"} {
		for _, c := range desync.Candidates(service, caps) {
			p := c.Proxy("R")
			if p["type"] != "desync" || p["name"] != "R-"+c.ID || p[desync.ManagedKey] != true || p["dns-fallback"] != true {
				t.Fatalf("%s %s: %v", service, c.Preset, p)
			}
			if _, legacy := p["desync"]; legacy {
				t.Fatalf("%s: legacy direct.desync block", c.Preset)
			}
			udp, hasUDP := p["udp"].(map[string]any)
			if service == "voice" {
				if !hasUDP || udp["stable-binding"] != true || p["reject-quic"] != nil {
					t.Fatalf("voice %s: %v", c.Preset, p)
				}
			} else if hasUDP || p["reject-quic"] != true {
				t.Fatalf("TCP %s: %v", c.Preset, p)
			}
		}
	}
	yt := desync.Candidates("youtube", caps)[0].Proxy("SLOTH-DESYNC-YOUTUBE")
	if fmt.Sprint(yt) != fmt.Sprintf("map[%s:true dns-fallback:true name:SLOTH-DESYNC-YOUTUBE-%s reject-quic:true tlsrec:3+s type:desync]", desync.ManagedKey, presetID(t, "tlsrec-3s")) {
		t.Fatalf("youtube lead = %v", yt)
	}
}

// parseProxy reads the keys Proxy writes back into library options, the way
// the core's desync outbound does (patch 0023, DesyncOption), so the test
// fails when a catalog field is dropped or renamed on the way.
func parseProxy(t *testing.T, p map[string]any) (dlib.Options, *dlib.UDPOptions) {
	t.Helper()
	pos := func(v any) []dlib.Pos {
		s, _ := v.(string)
		if s == "" {
			return nil
		}
		var out []dlib.Pos
		for _, part := range strings.Split(s, ",") {
			p, err := dlib.ParsePos(part)
			if err != nil {
				t.Fatal(err)
			}
			out = append(out, p)
		}
		return out
	}
	num := func(v any) int { n, _ := v.(int); return n }
	cuts := func(v any) []dlib.RandomCut {
		var out []dlib.RandomCut
		list, _ := v.([]any)
		for _, item := range list {
			m := item.(map[string]any)
			from, _ := dlib.ParsePos(m["from"].(string))
			to, _ := dlib.ParsePos(m["to"].(string))
			out = append(out, dlib.RandomCut{From: from, To: to, Count: num(m["count"]), MinPiece: num(m["min-piece"])})
		}
		return out
	}
	known := map[string]bool{"name": true, "type": true, desync.ManagedKey: true, "dns-fallback": true, "reject-quic": true,
		"tlsrec": true, "split": true, "split-delay": true, "split-wait": true, "split-wait-timeout": true, "disorder": true,
		"oob": true, "oob-byte": true, "random": true, "fake": true, "udp": true}
	for k := range p {
		if !known[k] {
			t.Fatalf("unknown key %s", k)
		}
	}
	var o dlib.Options
	o.TLSRec, o.Split = pos(p["tlsrec"]), pos(p["split"])
	o.SplitDelay = time.Duration(num(p["split-delay"])) * time.Millisecond
	o.SplitWaitTimeout = time.Duration(num(p["split-wait-timeout"])) * time.Millisecond
	if w, ok := p["split-wait"].(string); ok {
		o.SplitWait, _ = dlib.ParseSplitWait(w)
	}
	o.Disorder = p["disorder"] == true
	if p["oob"] == true {
		o.OOB = &dlib.OOBOptions{}
	}
	if r, ok := p["random"].(map[string]any); ok {
		o.Random = &dlib.RandomOptions{TLSRec: cuts(r["tlsrec"]), Split: cuts(r["split"])}
	}
	if f, ok := p["fake"].(map[string]any); ok {
		o.Fake = &dlib.FakeOptions{TTL: num(f["ttl"]), Prefix: num(f["prefix"]), Randomize: f["randomize"] == true}
		if list, ok := f["sni"].([]any); ok {
			for _, s := range list {
				o.Fake.SNI = append(o.Fake.SNI, s.(string))
			}
		}
	}
	var udp *dlib.UDPOptions
	if u, ok := p["udp"].(map[string]any); ok {
		udp = &dlib.UDPOptions{TTL: num(u["ttl"]), Repeats: num(u["repeats"]), Packets: num(u["packets"]), QUICOnly: u["quic-only"] == true}
		if u["template"] == "quic" {
			udp.Template = dlib.UDPTemplateQUICInitial
		}
	}
	return o, udp
}

func TestProxyRoundTripsEveryCandidate(t *testing.T) {
	caps := desynctest.Linux() // every method
	for _, service := range []string{"custom", "voice"} {
		for _, c := range desync.Candidates(service, caps) {
			tcp, udp := parseProxy(t, c.Proxy("R"))
			if c.TCP != nil && tcp.Key() != c.TCP.Key() {
				t.Errorf("%s: %s, want %s", c.Preset, tcp.Key(), c.TCP.Key())
			}
			if c.UDP != nil && (udp == nil || udp.Key() != c.UDP.Key()) {
				t.Errorf("%s: UDP options lost", c.Preset)
			}
		}
	}
}

// The fixture of this OS is what the library reports here (for the fields
// that decide which strategies are generated).
func TestPlatformMatchesFixture(t *testing.T) {
	want, ok := desynctest.ByOS(runtime.GOOS)
	if !ok {
		t.Skip("no fixture for " + runtime.GOOS)
	}
	got := desync.Platform
	if got.FakeTCP != want.FakeTCP || got.FakeUDP != want.FakeUDP || got.OOB != want.OOB || got.Disorder != want.Disorder || got.Ack != want.Ack || got.AutoTTL != want.AutoTTL {
		t.Fatalf("platform %+v, fixture %+v", got, want)
	}
	if !reflect.DeepEqual(presetNames(desync.Candidates("custom", got)), presetNames(desync.Candidates("custom", want))) {
		t.Fatal("fixture selects other candidates than the platform")
	}
}
