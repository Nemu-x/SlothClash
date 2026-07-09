package main

import (
	"errors"
	"net"
	"net/url"
	"strings"
)

// errSubscriptionURLPrivateTarget is returned when an UNTRUSTED subscription URL
// (one that arrived via a `slothclash://` deep link, i.e. potentially triggered
// by a web page the user merely visited) points at a loopback / private /
// link-local address. Fetching those would turn the app into a blind SSRF probe
// for the user's internal network and cloud metadata endpoints.
//
// This restriction deliberately applies ONLY to deep-link-originated URLs:
// a user who types a self-hosted subscription on their LAN (a legitimate and
// common setup) must keep working.
var errSubscriptionURLPrivateTarget = errors.New(
	"refusing to fetch a subscription from a private or loopback address supplied by a link",
)

// errSubscriptionURLScheme is returned for schemes we never fetch.
var errSubscriptionURLScheme = errors.New("unsupported subscription url scheme")

// subscriptionSchemeAllowed reports whether we are willing to fetch this scheme.
// mieru/mierus are handled by their own synthetic path before any HTTP fetch and
// are accepted here for completeness.
func subscriptionSchemeAllowed(scheme string) bool {
	switch strings.ToLower(strings.TrimSpace(scheme)) {
	case "http", "https", "mieru", "mierus":
		return true
	default:
		return false
	}
}

// hostIsPrivateTarget reports whether host (a hostname or literal IP) resolves
// to something we consider internal WITHOUT doing a DNS lookup: a literal
// loopback/private/link-local/unspecified IP, or the localhost name.
//
// We intentionally do not resolve hostnames here: it would add latency to every
// import and still not close DNS-rebinding (the resolution at fetch time can
// differ). Blocking literal internal addresses removes the practical attack
// (`http://169.254.169.254/…`, `http://192.168.1.1/…`) that a hostile page can
// aim at a user via a deep link.
func hostIsPrivateTarget(host string) bool {
	h := strings.ToLower(strings.TrimSpace(host))
	if h == "" {
		return true
	}
	if h == "localhost" || strings.HasSuffix(h, ".localhost") {
		return true
	}
	ip := net.ParseIP(strings.Trim(h, "[]"))
	if ip == nil {
		// A real hostname: allowed (resolution happens at fetch time).
		return false
	}
	return ip.IsLoopback() ||
		ip.IsPrivate() ||
		ip.IsLinkLocalUnicast() ||
		ip.IsLinkLocalMulticast() ||
		ip.IsUnspecified() ||
		ip.IsInterfaceLocalMulticast()
}

// validateUntrustedSubscriptionURL guards a normalized subscription URL that came
// from an untrusted source (deep link). Returns nil when the URL is safe to fetch.
func validateUntrustedSubscriptionURL(norm string) error {
	u, err := url.Parse(strings.TrimSpace(norm))
	if err != nil || u.Host == "" {
		return errors.New("invalid subscription url")
	}
	if !subscriptionSchemeAllowed(u.Scheme) {
		return errSubscriptionURLScheme
	}
	if hostIsPrivateTarget(u.Hostname()) {
		return errSubscriptionURLPrivateTarget
	}
	return nil
}
