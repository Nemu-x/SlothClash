package main

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"errors"
	"io"
	"net"
	"net/url"
	"path/filepath"
	"regexp"
	"strings"
	"sync/atomic"
	"time"

	"SlothClashDesktop/internal/runtimecfg"
)

// Subscription update status: Profile.LastUpdateAttempt / LastUpdateError
// record how the last subscription refresh went, so a refresh that keeps
// failing (oversized body, wrong age key, HTTP 403/404, provider down) is
// visible on the profile card instead of the profile silently sitting on its
// last-known-good body. See the Profile struct for the field semantics.

// maxProfileUpdateErrorRunes bounds the stored error: an HTTP error carries up
// to 4 KiB of the provider's response body, which is noise on a badge.
const maxProfileUpdateErrorRunes = 300

var (
	// urlInTextRe matches a URL inside an error message (Go's url.Error quotes
	// the full request URL, redirect targets included).
	urlInTextRe = regexp.MustCompile(`(?i)\b[a-z][a-z0-9+.-]*://[^\s"'<>]+`)
	// ageSecretKeyRe matches an age identity, in case a decrypt error echoes one.
	ageSecretKeyRe = regexp.MustCompile(`(?i)AGE-SECRET-KEY-1[0-9A-Z]+`)
)

// profileUpdateErrorText turns a subscription refresh failure into the text
// stored on the profile and shown in the UI. Subscription URLs are bearer
// secrets (the token usually sits in the path, not in a query parameter), so
// every URL is cut down to scheme://host; credential query values and age
// identities are redacted on top, whitespace collapsed and length bounded.
func profileUpdateErrorText(err error) string {
	if err == nil {
		return ""
	}
	msg := runtimecfg.RedactCredentialQuery(err.Error())
	msg = urlInTextRe.ReplaceAllStringFunc(msg, redactURLToHost)
	msg = ageSecretKeyRe.ReplaceAllString(msg, "[redacted]")
	msg = strings.Join(strings.Fields(msg), " ")
	if r := []rune(msg); len(r) > maxProfileUpdateErrorRunes {
		msg = string(r[:maxProfileUpdateErrorRunes]) + "…"
	}
	if msg == "" {
		msg = "subscription update failed"
	}
	return msg
}

// redactURLToHost keeps only scheme://host of raw (no userinfo, path, query
// or fragment), marking what was cut with "/…".
func redactURLToHost(raw string) string {
	u, err := url.Parse(raw)
	if err != nil || u.Host == "" {
		if i := strings.Index(raw, "://"); i >= 0 {
			return raw[:i+3] + "…"
		}
		return "…"
	}
	out := u.Scheme + "://" + u.Host
	if u.Host != raw[len(u.Scheme)+3:] {
		out += "/…"
	}
	return out
}

// isTransientFetchError reports whether a body fetch failed on the network
// (DNS, dial, timeout, connection reset, cut-off body, cancelled context)
// rather than on what the provider answered. Background refreshes race the
// core coming up (routes and TUN change under them), so these are not
// recorded from that path: they would badge a healthy subscription.
// http.Client wraps every transport failure in a *url.Error, including the
// deterministic ones (isDeterministicTransportError), which are not transient.
func isTransientFetchError(err error) bool {
	if err == nil || isDeterministicTransportError(err) {
		return false
	}
	var urlErr *url.Error
	var netErr net.Error
	return errors.As(err, &urlErr) || errors.As(err, &netErr) ||
		errors.Is(err, context.DeadlineExceeded) || errors.Is(err, context.Canceled) ||
		errors.Is(err, io.ErrUnexpectedEOF)
}

// goRedirectLimitRe matches net/http's own redirect limit error, returned when
// a client has no CheckRedirect of its own.
var goRedirectLimitRe = regexp.MustCompile(`^stopped after \d+ redirects$`)

// isDeterministicTransportError reports a transport-level failure that a
// retry cannot fix: the server's certificate does not verify (unknown
// authority, wrong host name, expired or otherwise invalid, no usable roots)
// or the request is caught in a redirect loop. Retrying these sooner only
// repeats them.
func isDeterministicTransportError(err error) bool {
	var (
		certVerify   *tls.CertificateVerificationError
		unknownCA    x509.UnknownAuthorityError
		hostname     x509.HostnameError
		invalidCert  x509.CertificateInvalidError
		systemRoots  x509.SystemRootsError
		constraint   x509.ConstraintViolationError
		critical     x509.UnhandledCriticalExtension
		insecureAlgo x509.InsecureAlgorithmError
	)
	switch {
	case errors.As(err, &certVerify), errors.As(err, &unknownCA), errors.As(err, &hostname),
		errors.As(err, &invalidCert), errors.As(err, &systemRoots), errors.As(err, &constraint),
		errors.As(err, &critical), errors.As(err, &insecureAlgo):
		return true
	case errors.Is(err, runtimecfg.ErrTooManyRedirects):
		return true
	}
	var urlErr *url.Error
	return errors.As(err, &urlErr) && urlErr.Err != nil && goRedirectLimitRe.MatchString(urlErr.Err.Error())
}

