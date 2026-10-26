package desync

import (
	"fmt"
	"reflect"
	"testing"
	"time"
)

func TestDesyncCustomCandidatesAreYouTubeThenDiscordTCPWithoutDuplicates(t *testing.T) {
	ids := func(cs []Candidate) []string {
		var out []string
		for _, c := range cs {
			out = append(out, c.ID)
		}
		return out
	}
	// Without fake TTL: exactly the YouTube set; Discord's "record" (1+s)
	// and "split" repeat r1 and split, so nothing is added.
	if got := ids(CustomCandidates(false)); !reflect.DeepEqual(got, ids(Candidates("youtube"))) {
		t.Fatalf("no-fake custom set = %v", got)
	}
	withFake := CustomCandidates(true)
	want := ids(Candidates("youtube"))
	for _, c := range Candidates("discord") {
		if c.TTL > 0 {
			want = append(want, c.ID)
		}
	}
	if got := ids(withFake); !reflect.DeepEqual(got, want) {
		t.Fatalf("fake custom set = %v, want %v", got, want)
	}
	seen := map[string]bool{}
	for _, c := range withFake {
		if c.UDP {
			t.Fatalf("UDP candidate %s in a TCP set", c.ID)
		}
		key := fmt.Sprint(c.Proxy("")["desync"])
		if seen[key] {
			t.Fatalf("duplicate transform %s", c.ID)
		}
		seen[key] = true
	}
	if got := OrderedCustom(true, "f7"); got[0].ID != "f7" || len(got) != len(withFake) {
		t.Fatal("cached custom winner not first or alternatives lost")
	}
	if got := OrderedCustom(false, "f7"); got[0].ID != "r3" {
		t.Fatal("unsupported cached fake candidate promoted")
	}
}

func TestDesyncCustomCacheIsPerDomain(t *testing.T) {
	cache := []CacheEntry{{Network: "n", YouTube: "r1", Custom: map[string]string{"example.org": "split", "example.net": DirectID}, Updated: time.Now().Unix()}}
	if CachedSelection(cache, "n", CustomService("example.org")) != "split" || CachedSelection(cache, "n", CustomService("example.net")) != DirectID {
		t.Fatal("custom winner not found by domain")
	}
	if CachedSelection(cache, "n", CustomService("other.org")) != "" || CachedSelection(cache, "other", CustomService("example.org")) != "" {
		t.Fatal("custom winner crossed domain or network")
	}
	if CachedSelection(cache, "n", "youtube") != "r1" {
		t.Fatal("built-in lookup changed")
	}
}

func TestDesyncBuiltinDomainCoverage(t *testing.T) {
	for domain, want := range map[string]string{"youtube.com": "youtube", "m.youtube.com": "youtube", "rr1.googlevideo.com": "youtube", "cdn.discordapp.com": "discord", "googleapis.com": "", "notyoutube.com": "", "example.org": ""} {
		if got := BuiltinDomainFor(domain); got != want {
			t.Errorf("%s: %q, want %q", domain, got, want)
		}
	}
}

func TestDesyncStableBindingOnlyGeneratedForDiscordUDP(t *testing.T) {
	for _, service := range []string{"youtube", "discord", "voice"} {
		for _, candidate := range Candidates(service) {
			proxy := candidate.Proxy("test-route")
			opts := proxy["desync"].(map[string]any)
			udp, present := opts["udp"].(map[string]any)
			if service == "voice" {
				if !present || udp["stable-binding"] != true {
					t.Fatal("Discord UDP candidate lost binding continuity")
				}
			} else if present {
				t.Fatal("TCP candidate received UDP pooling")
			}
		}
	}
}

func TestDesyncCandidatesCoverTTLRangeWithoutUDPRepeatFlood(t *testing.T) {
	for _, service := range []string{"discord", "voice"} {
		seen := map[int]bool{}
		for _, c := range Candidates(service) {
			if c.TTL > 0 {
				seen[c.TTL] = true
			}
			if service == "voice" && c.Repeats > 2 {
				t.Fatal("aggressive UDP burst reintroduced")
			}
		}
		for ttl := 1; ttl <= 32; ttl++ {
			if !seen[ttl] {
				t.Fatalf("%s has no candidate for TTL %d", service, ttl)
			}
		}
	}
}
