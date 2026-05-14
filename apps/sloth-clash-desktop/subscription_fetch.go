package main

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
)

// ---------------------------------------------------------------------------
// HTTP fetch + cache + tryWriteMergedFullProfile.
//
// Hot path summary:
//
//   tryWriteMergedFullProfile (called from writeRuntimeConfig)
//     ├── readSubscriptionBodyCache (cache hit → use immediately + bg refresh)
//     │       └── kickBackgroundSubscriptionRefresh
//     │           └── inflightSubscriptionFetch (singleflight dedup)
//     │               └── refreshSubscriptionBodyCache
//     │                       └── fetchSubscriptionBody (50 s timeout)
//     └── on cache miss: blocking fetchSubscriptionBody (55 s)
// ---------------------------------------------------------------------------

func fetchSubscriptionBody(ctx context.Context, rawURL string) ([]byte, error) {
	norm, err := normalizeSubscriptionURL(rawURL)
	if err != nil {
		return nil, err
	}
	if subscriptionURLIsMieru(norm) {
		return buildMieruSubscriptionYAML(norm)
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, norm, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("User-Agent", "clash.meta/mihomo; SlothClash/1.0")
	applySubscriptionIdentityHeaders(req)

	client := &http.Client{
		Timeout: 50 * time.Second,
		CheckRedirect: func(_ *http.Request, via []*http.Request) error {
			if len(via) >= 12 {
				return errors.New("too many redirects")
			}
			return nil
		},
	}

	resp, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		b, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
		return nil, fmt.Errorf("subscription HTTP %d: %s", resp.StatusCode, strings.TrimSpace(string(b)))
	}
	return io.ReadAll(io.LimitReader(resp.Body, 6<<20))
}

// subscriptionBodyCachePath returns the on-disk location where we persist the
// last-known-good subscription body bytes for a given runtime dataDir. The
// cache is the PRIMARY source on every Connect — the HTTP fetch runs in the
// background to refresh the cache for the NEXT connect, so Connect itself is
// never blocked on a slow / flaky subscription provider (previously a single
// slow origin could add 5–50 s of latency to Connect). This mirrors
// clash-verge-rev's behaviour: the runtime uses the last profile file on disk
// and subscription updates are a separate scheduled / manual operation.
func subscriptionBodyCachePath(dataDir string) string {
	return filepath.Join(dataDir, "subscription.cache.yaml")
}

func writeSubscriptionBodyCache(dataDir string, body []byte) {
	if dataDir == "" || len(body) == 0 {
		return
	}
	_ = os.MkdirAll(dataDir, 0o755)
	_ = atomicWriteFile(subscriptionBodyCachePath(dataDir), body, 0o600)
}

