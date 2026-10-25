package main

import (
	"context"
	"errors"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"
	"unicode/utf8"

	"SlothClashDesktop/internal/runtimecfg"
)

// SubscriptionPeek is returned before import so the UI can show a suggested name.
type SubscriptionPeek struct {
	URL                      string `json:"url"`
	SuggestedName            string `json:"suggestedName"`
	ProfileTitleRaw          string `json:"profileTitleRaw,omitempty"`
	HTTPStatus               int    `json:"httpStatus,omitempty"`
	LastError                string `json:"lastError,omitempty"`
	SubscriptionInfo         string `json:"subscriptionInfo,omitempty"` // decoded userinfo JSON when present
	SubscriptionSupportURL   string `json:"subscriptionSupportUrl,omitempty"`
	SubscriptionAnnouncement string `json:"subscriptionAnnouncement,omitempty"`
}

// HTTP header names that some providers use for support / docs links (first non-empty wins).
var subscriptionSupportHeaderKeys = []string{
	"Support",
	"Support-Url",
	"support-url",
	"Profile-Web-Page",
	"profile-web-page",
	"Web-Page",
	"Website",
}

// HTTP header names for a short provider announcement / notice.
var subscriptionAnnounceHeaderKeys = []string{
	"Announcement",
	"announcement",
	"Announce",
	"Profile-Announce",
	"X-Announcement",
}

func headerFirstNonEmpty(h http.Header, keys []string) string {
	for _, k := range keys {
		if v := strings.TrimSpace(headerGet(h, k)); v != "" {
			return v
		}
	}
	return ""
}

func sanitizeSubscriptionSupportURL(raw string) string {
	s := strings.TrimSpace(raw)
	if s == "" {
		return ""
	}
	if len(s) > 2048 {
		s = s[:2048]
	}
	lower := strings.ToLower(s)
	switch {
	case strings.HasPrefix(lower, "https://") || strings.HasPrefix(lower, "http://"):
		return s
	case strings.HasPrefix(lower, "tg://"):
		return s
	case strings.HasPrefix(lower, "t.me/") || strings.HasPrefix(lower, "telegram.me/"):
		return "https://" + strings.TrimPrefix(strings.TrimPrefix(s, "https://"), "http://")
	default:
		if strings.Contains(lower, "t.me/") {
			if idx := strings.Index(lower, "t.me/"); idx >= 0 {
				return "https://" + strings.TrimSpace(s[idx:])
			}
		}
	}
	return ""
}

func sanitizeSubscriptionAnnouncement(raw string) string {
	s := strings.TrimSpace(raw)
	if s == "" {
		return ""
	}
	s = strings.ReplaceAll(s, "\r\n", "\n")
	s = strings.ReplaceAll(s, "\r", "\n")
	runes := []rune(s)
	const maxRunes = 6000
	if len(runes) > maxRunes {
		s = string(runes[:maxRunes])
	}
	return s
}

// normalizeSubscriptionAnnouncementFromHeader decodes provider payloads (e.g. `base64:…` UTF-8)
// before we persist — same idea as Profile-Title / Subscription-Userinfo normalization.
func normalizeSubscriptionAnnouncementFromHeader(raw string) string {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return ""
	}
	text := raw
	if strings.HasPrefix(strings.ToLower(raw), "base64:") {
		payload := strings.TrimSpace(raw[len("base64:"):])
		if b, err := runtimecfg.DecodeBase64Flexible(payload); err == nil && utf8.Valid(b) {
			text = strings.TrimSpace(string(b))
		}
	}
	return sanitizeSubscriptionAnnouncement(text)
}

func (a *App) PeekSubscriptionFromURL(raw string) (SubscriptionPeek, error) {
	_ = a
	return peekSubscription(context.Background(), raw, currentSubscriptionClient())
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
	if b, err := runtimecfg.DecodeBase64Flexible(raw); err == nil && utf8.Valid(b) {
		return strings.TrimSpace(string(b))
	}
	if dec, err := url.QueryUnescape(raw); err == nil && strings.TrimSpace(dec) != "" {
		raw2 := strings.TrimSpace(dec)
		if b, err := runtimecfg.DecodeBase64Flexible(raw2); err == nil && utf8.Valid(b) {
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

// The probe sends userAgent and otherwise identifies itself as client.
func fetchSubscriptionPeekHeaders(ctx context.Context, norm, userAgent string, client runtimecfg.SubscriptionClient) (SubscriptionPeek, error) {
	out := SubscriptionPeek{URL: norm}

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, norm, nil)
	if err != nil {
		out.LastError = err.Error()
		return out, err
	}
	req.Header.Set("User-Agent", userAgent)
	runtimecfg.ApplyIdentityHeaders(req, client)

	httpClient := &http.Client{
		Timeout: 22 * time.Second,
		CheckRedirect: func(_ *http.Request, via []*http.Request) error {
			if len(via) >= runtimecfg.SubscriptionMaxRedirects {
				return runtimecfg.ErrTooManyRedirects
			}
			return nil
		},
	}

	resp, err := httpClient.Do(req)
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
	if sup := sanitizeSubscriptionSupportURL(headerFirstNonEmpty(resp.Header, subscriptionSupportHeaderKeys)); sup != "" {
		out.SubscriptionSupportURL = sup
	}
	if ann := normalizeSubscriptionAnnouncementFromHeader(headerFirstNonEmpty(resp.Header, subscriptionAnnounceHeaderKeys)); ann != "" {
		out.SubscriptionAnnouncement = ann
	}

	return out, nil
}

// client is the caller's currentSubscriptionClient snapshot.
func peekSubscription(ctx context.Context, raw string, client runtimecfg.SubscriptionClient) (SubscriptionPeek, error) {
	norm, err := runtimecfg.NormalizeSubscriptionURL(raw)
	if err != nil {
		return SubscriptionPeek{}, err
	}

	if peek, ok := mieruSubscriptionPeek(norm); ok {
		return peek, nil
	}

	cctx, cancel := context.WithTimeout(ctx, 26*time.Second)
	defer cancel()

	userAgents := subscriptionProbeUserAgents()

	var bestErr SubscriptionPeek
	var lastErr error

	for _, ua := range userAgents {
		out, err := fetchSubscriptionPeekHeaders(cctx, norm, ua, client)
		if err != nil {
			lastErr = err
			if out.HTTPStatus != 0 || out.LastError != "" {
				bestErr = out
			}
			continue
		}
		// Fast path: first successful probe is usually enough and avoids N sequential HTTP probes.
		return out, nil
	}
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

// mieruSubscriptionPeek returns a synthetic peek for mieru/mierus subscription
// URLs. Go's net/http cannot GET these schemes (unsupported protocol scheme).
func mieruSubscriptionPeek(norm string) (SubscriptionPeek, bool) {
	if !runtimecfg.IsMieruSubscriptionURL(norm) {
		return SubscriptionPeek{}, false
	}
	u, err := url.Parse(norm)
	if err != nil || u.Hostname() == "" {
		return SubscriptionPeek{URL: norm, LastError: "invalid mieru url"}, true
	}
	name := "Mieru " + u.Hostname()
	if p := u.Port(); p != "" {
		name = name + ":" + p
	}
	return SubscriptionPeek{
		URL:           norm,
		SuggestedName: name,
		HTTPStatus:    200,
	}, true
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
	b, err := runtimecfg.DecodeBase64Flexible(raw)
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
