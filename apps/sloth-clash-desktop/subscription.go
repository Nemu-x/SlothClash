package main

import (
	"context"
	"encoding/base64"
	"errors"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"
	"unicode/utf8"
)

// SubscriptionPeek is returned before import so the UI can show a suggested name.
type SubscriptionPeek struct {
	URL              string `json:"url"`
	SuggestedName    string `json:"suggestedName"`
	ProfileTitleRaw  string `json:"profileTitleRaw,omitempty"`
	HTTPStatus       int    `json:"httpStatus,omitempty"`
	LastError        string `json:"lastError,omitempty"`
	SubscriptionInfo string `json:"subscriptionInfo,omitempty"` // decoded userinfo JSON when present
}

func (a *App) PeekSubscriptionFromURL(raw string) (SubscriptionPeek, error) {
	_ = a
	return peekSubscription(context.Background(), raw)
}

func resolveSubscriptionName(ctx context.Context, nameHint, raw string) (finalName string, peek SubscriptionPeek, err error) {
	peek, err = peekSubscription(ctx, raw)
	if err != nil {
		return "", peek, err
	}
	hint := strings.TrimSpace(nameHint)
	if hint != "" {
		return hint, peek, nil
	}
	if strings.TrimSpace(peek.SuggestedName) != "" {
		return strings.TrimSpace(peek.SuggestedName), peek, nil
	}
	return "Subscription", peek, nil
}

func hostFromSubscriptionURL(norm string) string {
	u, err := url.Parse(norm)
	if err != nil || u.Hostname() == "" {
		return ""
	}
	return u.Hostname()
}

// opaqueBase64Shell reports whether s looks like a long base64 payload (not a human title).
func opaqueBase64Shell(s string) bool {
	s = strings.TrimSpace(s)
	if len(s) < 20 {
		return false
	}
	ok := 0
	for _, r := range s {
		switch {
		case (r >= 'A' && r <= 'Z') || (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9') || r == '+' || r == '/' || r == '=':
			ok++
		default:
			return false
		}
	}
	return ok == len([]rune(s))
}

func printableHumanTitle(s string) bool {
	if len(strings.TrimSpace(s)) == 0 || len(s) > 240 {
		return false
	}
	for _, r := range s {
		if r < 32 && r != '\t' {
			return false
		}
	}
	return utf8.ValidString(s)
}

func deriveSuggestedNameFromTitle(norm, titleRaw string) string {
	host := hostFromSubscriptionURL(norm)
	titleTrim := strings.TrimSpace(titleRaw)
	if titleTrim == "" {
		return host
	}
	if dec := decodeProfileTitleB64(titleRaw); dec != "" {
		return dec
	}
	if !opaqueBase64Shell(titleTrim) && printableHumanTitle(titleTrim) {
		return titleTrim
	}
	return host
}

// normalizeSubscriptionUserinfoHeader turns Subscription-Userinfo into a JSON-ish string we can
// persist and parse on the frontend. Providers differ: base64 JSON, raw JSON, or key=value.
func normalizeSubscriptionUserinfoHeader(raw string) string {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return ""
	}
	if b, err := decodeBase64Flexible(raw); err == nil && utf8.Valid(b) {
		return strings.TrimSpace(string(b))
	}
	if dec, err := url.QueryUnescape(raw); err == nil && strings.TrimSpace(dec) != "" {
		raw2 := strings.TrimSpace(dec)
		if b, err := decodeBase64Flexible(raw2); err == nil && utf8.Valid(b) {
			return strings.TrimSpace(string(b))
		}
		if strings.HasPrefix(raw2, "{") {
			return raw2
		}
		raw = raw2
	}
	if strings.HasPrefix(raw, "{") {
		return raw
	}
	// Plain key=value (some providers send this without base64)
	if strings.Contains(raw, "=") {
		return raw
	}
	return ""
}

func fetchSubscriptionPeekHeaders(ctx context.Context, norm, userAgent string) (SubscriptionPeek, error) {
	out := SubscriptionPeek{URL: norm}

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, norm, nil)
	if err != nil {
		out.LastError = err.Error()
		return out, err
	}
	req.Header.Set("User-Agent", userAgent)

	client := &http.Client{
		Timeout: 22 * time.Second,
		CheckRedirect: func(_ *http.Request, via []*http.Request) error {
			if len(via) >= 12 {
				return errors.New("too many redirects")
			}
			return nil
		},
	}

	resp, err := client.Do(req)
	if err != nil {
		out.LastError = err.Error()
		return out, err
	}
	defer resp.Body.Close()
	_, _ = io.Copy(io.Discard, io.LimitReader(resp.Body, 512*1024))

	out.HTTPStatus = resp.StatusCode
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		out.LastError = "HTTP " + resp.Status
		return out, errors.New(out.LastError)
	}

	titleRaw := headerGet(resp.Header, "Profile-Title")
	out.ProfileTitleRaw = titleRaw
	out.SuggestedName = deriveSuggestedNameFromTitle(norm, titleRaw)

	if sub := headerGet(resp.Header, "Subscription-Userinfo"); sub != "" {
		if norm := normalizeSubscriptionUserinfoHeader(sub); norm != "" {
			out.SubscriptionInfo = norm
		}
	}

	return out, nil
}

