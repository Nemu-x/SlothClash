package runtimecfg

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"SlothClashDesktop/internal/boundedio"
	"SlothClashDesktop/internal/fsutil"
)

// ---------------------------------------------------------------------------
// HTTP fetch + cache + tryWriteMergedFullProfile.
//
// Hot path summary:
//
//   tryWriteMergedFullProfile (called from Write)
//     ├── ReadSubscriptionBodyCache (cache hit → use immediately + bg refresh)
//     │       └── kickBackgroundSubscriptionRefresh
//     │           └── inflightSubscriptionFetch (singleflight dedup)
//     │               └── refreshSubscriptionBodyCache
//     │                       └── FetchSubscriptionBody (50 s timeout)
//     └── on cache miss: blocking FetchSubscriptionBody (55 s)
// ---------------------------------------------------------------------------

// Subscription size caps. A truncated body is worse than none: YAML cut at a
// node boundary still parses and silently drops nodes, groups and rules, so an
// oversized body is refused with an explicit error instead.
//
// Measured with realisticSubscriptionYAML (subscription_fixture_test.go): block
// YAML with mixed vless-reality / vmess-ws / ss2022 / hysteria2 nodes, five
// groups listing every node, per-country groups and 3000 inline rules costs
// ~825 B per node; 10k nodes = 7.9 MiB, which the former 6 MiB cap truncated.
// 32 MiB leaves 4x headroom (~40k nodes) while still bounding memory.
//
// The wire cap covers an age-armored body whose plaintext is at the content
// cap: base64 (4/3), armor line breaks (65/64) and per-chunk tags measure
// 1.355x (TestFetchAgeSubscriptionAtAndOverLimit), so 32 MiB of YAML arrives
// as 43.3 MiB; 48 MiB covers it.
const (
	subscriptionMaxBodyBytes = 32 << 20
	subscriptionMaxWireBytes = 48 << 20
)

// FetchSubscriptionBody returns the (decrypted) subscription body plus the
// response headers of the successful GET — the same response carries the
// optional brand manifest (main's branding.go), so callers can capture it
// without a second billed request. Header is nil for non-HTTP sources (mieru).
// The request identifies itself as client.
func FetchSubscriptionBody(ctx context.Context, rawURL string, ageKey string, client SubscriptionClient) ([]byte, http.Header, error) {
	norm, err := NormalizeSubscriptionURL(rawURL)
	if err != nil {
		return nil, nil, err
	}
	if IsMieruSubscriptionURL(norm) {
		b, err := buildMieruSubscriptionYAML(norm)
		return b, nil, err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, norm, nil)
	if err != nil {
		return nil, nil, err
	}
	req.Header.Set("User-Agent", client.UserAgent)
	ApplyIdentityHeaders(req, client)

	httpClient := &http.Client{
		Timeout: 50 * time.Second,
		CheckRedirect: func(_ *http.Request, via []*http.Request) error {
			if len(via) >= 12 {
				return errors.New("too many redirects")
			}
			return nil
		},
	}

	resp, err := httpClient.Do(req)
	if err != nil {
		return nil, nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		b, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
		return nil, nil, fmt.Errorf("subscription HTTP %d: %s", resp.StatusCode, strings.TrimSpace(string(b)))
	}
	body, err := boundedio.Read(resp.Body, "subscription", subscriptionMaxWireBytes)
	if err != nil {
		return nil, nil, err
	}
	// Providers may ship the body age-encrypted; decrypt BEFORE caching/parsing
	// so every downstream consumer keeps seeing plain YAML.
	body, err = decryptSubscriptionBodyIfAge(body, ageKey)
	if err != nil {
		return nil, nil, err
	}
	// The wire cap leaves room for age armor; the content cap applies to what
	// the pipeline actually parses, encrypted or not.
	if int64(len(body)) > subscriptionMaxBodyBytes {
		return nil, nil, &boundedio.LimitError{What: "subscription", Limit: subscriptionMaxBodyBytes}
	}
	return body, resp.Header, nil
}

