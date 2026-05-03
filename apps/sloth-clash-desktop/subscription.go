package main

import (
	"context"
	"encoding/base64"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"

	"gopkg.in/yaml.v3"
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
		if b, err := decodeBase64Flexible(payload); err == nil && utf8.Valid(b) {
			text = strings.TrimSpace(string(b))
		}
	}
	return sanitizeSubscriptionAnnouncement(text)
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
	applySubscriptionIdentityHeaders(req)

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
	if sup := sanitizeSubscriptionSupportURL(headerFirstNonEmpty(resp.Header, subscriptionSupportHeaderKeys)); sup != "" {
		out.SubscriptionSupportURL = sup
	}
	if ann := normalizeSubscriptionAnnouncementFromHeader(headerFirstNonEmpty(resp.Header, subscriptionAnnounceHeaderKeys)); ann != "" {
		out.SubscriptionAnnouncement = ann
	}

	return out, nil
}

func peekSubscription(ctx context.Context, raw string) (SubscriptionPeek, error) {
	norm, err := normalizeSubscriptionURL(raw)
	if err != nil {
		return SubscriptionPeek{}, err
	}

	if peek, ok := mieruSubscriptionPeek(norm); ok {
		return peek, nil
	}

	cctx, cancel := context.WithTimeout(ctx, 26*time.Second)
	defer cancel()

	userAgents := []string{
		"clash.meta/mihomo; SlothClash/1.0",
		"ClashMeta/2.10.1.Meta-Alpha",
		"ClashForWindows/0.20.39",
		"SlothClash/1.0 (compatible; mihomo-like-client)",
	}

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

// normalizeMieruSubscriptionURL rebuilds mieru/mierus URLs so passwords may
// contain unescaped '@' (delimiter before host:port is the last '@').
func normalizeMieruSubscriptionURL(raw string) (string, error) {
	lower := strings.ToLower(raw)
	const pMierus = "mierus://"
	const pMieru = "mieru://"
	var scheme string
	var rest string
	switch {
	case strings.HasPrefix(lower, pMierus):
		scheme = "mierus"
		rest = raw[len(pMierus):]
	case strings.HasPrefix(lower, pMieru):
		scheme = "mieru"
		rest = raw[len(pMieru):]
	default:
		return "", errors.New("invalid subscription url")
	}

	frag := ""
	if i := strings.Index(rest, "#"); i >= 0 {
		frag = rest[i+1:]
		rest = rest[:i]
	}
	query := ""
	if i := strings.Index(rest, "?"); i >= 0 {
		query = rest[i+1:]
		rest = rest[:i]
	}

	lastAt := strings.LastIndex(rest, "@")
	var userInfo, hostPort string
	if lastAt < 0 {
		hostPort = strings.TrimSpace(rest)
	} else {
		userInfo = rest[:lastAt]
		hostPort = strings.TrimSpace(rest[lastAt+1:])
	}
	if hostPort == "" {
		return "", errors.New("invalid subscription url")
	}

	u := &url.URL{Scheme: scheme}
	userInfo = strings.TrimSpace(userInfo)
	if userInfo != "" {
		user, pass, hasColon := strings.Cut(userInfo, ":")
		if hasColon {
			u.User = url.UserPassword(user, pass)
		} else {
			u.User = url.User(userInfo)
		}
	}

	host, port, splitErr := net.SplitHostPort(hostPort)
	if splitErr != nil {
		u.Host = hostPort
	} else {
		u.Host = net.JoinHostPort(host, port)
	}

	u.RawQuery = query
	u.Fragment = frag
	return u.String(), nil
}

func normalizeSubscriptionURL(raw string) (string, error) {
	s := strings.TrimSpace(raw)
	if s == "" {
		return "", errors.New("url is required")
	}
	lower := strings.ToLower(s)
	if strings.HasPrefix(lower, "mieru://") || strings.HasPrefix(lower, "mierus://") {
		return normalizeMieruSubscriptionURL(s)
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

func subscriptionURLIsMieru(norm string) bool {
	u, err := url.Parse(norm)
	if err != nil || u == nil {
		return false
	}
	switch strings.ToLower(u.Scheme) {
	case "mieru", "mierus":
		return true
	default:
		return false
	}
}

// mieruSubscriptionPeek returns a synthetic peek for mieru/mierus subscription
// URLs. Go's net/http cannot GET these schemes (unsupported protocol scheme).
func mieruSubscriptionPeek(norm string) (SubscriptionPeek, bool) {
	if !subscriptionURLIsMieru(norm) {
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

// buildMieruSubscriptionYAML returns a minimal full-profile YAML document so
// tryWriteMergedFullProfile treats it like a normal subscription body. Mihomo
// cannot fetch mieru:// over HTTP; Clash Verge-style flow is inline proxies
// (see enfein/mieru clash-verge-rev docs).
func buildMieruSubscriptionYAML(norm string) ([]byte, error) {
	u, err := url.Parse(norm)
	if err != nil {
		return nil, err
	}
	if !subscriptionURLIsMieru(norm) {
		return nil, errors.New("not a mieru subscription url")
	}
	if u.Hostname() == "" {
		return nil, errors.New("mieru subscription url has no host")
	}
	portStr := u.Port()
	if portStr == "" {
		portStr = "443"
	}
	portNum, err := strconv.Atoi(portStr)
	if err != nil || portNum < 1 || portNum > 65535 {
		return nil, fmt.Errorf("invalid mieru port %q", portStr)
	}
	proxyName := "mieru-auto"
	if frag := strings.TrimSpace(u.Fragment); frag != "" {
		proxyName = frag
	}
	proxy := map[string]any{
		"name":   proxyName,
		"type":   "mieru",
		"server": u.Hostname(),
		"port":   portNum,
	}
	if u.User != nil {
		if v := strings.TrimSpace(u.User.Username()); v != "" {
			proxy["username"] = v
		}
		if pw, ok := u.User.Password(); ok && pw != "" {
			proxy["password"] = pw
		}
	}
	if q := u.Query(); len(q) > 0 {
		if v := strings.TrimSpace(q.Get("port-range")); v != "" {
			proxy["port-range"] = v
		}
		if v := strings.TrimSpace(q.Get("transport")); v != "" {
			proxy["transport"] = v
		}
		if vals, ok := q["udp"]; ok && len(vals) > 0 {
			v := strings.TrimSpace(vals[0])
			if v == "" {
				// ?udp — флаг присутствия (как parseBoolOrPresence на фронте)
				proxy["udp"] = true
			} else {
				proxy["udp"] = strings.EqualFold(v, "true") || v == "1"
			}
		}
		if v := strings.TrimSpace(q.Get("handshake-mode")); v != "" {
			proxy["handshake-mode"] = v
		}
		if v := strings.TrimSpace(q.Get("multiplexing")); v != "" {
			proxy["multiplexing"] = v
		}
	}
	// Mihomo requires `transport`. Order: explicit ?transport= wins; else if
	// ?udp is a truthy flag use UDP (matches ?udp / ?udp=true); else TCP.
	if _, ok := proxy["transport"]; !ok {
		if u, okb := proxy["udp"].(bool); okb && u {
			proxy["transport"] = "UDP"
		} else {
			proxy["transport"] = "TCP"
		}
	}
	if tr, _ := proxy["transport"].(string); strings.EqualFold(strings.TrimSpace(tr), "UDP") {
		if _, has := proxy["udp"]; !has {
			proxy["udp"] = true
		}
	}
	groupName := "mieru-manual"
	doc := map[string]any{
		"proxies": []any{proxy},
		"proxy-groups": []any{
			map[string]any{
				"name":    groupName,
				"type":    "select",
				"proxies": []any{proxyName},
			},
		},
		"rules": []any{
			fmt.Sprintf("MATCH,%s", groupName),
		},
	}
	return yaml.Marshal(doc)
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