// applyProfileUpdateOutcomeLocked records one refresh attempt on p: success
// moves LastUpdated, failure leaves it alone and stores the reason. Caller
// holds a.mu for writing.
func applyProfileUpdateOutcomeLocked(p *Profile, now int64, err error) {
	p.LastUpdateAttempt = now
	if err != nil {
		p.LastUpdateError = profileUpdateErrorText(err)
		p.LastUpdateErrorTransient = isTransientFetchError(err)
		return
	}
	p.LastUpdated = now
	p.LastUpdateError = ""
	p.LastUpdateErrorTransient = false
}

// resetProfileUpdateStatusLocked forgets the last refresh attempt and its
// failure. Called when the subscription URL or the age secret key actually
// changes: the stored failure described the old source, so the badge must not
// outlive the user's fix. It is not a refresh: nothing was fetched, the cached
// body is still the old one, and LastUpdated is left to the caller (an edit
// moves it like any other edit, see the Profile struct). The next attempt
// records a fresh outcome. Caller holds a.mu for writing.
func resetProfileUpdateStatusLocked(p *Profile) {
	p.LastUpdateAttempt = 0
	p.LastUpdateError = ""
	p.LastUpdateErrorTransient = false
}

// profileTransientRetryMinutes is how soon a profile whose last refresh failed
// on the network is retried (bounded by its own interval, see
// profileRefreshDelaySeconds). Long enough not to hammer a provider that is
// down, short enough that a refresh lost to a resume before the network came
// up is not postponed by the whole interval.
const profileTransientRetryMinutes = 15

// profileRefreshAnchor is the moment the auto-update schedule counts the
// interval from: the later of the last success and the last attempt, so a
// failing profile is retried at its normal interval (profileRefreshDelaySeconds
// shortens it after a transient failure) rather than on every tick.
// Profiles saved before LastUpdateAttempt existed schedule exactly as before.
func profileRefreshAnchor(p Profile) int64 {
	if p.LastUpdateAttempt > p.LastUpdated {
		return p.LastUpdateAttempt
	}
	return p.LastUpdated
}

// profileRefreshDelaySeconds is how long after profileRefreshAnchor the
// profile is due: its interval, or min(interval, profileTransientRetryMinutes)
// while the last attempt failed transiently. intervalMinutes is > 0.
func profileRefreshDelaySeconds(p Profile, intervalMinutes int) int64 {
	if p.LastUpdateErrorTransient && p.LastUpdateError != "" {
		intervalMinutes = min(intervalMinutes, profileTransientRetryMinutes)
	}
	return int64(intervalMinutes) * 60
}

// bodyFetchRecorder is the App that receives the generation pipeline's own
// body fetch outcomes (runtimecfg.SubscriptionClient.OnBodyFetch). Set in
// startup; nil (tests, before startup) drops them.
var bodyFetchRecorder atomic.Pointer[App]

// recordPipelineBodyFetch is the OnBodyFetch hook of every subscription
// client main builds.
func recordPipelineBodyFetch(dataDir string, err error) {
	if a := bodyFetchRecorder.Load(); a != nil {
		a.recordPipelineBodyFetch(dataDir, err)
	}
}

// recordPipelineBodyFetch records a body fetch the pipeline made on its own
// (first-Connect cache miss, background refresh after a cache hit). Success
// clears a stored error but leaves LastUpdated/LastUpdateAttempt alone:
// LastUpdated anchors the scheduled full refresh, which also re-reads the
// provider's userinfo metadata, and Connect must not postpone it. A
// deterministic failure is recorded like a scheduled attempt; a transient
// network failure is not recorded at all (see isTransientFetchError).
func (a *App) recordPipelineBodyFetch(dataDir string, err error) {
	if err != nil && isTransientFetchError(err) {
		return
	}
	root, rerr := slothDataRoot()
	if rerr != nil {
		return
	}
	dataDir = filepath.Clean(dataDir)
	now := time.Now().Unix()
	a.mu.Lock()
	changed := false
	for i := range a.profiles {
		p := &a.profiles[i]
		if strings.TrimSpace(p.URL) == "" || filepath.Join(root, "runtime", p.ID) != dataDir {
			continue
		}
		if err != nil {
			p.LastUpdateAttempt = now
			p.LastUpdateError = profileUpdateErrorText(err)
			p.LastUpdateErrorTransient = false // transient ones returned above
			changed = true
		} else if p.LastUpdateError != "" {
			p.LastUpdateError = ""
			p.LastUpdateErrorTransient = false
			changed = true
		}
		break
	}
	if !changed {
		a.mu.Unlock()
		return
	}
	a.state.Profile.Profiles = a.profiles
	a.state.UpdatedAt = now
	if perr := a.persistProfilesLocked(); perr != nil {
		debugLog("profiles", "PU-1", "profile_update_status.go:recordPipelineBodyFetch",
			"failed to persist subscription update status", map[string]any{"error": perr.Error()})
	}
	a.mu.Unlock()
	a.emitAppStateChanged()
}
