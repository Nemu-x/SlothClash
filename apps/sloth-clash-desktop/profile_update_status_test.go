package main

import (
	"errors"
	"fmt"
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"SlothClashDesktop/internal/runtimecfg"
	"SlothClashDesktop/internal/runtimecfg/runtimecfgtest"
)

// A successful refresh moves LastUpdated, records the attempt and clears an
// error left by an earlier failure.
func TestRefreshProfileSubscriptionSuccessClearsUpdateError(t *testing.T) {
	isolateAppDataRoot(t)
	small := runtimecfgtest.RealisticSubscriptionYAML(5)
	var body atomic.Pointer[[]byte]
	body.Store(&small)
	subURL := runtimecfgtest.ServeSubscription(t, &body)

	const id = "profile-update-ok"
	a := &App{profiles: []Profile{{
		ID: id, Name: "ok", Type: "subscription", URL: subURL,
		LastUpdated: 1000, LastUpdateAttempt: 2000, LastUpdateError: "subscription HTTP 403: forbidden",
	}}}
	before := time.Now().Unix()
	state, err := a.refreshProfileSubscription(id, false)
	if err != nil {
		t.Fatalf("refresh: %v", err)
	}
	got := state.Profile.Profiles[0]
	if got.LastUpdateError != "" {
		t.Fatalf("LastUpdateError = %q, want cleared", got.LastUpdateError)
	}
	if got.LastUpdated < before || got.LastUpdateAttempt < before {
		t.Fatalf("LastUpdated=%d LastUpdateAttempt=%d, want >= %d", got.LastUpdated, got.LastUpdateAttempt, before)
	}
}

// A body failure other than an oversized body is tolerated by the explicit
// refresh (nothing returned, as before) but recorded on the profile, and it
// does not move LastUpdated.
func TestRefreshProfileSubscriptionRecordsToleratedBodyFailure(t *testing.T) {
	isolateAppDataRoot(t)
	empty := []byte("")
	var body atomic.Pointer[[]byte]
	body.Store(&empty) // peek succeeds on 200; the body fetch refuses an empty body
	subURL := runtimecfgtest.ServeSubscription(t, &body)

	const id = "profile-update-empty"
	a := &App{profiles: []Profile{{ID: id, Name: "empty", Type: "subscription", URL: subURL, LastUpdated: 1000}}}
	state, err := a.refreshProfileSubscription(id, false)
	if err != nil {
		t.Fatalf("refresh returned %v; non-oversize body failures must stay tolerated", err)
	}
	got := state.Profile.Profiles[0]
	if got.LastUpdated != 1000 {
		t.Fatalf("LastUpdated = %d, want 1000 (no body was fetched)", got.LastUpdated)
	}
	if got.LastUpdateAttempt <= 1000 || got.LastUpdateError != "empty subscription body" {
		t.Fatalf("attempt=%d error=%q", got.LastUpdateAttempt, got.LastUpdateError)
	}
}

// A provider that answers 403 fails the peek: returned as before, and now also
// recorded, so the scheduler stops retrying it every minute.
func TestRefreshProfileSubscriptionRecordsHTTPFailure(t *testing.T) {
	isolateAppDataRoot(t)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		http.Error(w, "forbidden", http.StatusForbidden)
	}))
	t.Cleanup(srv.Close)

	const id = "profile-update-403"
	a := &App{profiles: []Profile{{ID: id, Name: "403", Type: "subscription", URL: srv.URL + "/sub/tok", LastUpdated: 1000}}}
	state, err := a.refreshProfileSubscription(id, false)
	if err == nil {
		t.Fatal("refresh of a 403 subscription returned no error")
	}
	got := state.Profile.Profiles[0]
	if got.LastUpdated != 1000 || got.LastUpdateAttempt <= 1000 || !strings.Contains(got.LastUpdateError, "403") {
		t.Fatalf("updated=%d attempt=%d error=%q", got.LastUpdated, got.LastUpdateAttempt, got.LastUpdateError)
	}
	if ids := a.dueProfileIDs(got.LastUpdateAttempt + 60); len(ids) != 0 {
		t.Fatalf("a just-failed profile is due again a minute later: %v", ids)
	}
}

// The stored error never carries the subscription URL's secret parts: the
// token in the path, credential query values, userinfo.
func TestRefreshProfileSubscriptionErrorIsRedacted(t *testing.T) {
	isolateAppDataRoot(t)
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	addr := ln.Addr().String()
	_ = ln.Close() // nothing listens: the fetch fails with Go's url.Error quoting the URL

	const secret = "s3cr3tPathToken"
	subURL := fmt.Sprintf("http://user:pw@%s/sub/%s?token=qtok", addr, secret)
	const id = "profile-update-redact"
	a := &App{profiles: []Profile{{ID: id, Name: "r", Type: "subscription", URL: subURL}}}
	state, _ := a.refreshProfileSubscription(id, false)
	msg := state.Profile.Profiles[0].LastUpdateError
	if msg == "" {
		t.Fatal("no error recorded")
	}
	for _, leak := range []string{secret, "qtok", "user:pw", "pw@"} {
		if strings.Contains(msg, leak) {
			t.Fatalf("error leaks %q: %s", leak, msg)
		}
	}
	if !strings.Contains(msg, addr) {
		t.Fatalf("error lost the host, which is the useful part: %s", msg)
	}
}