// SubscriptionBodyCachePath returns the on-disk location where we persist the
// last-known-good subscription body bytes for a given runtime dataDir. The
// cache is the PRIMARY source on every Connect — the HTTP fetch runs in the
// background to refresh the cache for the NEXT connect, so Connect itself is
// never blocked on a slow / flaky subscription provider (previously a single
// slow origin could add 5–50 s of latency to Connect). This mirrors
// clash-verge-rev's behaviour: the runtime uses the last profile file on disk
// and subscription updates are a separate scheduled / manual operation.
func SubscriptionBodyCachePath(dataDir string) string {
	return filepath.Join(dataDir, "subscription.cache.yaml")
}

// WriteSubscriptionBodyCache stores body as dataDir's last-known-good
// subscription body (no-op for an empty body or dataDir).
func WriteSubscriptionBodyCache(dataDir string, body []byte) {
	if dataDir == "" || len(body) == 0 {
		return
	}
	_ = os.MkdirAll(dataDir, 0o755)
	_ = fsutil.AtomicWriteFile(SubscriptionBodyCachePath(dataDir), body, 0o600)
}

// ReadSubscriptionBodyCache returns dataDir's cached subscription body, nil
// when there is none.
func ReadSubscriptionBodyCache(dataDir string) []byte {
	if dataDir == "" {
		return nil
	}
	b, err := os.ReadFile(SubscriptionBodyCachePath(dataDir))
	if err != nil {
		return nil
	}
	return b
}

// refreshSubscriptionBodyCache synchronously fetches the subscription body and
// writes it to the on-disk cache. Called from RefreshProfileSubscription and
// the background auto-update loop so the next Connect for that profile uses
// the freshly fetched body (cache-first semantics). On fetch failure it
// leaves the existing cache untouched — a transient flap must never wipe the
// last-known-good body.
func refreshSubscriptionBodyCache(ctx context.Context, dataDir, subURL, ageKey string, client SubscriptionClient) error {
	if strings.TrimSpace(dataDir) == "" || strings.TrimSpace(subURL) == "" {
		return nil
	}
	body, hdr, err := FetchSubscriptionBody(ctx, subURL, ageKey, client)
	if err == nil && len(bytes.TrimSpace(body)) == 0 {
		err = errEmptySubscriptionBody
	}
	if err != nil {
		client.bodyFetched(dataDir, err)
		return err
	}
	WriteSubscriptionBodyCache(dataDir, body)
	client.headers(dataDir, subURL, hdr)
	client.bodyFetched(dataDir, nil)
	return nil
}

var errEmptySubscriptionBody = errors.New("empty subscription body")

// ---------------------------------------------------------------------------
// Singleflight dedup for subscription fetches.
//
// Background and explicit refreshes share one inflight map so:
//   1. Two concurrent background ticks for the same profile collapse to one fetch.
//   2. An explicit "Refresh subscription" click during a background fetch
//      attaches to the in-flight call and returns the same result — no double
//      write to the cache file, no double-billed origin request.
// ---------------------------------------------------------------------------

type subscriptionFetchInFlight struct {
	done chan struct{}
	err  error
}

var inflightSubscriptionFetch sync.Map // dataDir -> *subscriptionFetchInFlight

// RunSubscriptionFetchOnce ensures only one fetch is in flight per dataDir.
// All callers — background ticks and explicit refresh clicks alike — share
// the same outcome. If a fetch is already running, this call blocks (subject
// to its own ctx) until that fetch completes and then returns its error.
func RunSubscriptionFetchOnce(ctx context.Context, dataDir, subURL, ageKey string, client SubscriptionClient) error {
	if strings.TrimSpace(dataDir) == "" || strings.TrimSpace(subURL) == "" {
		return nil
	}
	candidate := &subscriptionFetchInFlight{done: make(chan struct{})}
	actual, loaded := inflightSubscriptionFetch.LoadOrStore(dataDir, candidate)
	if loaded {
		// Another goroutine is already fetching for this profile — wait.
		existing := actual.(*subscriptionFetchInFlight)
		select {
		case <-existing.done:
			return existing.err
		case <-ctx.Done():
			return ctx.Err()
		}
	}
	defer func() {
		close(candidate.done)
		inflightSubscriptionFetch.Delete(dataDir)
	}()
	candidate.err = refreshSubscriptionBodyCache(ctx, dataDir, subURL, ageKey, client)
	return candidate.err
}

