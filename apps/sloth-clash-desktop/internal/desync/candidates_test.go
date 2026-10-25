package desync

import "testing"

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