func TestProfileUpdateErrorText(t *testing.T) {
	cases := []struct {
		name    string
		err     error
		want    string
		notWant []string
	}{
		{
			name:    "url error",
			err:     &url.Error{Op: "Get", URL: "https://a:b@sub.example.com/api/v1/client/subscribe/TOKEN123?flag=clash&token=Q", Err: errors.New("dial tcp: i/o timeout")},
			want:    `Get "https://sub.example.com/…": dial tcp: i/o timeout`,
			notWant: []string{"TOKEN123", "a:b", "flag=clash"},
		},
		{
			name: "host only url is kept",
			err:  errors.New("redirect to https://sub.example.com failed"),
			want: "redirect to https://sub.example.com failed",
		},
		{
			name: "http body is collapsed",
			err:  errors.New("subscription HTTP 404: <html>\n  <body>not   found</body>\n</html>"),
			want: "subscription HTTP 404: <html> <body>not found</body> </html>",
		},
		{
			name:    "age key",
			err:     errors.New("invalid AGE secret key on this profile: AGE-SECRET-KEY-1QQQQQQQQQQQQ"),
			want:    "invalid AGE secret key on this profile: [redacted]",
			notWant: []string{"AGE-SECRET-KEY-1Q"},
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := profileUpdateErrorText(tc.err)
			if got != tc.want {
				t.Fatalf("got  %q\nwant %q", got, tc.want)
			}
			for _, nw := range tc.notWant {
				if strings.Contains(got, nw) {
					t.Fatalf("%q leaks %q", got, nw)
				}
			}
		})
	}
	long := profileUpdateErrorText(errors.New(strings.Repeat("x", 5000)))
	if r := []rune(long); len(r) != maxProfileUpdateErrorRunes+1 {
		t.Fatalf("long error is %d runes, want %d", len(r), maxProfileUpdateErrorRunes+1)
	}
}

// The schedule counts from the later of the last success and the last
// attempt; a profile without an attempt (saved before the field existed)
// schedules exactly as before.
func TestDueProfileIDsCountsFromLastAttempt(t *testing.T) {
	const interval = 60 // minutes
	const t0 = int64(1_000_000)
	a := &App{profiles: []Profile{
		{ID: "legacy", URL: "https://x/a", AutoUpdateEnabled: true, AutoUpdateIntervalMinutes: interval, LastUpdated: t0},
		{ID: "failing", URL: "https://x/b", AutoUpdateEnabled: true, AutoUpdateIntervalMinutes: interval, LastUpdated: t0, LastUpdateAttempt: t0 + 1800, LastUpdateError: "boom"},
		{ID: "never", URL: "https://x/c", AutoUpdateEnabled: true, AutoUpdateIntervalMinutes: interval},
		{ID: "local", AutoUpdateEnabled: true, AutoUpdateIntervalMinutes: interval},
	}}
	check := func(now int64, want ...string) {
		t.Helper()
		got := a.dueProfileIDs(now)
		if strings.Join(got, ",") != strings.Join(want, ",") {
			t.Fatalf("due at t0+%d = %v, want %v", now-t0, got, want)
		}
	}
	check(t0+3599, "never")
	check(t0+3600, "legacy", "never")                 // legacy: unchanged, due interval after LastUpdated
	check(t0+1800+3599, "legacy", "never")            // failing: not yet, counted from its attempt
	check(t0+1800+3600, "legacy", "failing", "never") // failing: due one interval after the attempt
}

