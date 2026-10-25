package main

import (
	"context"
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
// rather than on what the provider answered. Background refreshes race the
// core coming up (routes and TUN change under them), so these are not
// recorded from that path: they would badge a healthy subscription.
func isTransientFetchError(err error) bool {
	if err == nil {
		return false
	}
	var urlErr *url.Error
	var netErr net.Error
	return errors.As(err, &urlErr) || errors.As(err, &netErr) ||
		errors.Is(err, context.DeadlineExceeded) || errors.Is(err, context.Canceled) ||
		errors.Is(err, io.ErrUnexpectedEOF)
}

// applyProfileUpdateOutcomeLocked records one refresh attempt on p: success
// moves LastUpdated, failure leaves it alone and stores the reason. Caller
// holds a.mu for writing.
func applyProfileUpdateOutcomeLocked(p *Profile, now int64, err error) {
	p.LastUpdateAttempt = now
	if err != nil {
		p.LastUpdateError = profileUpdateErrorText(err)
		return
	}
	p.LastUpdated = now
	p.LastUpdateError = ""
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
}

// profileRefreshAnchor is the moment the auto-update schedule counts the
// interval from: the later of the last success and the last attempt, so a
// failing profile is retried at its normal interval rather than on every tick.
// Profiles saved before LastUpdateAttempt existed schedule exactly as before.
func profileRefreshAnchor(p Profile) int64 {
	if p.LastUpdateAttempt > p.LastUpdated {
		return p.LastUpdateAttempt
	}
	return p.LastUpdated
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
			changed = true
		} else if p.LastUpdateError != "" {
			p.LastUpdateError = ""
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
