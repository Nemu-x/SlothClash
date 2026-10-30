package desync

import (
	"reflect"
	"strings"
	"testing"

	"golang.org/x/net/publicsuffix"
)

// sharedServiceDomains is every domain more than one service may list, with the
// services listing it in catalog order (the first enabled one owns it). Any
// other overlap is a catalog mistake.
var sharedServiceDomains = map[string][]string{
	"fbcdn.net": {"facebook", "instagram"},
}

// serviceMediaCDN is each service's own media/static CDN domain, which its
// probes must always check (the owner's failure: page loads, images do not).
var serviceMediaCDN = map[string]string{"facebook": "fbcdn.net", "instagram": "cdninstagram.com", "x": "twimg.com"}

func TestServiceCatalogSanity(t *testing.T) {
	if len(serviceMediaCDN) != len(Services) {
		t.Fatalf("declare the media CDN of every service: %v", serviceMediaCDN)
	}
	ids := map[string]bool{}
	listedBy := map[string][]string{}
	for _, p := range Services {
		if p.ID == "" || strings.Trim(p.ID, "abcdefghijklmnopqrstuvwxyz0123456789") != "" || ids[p.ID] {
			t.Fatalf("bad or duplicate service id %q", p.ID)
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
	// Page AND CDN: even with every service on (the fewest owned domains),
	// each service's probes still cover at least two of its domains: the site
	// and its media/static CDN.
	all := []string{}
	for _, p := range Services {
		all = append(all, p.ID)
	}
	for _, p := range Services {
		owned := ServiceOwnedDomains(p.ID, all)
		covered := map[string]bool{}
		for _, host := range ServiceProbes(p.ID, owned) {
			for _, d := range owned {
				if CoversDomain(d, host) {
					covered[d] = true
				}
			}
		}
		if len(covered) < 2 || !covered[serviceMediaCDN[p.ID]] {
			t.Fatalf("%s: probes cover only %v with every service on (media CDN %s)", p.ID, covered, serviceMediaCDN[p.ID])
		}
	}
	for d, by := range listedBy {
		if len(by) > 1 && !reflect.DeepEqual(by, sharedServiceDomains[d]) {
			t.Fatalf("domain %s listed by %v; shared domains must be declared", d, by)
		}
	}
	for d, by := range sharedServiceDomains {
		if !reflect.DeepEqual(listedBy[d], by) {
			t.Fatalf("declared shared domain %s is listed by %v", d, listedBy[d])
		}
	}
	// Not twitter: xAI's domains stay out of the X service.
	for _, d := range Services[2].Domains {
		if d == "x.ai" || d == "grok.com" {
			t.Fatalf("x service lists %s", d)
		}
	}
}

func TestServiceOwnershipFollowsCatalogOrder(t *testing.T) {
	has := func(list []string, d string) bool {
		for _, v := range list {
			if v == d {
				return true
			}
		}
		return false
	}
	// Instagram alone owns the shared CDN and probes it.
	insta := ServiceOwnedDomains("instagram", []string{"instagram"})
	if !has(insta, "fbcdn.net") || !has(ServiceProbes("instagram", insta), "scontent.xx.fbcdn.net") {
		t.Fatalf("instagram alone: %v", insta)
	}
	// With Facebook on, Facebook owns it whatever the list order, and
	// Instagram neither routes nor probes it.
	for _, enabled := range [][]string{{"facebook", "instagram"}, {"instagram", "facebook"}} {
		insta = ServiceOwnedDomains("instagram", enabled)
		fb := ServiceOwnedDomains("facebook", enabled)
		if has(insta, "fbcdn.net") || !has(fb, "fbcdn.net") {
			t.Fatalf("%v: instagram %v facebook %v", enabled, insta, fb)
		}
		probes := ServiceProbes("instagram", insta)
		if has(probes, "scontent.xx.fbcdn.net") || !reflect.DeepEqual(probes, []string{"www.instagram.com", "static.cdninstagram.com", "scontent.cdninstagram.com"}) {
			t.Fatalf("%v: instagram probes %v", enabled, probes)
		}
	}
	// No two enabled services ever own one domain.
	all := []string{}
	for _, p := range Services {
		all = append(all, p.ID)
	}
	owner := map[string]string{}
	for _, p := range Services {
		for _, d := range ServiceOwnedDomains(p.ID, all) {
			if owner[d] != "" {
				t.Fatalf("%s owned by %s and %s", d, owner[d], p.ID)
			}
			owner[d] = p.ID
		}
	}
	if ServiceOwnedDomains("nope", all) != nil || ServiceProbes("nope", []string{"x.com"}) != nil {
		t.Fatal("unknown service has domains")
	}
}

func TestServiceForDomainAndEntryKey(t *testing.T) {
	cases := map[string]string{"facebook.com": "facebook", "scontent-ams.xx.fbcdn.net": "facebook", "www.instagram.com": "instagram", "pbs.twimg.com": "x", "x.ai": "", "example.org": "", "notfacebook.com": ""}
	for d, want := range cases {
		if got := ServiceForDomain(d); got != want {
			t.Fatalf("ServiceForDomain(%s) = %q, want %q", d, got, want)
		}
	}
	if CustomEntryKey("example.org", "") != "example.org" || CustomService(CustomEntryKey("", "facebook")) != "custom:preset:facebook" {
		t.Fatal("entry key")
	}
	if key, ok := CustomServiceDomain(CustomService(CustomEntryKey("", "x"))); !ok || key != "preset:x" {
		t.Fatalf("service key round trip: %q", key)
	}
}