// The pipeline's own fetches (first-Connect cache miss, background refresh)
// record deterministic failures, ignore transient network ones, and clear the
// error on success without moving the schedule anchors.
func TestRecordPipelineBodyFetch(t *testing.T) {
	isolateAppDataRoot(t)
	root, err := slothDataRoot()
	if err != nil {
		t.Fatal(err)
	}
	const id = "profile-pipeline"
	dataDir := filepath.Join(root, "runtime", id)
	a := &App{profiles: []Profile{
		{ID: "other", URL: "https://x/o"},
		{ID: id, URL: "https://x/p", LastUpdated: 1000},
	}}
	p := func() Profile { return a.profiles[1] }

	a.recordPipelineBodyFetch(dataDir, &url.Error{Op: "Get", URL: "https://x/p", Err: errors.New("connection refused")})
	if p().LastUpdateError != "" || p().LastUpdateAttempt != 0 {
		t.Fatalf("transient failure recorded: %+v", p())
	}

	a.recordPipelineBodyFetch(dataDir, errors.New("subscription HTTP 404: gone"))
	if p().LastUpdateError != "subscription HTTP 404: gone" || p().LastUpdateAttempt == 0 || p().LastUpdated != 1000 {
		t.Fatalf("deterministic failure: %+v", p())
	}
	if a.profiles[0].LastUpdateError != "" {
		t.Fatal("recorded on the wrong profile")
	}

	attempt := p().LastUpdateAttempt
	a.recordPipelineBodyFetch(dataDir, nil)
	if p().LastUpdateError != "" || p().LastUpdated != 1000 || p().LastUpdateAttempt != attempt {
		t.Fatalf("success: %+v", p())
	}
}

// The hook is wired: a body fetch made by runtimecfg reaches the registered
// App through the subscription client main builds.
func TestPipelineBodyFetchHookIsWired(t *testing.T) {
	isolateAppDataRoot(t)
	root, err := slothDataRoot()
	if err != nil {
		t.Fatal(err)
	}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		http.Error(w, "nope", http.StatusNotFound)
	}))
	t.Cleanup(srv.Close)
	const id = "profile-hook"
	a := &App{profiles: []Profile{{ID: id, URL: srv.URL + "/sub"}}}
	prev := bodyFetchRecorder.Swap(a)
	t.Cleanup(func() { bodyFetchRecorder.Store(prev) })

	dataDir := filepath.Join(root, "runtime", id)
	if err := runtimecfg.RunSubscriptionFetchOnce(t.Context(), dataDir, srv.URL+"/sub", "", currentSubscriptionClient()); err == nil {
		t.Fatal("fetch of a 404 subscription succeeded")
	}
	if got := a.profiles[0].LastUpdateError; !strings.Contains(got, "404") {
		t.Fatalf("LastUpdateError = %q, want the 404", got)
	}
}

// Changing the subscription URL or the age secret key clears the stored
// update status (the failure described the old source); saving the same value
// again, which the edit dialog does on every save, keeps it. Neither edit
// counts as a body refresh: the edit moves LastUpdated like any other edit,
// and nothing claims a successful attempt.
func TestProfileSourceEditResetsUpdateStatus(t *testing.T) {
	isolateAppDataRoot(t)
	_, key1, err := runtimecfg.GenerateAgeKeyPair("x25519")
	if err != nil {
		t.Fatal(err)
	}
	_, key2, err := runtimecfg.GenerateAgeKeyPair("x25519")
	if err != nil {
		t.Fatal(err)
	}
	const id = "profile-edit"
	const oldURL = "https://sub.example.com/old"
	failed := func() *App {
		return &App{profiles: []Profile{{
			ID: id, Name: "n", Type: "subscription", URL: oldURL, AgeSecretKey: key1,
			LastUpdated: 1000, LastUpdateAttempt: 2000, LastUpdateError: "subscription HTTP 403: forbidden",
		}}}
	}
	kept := func(t *testing.T, p Profile) {
		t.Helper()
		if p.LastUpdateError == "" || p.LastUpdateAttempt != 2000 {
			t.Fatalf("unchanged source cleared the status: %+v", p)
		}
	}
	cleared := func(t *testing.T, p Profile) {
		t.Helper()
		if p.LastUpdateError != "" || p.LastUpdateAttempt != 0 {
			t.Fatalf("changed source kept the status: attempt=%d error=%q", p.LastUpdateAttempt, p.LastUpdateError)
		}
		if p.LastUpdated <= 1000 {
			t.Fatalf("LastUpdated = %d, an edit moves it", p.LastUpdated)
		}
	}
	cases := []struct {
		name  string
		edit  func(a *App) (AppState, error)
		check func(*testing.T, Profile)
	}{
		{"same url", func(a *App) (AppState, error) { return a.UpdateProfileInfo(id, "renamed", oldURL) }, kept},
		{"empty url keeps the old one", func(a *App) (AppState, error) { return a.UpdateProfileInfo(id, "renamed", "") }, kept},
		{"new url", func(a *App) (AppState, error) { return a.UpdateProfileInfo(id, "n", "https://sub.example.com/new") }, cleared},
		{"same key", func(a *App) (AppState, error) { return a.SetProfileAgeSecretKey(id, key1) }, kept},
		{"new key", func(a *App) (AppState, error) { return a.SetProfileAgeSecretKey(id, key2) }, cleared},
		{"key removed", func(a *App) (AppState, error) { return a.SetProfileAgeSecretKey(id, "") }, cleared},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			st, err := tc.edit(failed())
			if err != nil {
				t.Fatal(err)
			}
			tc.check(t, st.Profile.Profiles[0])
		})
	}
}
