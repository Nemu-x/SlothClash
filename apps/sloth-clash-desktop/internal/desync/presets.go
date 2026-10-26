package desync

import "strings"

// Preset is a curated custom-domain entry for a service whose pages and media
// live on several registrable domains (a plain entry covers one domain and
// its subdomains: "facebook.com" loads the page but not the images, which
// come from fbcdn.net). A preset entry stores only its ID; the domains and
// probes are read from this catalog when the config is generated and when the
// entry is calibrated, so catalog updates reach existing entries.
type Preset struct {
	ID   string
	Name string
	// Domains are routed through the entry's group (each with its
	// subdomains). Only domains the service itself owns: no shared clouds or
	// CDN catch-alls, whose other customers must not follow this entry.
	Domains []string
	// Probes are the hosts the calibration checks over HTTPS. Every one must
	// pass (page AND CDN): a strategy that opens the page but not its media is
	// exactly the half-working state a preset exists to avoid. Each is a
	// concrete host that serves HTTPS with a certificate valid for that exact
	// name (a bare CDN apex such as cdninstagram.com or twimg.com has none and
	// would fail every probe). The reachable probe accepts any HTTP status, so
	// a 204/302/400/404 at "/" is fine. Measured 2026-10-26 with
	// curl -I https://<host>/ (certificate verified for the host).
	Probes []string
}

// Presets is the catalog, in precedence order: a domain listed by two presets
// (see PresetOwnedDomains) belongs to the first enabled one.
var Presets = []Preset{
	{
		ID:   "facebook",
		Name: "Facebook",
		Domains: []string{
			"facebook.com",  // site, API (graph., edge-chat.), m./web./mbasic.
			"fbcdn.net",     // static.xx (JS/CSS) and scontent-* (photos, video)
			"facebook.net",  // connect. (SDK) and other first-party script hosts
			"fbsbx.com",     // lookaside. (attachments, link previews, uploads)
			"fb.com",        // short and corporate links into the site
			"fb.me",         // short links
			"fb.watch",      // video short links
			"messenger.com", // Messenger web client
			"m.me",          // Messenger short links
		},
		// www.facebook.com: 200. static.xx.fbcdn.net: 302 (scripts and
		// styles). scontent.xx.fbcdn.net: 204 (the media CDN; without it the
		// page loads with no pictures).
		Probes: []string{"www.facebook.com", "static.xx.fbcdn.net", "scontent.xx.fbcdn.net"},
	},
	{
		ID:   "instagram",
		Name: "Instagram",
		Domains: []string{
			"instagram.com",    // site and API (i., graph., www.)
			"cdninstagram.com", // static. (JS/CSS) and scontent-* (media)
			"ig.me",            // short links (profiles, Direct)
			"instagr.am",       // legacy short links
			// Shared with Facebook: much of Instagram's media is served from
			// instagram.*.fna.fbcdn.net. Without it an Instagram-only entry
			// shows the feed with holes. When Facebook is on too, Facebook's
			// group carries it (catalog order).
			"fbcdn.net",
		},
		// www.instagram.com: 200. static.cdninstagram.com: 404 (scripts).
		// scontent.cdninstagram.com: 204 (media). scontent.xx.fbcdn.net is
		// checked only while this entry owns fbcdn.net.
		Probes: []string{"www.instagram.com", "static.cdninstagram.com", "scontent.cdninstagram.com", "scontent.xx.fbcdn.net"},
	},
	{
		ID:   "x",
		Name: "X (Twitter)",
		Domains: []string{
			"x.com",       // site and API (api.x.com)
			"twitter.com", // legacy site and API (api.twitter.com)
			"twimg.com",   // abs. (JS/CSS), pbs. (images), video., ton. (DM media)
			"t.co",        // link shortener every outbound link passes
			"pscp.tv",     // Live and Spaces video/audio streams
			// Not included: x.ai and grok.com (xAI, a different service).
		},
		// x.com: 200. abs.twimg.com and pbs.twimg.com: 400 at "/" (no
		// object), with a valid certificate: scripts and images.
		Probes: []string{"x.com", "abs.twimg.com", "pbs.twimg.com"},
	},
}

// PresetByID returns the catalog preset with the given ID.
func PresetByID(id string) (Preset, bool) {
	for _, p := range Presets {
		if p.ID == id {
			return p, true
		}
	}
	return Preset{}, false
}

// CoversDomain reports whether domain equals, or is a subdomain of, base.
func CoversDomain(base, domain string) bool {
	return domain == base || strings.HasSuffix(domain, "."+base)
}

// DomainsOverlap reports whether one of the two domains covers the other: two
// entries for them would route the same names.
func DomainsOverlap(a, b string) bool { return CoversDomain(a, b) || CoversDomain(b, a) }

// PresetForDomain returns the ID of the first catalog preset with a domain
// overlapping domain, or "".
func PresetForDomain(domain string) string {
	for _, p := range Presets {
		for _, d := range p.Domains {
			if DomainsOverlap(d, domain) {
				return p.ID
			}
		}
	}
	return ""
}

// PresetOwnedDomains returns the domains preset id routes while the presets
// in enabled are on (id itself is assumed on): its catalog domains minus
// those an enabled preset earlier in the catalog also lists. A domain belongs
// to at most one group, and which one does not depend on the user's list
// order. Unknown IDs have no domains.
func PresetOwnedDomains(id string, enabled []string) []string {
	on := map[string]bool{id: true}
	for _, e := range enabled {
		on[e] = true
	}
	taken := map[string]bool{}
	for _, p := range Presets {
		if p.ID == id {
			var out []string
			for _, d := range p.Domains {
				if !taken[d] {
					out = append(out, d)
				}
			}
			return out
		}
		if on[p.ID] {
			for _, d := range p.Domains {
				taken[d] = true
			}
		}
	}
	return nil
}

// PresetProbes returns the probe hosts of preset id that lie under one of
// owned (its PresetOwnedDomains): a probe of a domain another group carries
// says nothing about this group's strategy.
func PresetProbes(id string, owned []string) []string {
	p, ok := PresetByID(id)
	if !ok {
		return nil
	}
	var out []string
	for _, host := range p.Probes {
		for _, d := range owned {
			if CoversDomain(d, host) {
				out = append(out, host)
				break
			}
		}
	}
	return out
}

// CustomEntryKey is the cache key of a custom entry within CacheEntry.Custom:
// the domain of a plain entry, "preset:<id>" of a preset one (a domain never
// contains ':'). CustomService(CustomEntryKey(...)) is its service key.
func CustomEntryKey(domain, preset string) string {
	if preset != "" {
		return "preset:" + preset
	}
	return domain
}