func peekSubscription(ctx context.Context, raw string) (SubscriptionPeek, error) {
	norm, err := normalizeSubscriptionURL(raw)
	if err != nil {
		return SubscriptionPeek{}, err
	}

	cctx, cancel := context.WithTimeout(ctx, 26*time.Second)
	defer cancel()

	userAgents := []string{
		"clash.meta/mihomo; SlothClash/1.0",
		"ClashMeta/2.10.1.Meta-Alpha",
		"ClashForWindows/0.20.39",
		"SlothClash/1.0 (compatible; mihomo-like-client)",
	}

	host := hostFromSubscriptionURL(norm)
	var successes []SubscriptionPeek
	var bestErr SubscriptionPeek
	var lastErr error

	for _, ua := range userAgents {
		out, err := fetchSubscriptionPeekHeaders(cctx, norm, ua)
		if err != nil {
			lastErr = err
			if out.HTTPStatus != 0 || out.LastError != "" {
				bestErr = out
			}
			continue
		}
		successes = append(successes, out)
	}

	if len(successes) == 0 {
		if bestErr.HTTPStatus != 0 {
			if bestErr.HTTPStatus < 200 || bestErr.HTTPStatus >= 300 {
				if lastErr != nil {
					return bestErr, lastErr
				}
				return bestErr, errors.New(bestErr.LastError)
			}
			return bestErr, nil
		}
		if lastErr != nil {
			return bestErr, lastErr
		}
		return bestErr, errors.New("subscription probe failed")
	}

	scorePeek := func(p SubscriptionPeek) int {
		score := 0
		if strings.TrimSpace(p.SubscriptionInfo) != "" {
			score += 4
		}
		if strings.TrimSpace(p.SuggestedName) != "" && p.SuggestedName != host {
			score += 2
		}
		if p.HTTPStatus >= 200 && p.HTTPStatus < 300 {
			score += 1
		}
		return score
	}

	picked := successes[0]
	bestScore := scorePeek(picked)
	for i := 1; i < len(successes); i++ {
		s := successes[i]
		if sc := scorePeek(s); sc > bestScore {
			bestScore = sc
			picked = s
		}
	}
	return picked, nil
}

func normalizeSubscriptionURL(raw string) (string, error) {
	s := strings.TrimSpace(raw)
	if s == "" {
		return "", errors.New("url is required")
	}
	if !strings.Contains(s, "://") {
		s = "https://" + s
	}
	u, err := url.Parse(s)
	if err != nil || u.Host == "" {
		return "", errors.New("invalid subscription url")
	}
	return u.String(), nil
}

func headerGet(h http.Header, key string) string {
	if h == nil {
		return ""
	}
	can := http.CanonicalHeaderKey(key)
	if v := h.Get(can); v != "" {
		return v
	}
	for k, vals := range h {
		if strings.EqualFold(k, key) && len(vals) > 0 {
			return vals[0]
		}
	}
	return ""
}

func decodeProfileTitleB64(raw string) string {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return ""
	}
	// Some providers send Profile-Title as "base64:<payload>" instead of raw base64.
	if strings.HasPrefix(strings.ToLower(raw), "base64:") {
		raw = strings.TrimSpace(raw[len("base64:"):])
	}
	if raw == "" {
		return ""
	}
	b, err := decodeBase64Flexible(raw)
	if err != nil || len(b) == 0 {
		return ""
	}
	if !utf8.Valid(b) {
		return ""
	}
	s := strings.TrimSpace(string(b))
	s = strings.ReplaceAll(s, "\x00", "")
	return s
}

func decodeBase64Flexible(s string) ([]byte, error) {
	s = strings.TrimSpace(s)
	var last error
	for _, enc := range []*base64.Encoding{
		base64.RawURLEncoding,
		base64.URLEncoding,
		base64.RawStdEncoding,
		base64.StdEncoding,
	} {
		b, err := enc.DecodeString(s)
		if err == nil {
			return b, nil
		}
		last = err
	}
	if last == nil {
		last = errors.New("invalid base64")
	}
	return nil, last
}