// kickBackgroundSubscriptionRefresh starts a fire-and-forget fetch using the
// shared singleflight gate. Errors are logged through the pipeline trace
// (previously they were swallowed via `_ = refresh(...)` which made stale-
// cache regressions invisible).
func kickBackgroundSubscriptionRefresh(dataDir, subURL, ageKey string, client SubscriptionClient) {
	if strings.TrimSpace(dataDir) == "" || strings.TrimSpace(subURL) == "" {
		return
	}
	go func() {
		start := time.Now()
		ctx, cancel := context.WithTimeout(context.Background(), 55*time.Second)
		defer cancel()
		err := RunSubscriptionFetchOnce(ctx, dataDir, subURL, ageKey, client)
		fields := map[string]any{"dataDir": dataDir}
		if err != nil {
			fields["error"] = err.Error()
			traceEvent("pipeline.subscription.refresh_bg", "fail", time.Since(start), fields)
			return
		}
		traceEvent("pipeline.subscription.refresh_bg", "ok", time.Since(start), fields)
	}()
}

// ---------------------------------------------------------------------------
// Full-profile body → runtime config.yaml.
// ---------------------------------------------------------------------------

// tryWriteMergedFullProfile generates the runtime config.yaml from the active
// subscription body. The body source follows a cache-first policy:
//
//  1. If the dataDir already has a cached subscription body
//     (`subscription.cache.yaml` — written on first-ever Connect, by every
//     explicit "Refresh subscription", and by the background auto-update
//     loop), it is used as the source and Connect returns IMMEDIATELY
//     without any network I/O. A background refresh is kicked off for the
//     next Connect so subscription changes still propagate, just not on
//     the critical path.
//
//  2. If no cache exists (first-ever Connect, cache wiped by Clear cache,
//     brand-new profile), we do a synchronous fetch with a 55 s budget and
//     write the body to the cache. Subsequent connects fall into step 1.
//
//  3. If step 2's fetch fails the caller should fall back to the bare
//     `sub1 + Manual` profile.
//
// The returned pipelineOutcome explicitly tells the caller which branch was
// taken — replaces the prior `(bool, error)` return that conflated "not a
// full profile" with "fetch failed" with "merge template malformed".
//
// The profile's JavaScript override (in.Script) is threaded through to the
// pipeline; scriptOut, when non-nil, receives what it did.
func tryWriteMergedFullProfile(in Input, scriptOut *ScriptResult) (pipelineOutcome, error) {
	overallStart := time.Now()
	traceFields := map[string]any{
		"dataDir":   in.DataDir,
		"traffic":   in.Traffic,
		"enableTun": in.EnableTun,
	}

	var body []byte
	cached := ReadSubscriptionBodyCache(in.DataDir)
	cacheHit := len(bytes.TrimSpace(cached)) > 0
	if cacheHit {
		body = cached
		traceEvent("pipeline.subscription.cache_hit", "ok", 0, traceFields)
		// Fresh-enough body is on disk; defer the network hit. Next Connect
		// will pick up whatever the origin ships, but THIS Connect does
		// not wait for it.
		kickBackgroundSubscriptionRefresh(in.DataDir, in.SubURL, in.AgeKey, in.Env.Client)
	} else {
		// First-ever Connect for this profile (or cache was wiped). We HAVE
		// to block on the network because there's no prior body to serve.
		fetchStart := time.Now()
		ctx, cancel := context.WithTimeout(context.Background(), 55*time.Second)
		defer cancel()
		b, hdr, err := FetchSubscriptionBody(ctx, in.SubURL, in.AgeKey, in.Env.Client)
		fetchDur := time.Since(fetchStart)
		if err == nil {
			in.Env.Client.headers(in.DataDir, in.SubURL, hdr)
		}
		if err == nil && len(bytes.TrimSpace(b)) == 0 {
			err = errEmptySubscriptionBody
		}
		if err != nil {
			in.Env.Client.bodyFetched(in.DataDir, err)
			f := map[string]any{"dataDir": in.DataDir, "error": err.Error()}
			traceEvent("pipeline.subscription.fetch", "fail", fetchDur, f)
			if boundedio.IsLimitError(err) {
				return pipelineBodyTooLarge, err
			}
			return pipelineCacheMissNoNet, nil
		}
		traceEvent("pipeline.subscription.fetch", "ok", fetchDur, map[string]any{
			"dataDir":    in.DataDir,
			"body_bytes": len(b),
		})
		body = b
		WriteSubscriptionBodyCache(in.DataDir, body)
		in.Env.Client.bodyFetched(in.DataDir, nil)
	}

	parseStart := time.Now()
	doc, err := ParseClashDoc(body)
	if err != nil || doc == nil {
		f := map[string]any{"dataDir": in.DataDir}
		if err != nil {
			f["error"] = err.Error()
		}
		traceEvent("pipeline.subscription.parse", "fail", time.Since(parseStart), f)
		return pipelineParseFail, nil
	}
	if !IsFullProfile(doc) {
		traceEvent("pipeline.subscription.parse", "skip", time.Since(parseStart), map[string]any{
			"dataDir": in.DataDir,
			"reason":  "not_full_profile",
		})
		return pipelineNotFullProfile, nil
	}
	traceEvent("pipeline.subscription.parse", "ok", time.Since(parseStart), traceFields)

	mergeStart := time.Now()
	if err := ApplyMergeTemplate(doc, in.ExtendTemplate); err != nil {
		traceEvent("pipeline.subscription.merge", "fail", time.Since(mergeStart), map[string]any{
			"dataDir": in.DataDir,
			"stage":   "extend",
			"error":   err.Error(),
		})
		return pipelineTemplateFail, err
	}
	if err := ApplyMergeTemplate(doc, in.ProxyTemplate); err != nil {
		traceEvent("pipeline.subscription.merge", "fail", time.Since(mergeStart), map[string]any{
			"dataDir": in.DataDir,
			"stage":   "proxy",
			"error":   err.Error(),
		})
		return pipelineTemplateFail, err
	}
	if err := ApplyMergeTemplate(doc, in.RulesTemplate); err != nil {
		traceEvent("pipeline.subscription.merge", "fail", time.Since(mergeStart), map[string]any{
			"dataDir": in.DataDir,
			"stage":   "rules",
			"error":   err.Error(),
		})
		return pipelineTemplateFail, err
	}
	traceEvent("pipeline.subscription.merge", "ok", time.Since(mergeStart), traceFields)

	finalizeStart := time.Now()
	if err := finalizeRuntimeConfigPipelineWithScript(
		doc,
		in.DataDir,
		in.MixedPort,
		in.CtrlPort,
		in.Secret,
		in.Traffic,
		in.WithExternalController,
		in.EnableTun,
		in.Script,
		scriptOut,
		in.Env,
	); err != nil {
		traceEvent("pipeline.subscription.finalize", "fail", time.Since(finalizeStart), map[string]any{
			"dataDir": in.DataDir,
			"error":   err.Error(),
		})
		return pipelineValidateFail, err
	}
	traceEvent("pipeline.subscription.finalize", "ok", time.Since(finalizeStart), traceFields)

	out, err := MarshalYAML(doc)
	if err != nil {
		traceEvent("pipeline.subscription.write", "fail", 0, map[string]any{
			"dataDir": in.DataDir,
			"error":   err.Error(),
			"stage":   "marshal",
		})
		return pipelineWriteFail, err
	}
	cfgPath := filepath.Join(in.DataDir, "config.yaml")
	if err := fsutil.AtomicWriteFile(cfgPath, out, 0o644); err != nil {
		traceEvent("pipeline.subscription.write", "fail", 0, map[string]any{
			"dataDir": in.DataDir,
			"error":   err.Error(),
			"stage":   "writeFile",
		})
		return pipelineWriteFail, err
	}
	traceEvent("pipeline.subscription.write", "ok", time.Since(overallStart), map[string]any{
		"dataDir":   in.DataDir,
		"cache_hit": cacheHit,
		"yaml_size": len(out),
	})
	return pipelineOK, nil
}
