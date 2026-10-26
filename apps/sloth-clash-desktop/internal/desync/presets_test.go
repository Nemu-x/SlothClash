package desync

import (
	"reflect"
	"strings"
	"testing"

	"golang.org/x/net/publicsuffix"
)

// sharedPresetDomains is every domain more than one preset may list, with the
// presets listing it in catalog order (the first enabled one owns it). Any
// other overlap is a catalog mistake.
var sharedPresetDomains = map[string][]string{
	"fbcdn.net": {"facebook", "instagram"},
}

// presetMediaCDN is each preset's own media/static CDN domain, which its
// probes must always check (the owner's failure: page loads, images do not).
var presetMediaCDN = map[string]string{"facebook": "fbcdn.net", "instagram": "cdninstagram.com", "x": "twimg.com"}

func TestPresetCatalogSanity(t *testing.T) {
	if len(presetMediaCDN) != len(Presets) {
		t.Fatalf("declare the media CDN of every preset: %v", presetMediaCDN)
	}
	ids := map[string]bool{}
	listedBy := map[string][]string{}
	for _, p := range Presets {
		if p.ID == "" || strings.Trim(p.ID, "abcdefghijklmnopqrstuvwxyz0123456789") != "" || ids[p.ID] {
			t.Fatalf("bad or duplicate preset id %q", p.ID)
		}
		ids[p.ID] = true
		if p.Name == "" || len(p.Domains) == 0 {
			t.Fatalf("%s: empty name or domains", p.ID)
		}
		seen := map[string]bool{}
		for _, d := range p.Domains {
			if seen[d] {
				t.Fatalf("%s: duplicate domain %s", p.ID, d)
			}
			seen[d] = true
			// Lowercase ASCII registrable domains: the rule text takes them
			// verbatim, and a public suffix would route a whole zone.
			if d != strings.ToLower(d) || strings.Trim(d, "abcdefghijklmnopqrstuvwxyz0123456789.-") != "" {
				t.Fatalf("%s: domain %q is not normalized", p.ID, d)
			}
			if suffix, _ := publicsuffix.PublicSuffix(d); suffix == d {
				t.Fatalf("%s: %s is a public suffix", p.ID, d)
			}
			if BuiltinDomainFor(d) != "" {
				t.Fatalf("%s: %s is covered by a built-in service", p.ID, d)
			}
			for _, other := range p.Domains {
				if other != d && DomainsOverlap(d, other) {
					t.Fatalf("%s: %s and %s overlap", p.ID, d, other)
				}
			}
			listedBy[d] = append(listedBy[d], p.ID)
		}
		for _, host := range p.Probes {
			under := false
			for _, d := range p.Domains {
				under = under || CoversDomain(d, host)
			}
			if !under {
				t.Fatalf("%s: probe %s is not under any of its domains", p.ID, host)
			}
		}
	}
	// Page AND CDN: even with every preset on (the fewest owned domains),
	// each preset's probes still cover at least two of its domains: the site
	// and its media/static CDN.
	all := []string{}
	for _, p := range Presets {
		all = append(all, p.ID)
	}
	for _, p := range Presets {
		owned := PresetOwnedDomains(p.ID, all)
		covered := map[string]bool{}
		for _, host := range PresetProbes(p.ID, owned) {
			for _, d := range owned {
				if CoversDomain(d, host) {
					covered[d] = true
				}
			}
		}
		if len(covered) < 2 || !covered[presetMediaCDN[p.ID]] {
			t.Fatalf("%s: probes cover only %v with every preset on (media CDN %s)", p.ID, covered, presetMediaCDN[p.ID])
		}
	}
	for d, by := range listedBy {
		if len(by) > 1 && !reflect.DeepEqual(by, sharedPresetDomains[d]) {
			t.Fatalf("domain %s listed by %v; shared domains must be declared", d, by)
		}
	}
	for d, by := range sharedPresetDomains {
		if !reflect.DeepEqual(listedBy[d], by) {
			t.Fatalf("declared shared domain %s is listed by %v", d, listedBy[d])
		}
	}
	// Not twitter: xAI's domains stay out of the X preset.
	for _, d := range Presets[2].Domains {
		if d == "x.ai" || d == "grok.com" {
			t.Fatalf("x preset lists %s", d)
		}
	}
}

func TestPresetOwnershipFollowsCatalogOrder(t *testing.T) {
	has := func(list []string, d string) bool {
		for _, v := range list {
			if v == d {
				return true
			}
		}
		return false
	}
	// Instagram alone owns the shared CDN and probes it.
	insta := PresetOwnedDomains("instagram", []string{"instagram"})
	if !has(insta, "fbcdn.net") || !has(PresetProbes("instagram", insta), "scontent.xx.fbcdn.net") {
		t.Fatalf("instagram alone: %v", insta)
	}
	// With Facebook on, Facebook owns it whatever the list order, and
	// Instagram neither routes nor probes it.
	for _, enabled := range [][]string{{"facebook", "instagram"}, {"instagram", "facebook"}} {
		insta = PresetOwnedDomains("instagram", enabled)
		fb := PresetOwnedDomains("facebook", enabled)
		if has(insta, "fbcdn.net") || !has(fb, "fbcdn.net") {
			t.Fatalf("%v: instagram %v facebook %v", enabled, insta, fb)
		}
		probes := PresetProbes("instagram", insta)
		if has(probes, "scontent.xx.fbcdn.net") || !reflect.DeepEqual(probes, []string{"www.instagram.com", "static.cdninstagram.com", "scontent.cdninstagram.com"}) {
			t.Fatalf("%v: instagram probes %v", enabled, probes)
		}
	}
	// No two enabled presets ever own one domain.
	all := []string{}
	for _, p := range Presets {
		all = append(all, p.ID)
	}
	owner := map[string]string{}
	for _, p := range Presets {
		for _, d := range PresetOwnedDomains(p.ID, all) {
			if owner[d] != "" {
				t.Fatalf("%s owned by %s and %s", d, owner[d], p.ID)
			}
			owner[d] = p.ID
		}
	}
	if PresetOwnedDomains("nope", all) != nil || PresetProbes("nope", []string{"x.com"}) != nil {
		t.Fatal("unknown preset has domains")
	}
}

func TestPresetForDomainAndEntryKey(t *testing.T) {
	cases := map[string]string{"facebook.com": "facebook", "scontent-ams.xx.fbcdn.net": "facebook", "www.instagram.com": "instagram", "pbs.twimg.com": "x", "x.ai": "", "example.org": "", "notfacebook.com": ""}
	for d, want := range cases {
		if got := PresetForDomain(d); got != want {
			t.Fatalf("PresetForDomain(%s) = %q, want %q", d, got, want)
		}
	}
	if CustomEntryKey("example.org", "") != "example.org" || CustomService(CustomEntryKey("", "facebook")) != "custom:preset:facebook" {
		t.Fatal("entry key")
	}
	if key, ok := CustomServiceDomain(CustomService(CustomEntryKey("", "x"))); !ok || key != "preset:x" {
		t.Fatalf("service key round trip: %q", key)
	}
}