func readSubscriptionBodyCache(dataDir string) []byte {
	if dataDir == "" {
		return nil
	}
	b, err := os.ReadFile(subscriptionBodyCachePath(dataDir))
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
func refreshSubscriptionBodyCache(ctx context.Context, dataDir, subURL string) error {
	if strings.TrimSpace(dataDir) == "" || strings.TrimSpace(subURL) == "" {
		return nil
	}
	body, err := fetchSubscriptionBody(ctx, subURL)
	if err != nil {
		return err
	}
	if len(bytes.TrimSpace(body)) == 0 {
		return errors.New("empty subscription body")
	}
	writeSubscriptionBodyCache(dataDir, body)
	return nil
}

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

// runSubscriptionFetchOnce ensures only one fetch is in flight per dataDir.
// All callers — background ticks and explicit refresh clicks alike — share
// the same outcome. If a fetch is already running, this call blocks (subject
// to its own ctx) until that fetch completes and then returns its error.
func runSubscriptionFetchOnce(ctx context.Context, dataDir, subURL string) error {
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
	candidate.err = refreshSubscriptionBodyCache(ctx, dataDir, subURL)
	return candidate.err
}

// kickBackgroundSubscriptionRefresh starts a fire-and-forget fetch using the
// shared singleflight gate. Errors are logged through the pipeline trace
// (previously they were swallowed via `_ = refresh(...)` which made stale-
// cache regressions invisible).
func kickBackgroundSubscriptionRefresh(dataDir, subURL string) {
	if strings.TrimSpace(dataDir) == "" || strings.TrimSpace(subURL) == "" {
		return
	}
	go func() {
		start := time.Now()
		ctx, cancel := context.WithTimeout(context.Background(), 55*time.Second)
		defer cancel()
		err := runSubscriptionFetchOnce(ctx, dataDir, subURL)
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
func tryWriteMergedFullProfile(
	dataDir, subURL, extendTemplate, proxyTemplate, rulesTemplate string,
	ctrlPort, mixedPort int,
	secret, traffic string,
	withExternalController bool,
	enableTun bool,
) (pipelineOutcome, error) {
	overallStart := time.Now()
	traceFields := map[string]any{
		"dataDir":   dataDir,
		"traffic":   traffic,
		"enableTun": enableTun,
	}

	var body []byte
	cached := readSubscriptionBodyCache(dataDir)
	cacheHit := len(bytes.TrimSpace(cached)) > 0
	if cacheHit {
		body = cached
		traceEvent("pipeline.subscription.cache_hit", "ok", 0, traceFields)
		// Fresh-enough body is on disk; defer the network hit. Next Connect
		// will pick up whatever the origin ships, but THIS Connect does
		// not wait for it.
		kickBackgroundSubscriptionRefresh(dataDir, subURL)
	} else {
		// First-ever Connect for this profile (or cache was wiped). We HAVE
		// to block on the network because there's no prior body to serve.
		fetchStart := time.Now()
		ctx, cancel := context.WithTimeout(context.Background(), 55*time.Second)
		defer cancel()
		b, err := fetchSubscriptionBody(ctx, subURL)
		fetchDur := time.Since(fetchStart)
		if err != nil || len(bytes.TrimSpace(b)) == 0 {
			f := map[string]any{"dataDir": dataDir}
			if err != nil {
				f["error"] = err.Error()
			} else {
				f["error"] = "empty body"
			}
			traceEvent("pipeline.subscription.fetch", "fail", fetchDur, f)
			return pipelineCacheMissNoNet, nil
		}
		traceEvent("pipeline.subscription.fetch", "ok", fetchDur, map[string]any{
			"dataDir":     dataDir,
			"body_bytes":  len(b),
		})
		body = b
		writeSubscriptionBodyCache(dataDir, body)
	}

	parseStart := time.Now()
	doc, err := parseClashDocToMap(body)
	if err != nil || doc == nil {
		f := map[string]any{"dataDir": dataDir}
		if err != nil {
			f["error"] = err.Error()
		}
		traceEvent("pipeline.subscription.parse", "fail", time.Since(parseStart), f)
		return pipelineParseFail, nil
	}
	if !subscriptionDocIsFullProfile(doc) {
		traceEvent("pipeline.subscription.parse", "skip", time.Since(parseStart), map[string]any{
			"dataDir": dataDir,
			"reason":  "not_full_profile",
		})
		return pipelineNotFullProfile, nil
	}
	traceEvent("pipeline.subscription.parse", "ok", time.Since(parseStart), traceFields)

	mergeStart := time.Now()
	if err := applyProfileMergeTemplate(doc, extendTemplate); err != nil {
		traceEvent("pipeline.subscription.merge", "fail", time.Since(mergeStart), map[string]any{
			"dataDir": dataDir,
			"stage":   "extend",
			"error":   err.Error(),
		})
		return pipelineTemplateFail, err
	}
	if err := applyProfileMergeTemplate(doc, proxyTemplate); err != nil {
		traceEvent("pipeline.subscription.merge", "fail", time.Since(mergeStart), map[string]any{
			"dataDir": dataDir,
			"stage":   "proxy",
			"error":   err.Error(),
		})
		return pipelineTemplateFail, err
	}
	if err := applyProfileMergeTemplate(doc, rulesTemplate); err != nil {
		traceEvent("pipeline.subscription.merge", "fail", time.Since(mergeStart), map[string]any{
			"dataDir": dataDir,
			"stage":   "rules",
			"error":   err.Error(),
		})
		return pipelineTemplateFail, err
	}
	traceEvent("pipeline.subscription.merge", "ok", time.Since(mergeStart), traceFields)

	finalizeStart := time.Now()
	if err := finalizeRuntimeConfigPipeline(
		doc,
		dataDir,
		mixedPort,
		ctrlPort,
		secret,
		traffic,
		withExternalController,
		enableTun,
	); err != nil {
		traceEvent("pipeline.subscription.finalize", "fail", time.Since(finalizeStart), map[string]any{
			"dataDir": dataDir,
			"error":   err.Error(),
		})
		return pipelineValidateFail, err
	}
	traceEvent("pipeline.subscription.finalize", "ok", time.Since(finalizeStart), traceFields)

	out, err := marshalRuntimeYAML(doc)
	if err != nil {
		traceEvent("pipeline.subscription.write", "fail", 0, map[string]any{
			"dataDir": dataDir,
			"error":   err.Error(),
			"stage":   "marshal",
		})
		return pipelineWriteFail, err
	}
	cfgPath := filepath.Join(dataDir, "config.yaml")
	if err := atomicWriteFile(cfgPath, out, 0o644); err != nil {
		traceEvent("pipeline.subscription.write", "fail", 0, map[string]any{
			"dataDir": dataDir,
			"error":   err.Error(),
			"stage":   "writeFile",
		})
		return pipelineWriteFail, err
	}
	traceEvent("pipeline.subscription.write", "ok", time.Since(overallStart), map[string]any{
		"dataDir":   dataDir,
		"cache_hit": cacheHit,
		"yaml_size": len(out),
	})
	return pipelineOK, nil
}
