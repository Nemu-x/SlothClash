package runtimecfg

import (
	"encoding/base64"
	"errors"
	"fmt"
	"net"
	"net/url"
	"strconv"
	"strings"

	"gopkg.in/yaml.v3"
)

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

// NormalizeSubscriptionURL canonicalises a user-entered subscription URL: it
// defaults the scheme to https, rebuilds mieru URLs and refuses schemes we
// never fetch.
func NormalizeSubscriptionURL(raw string) (string, error) {
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
	// Only fetchable schemes. Without this, anything with a host (ftp://, gopher://…)
	// reached the HTTP client, which then failed obscurely instead of up front.
	if !subscriptionSchemeAllowed(u.Scheme) {
		return "", errSubscriptionURLScheme
	}
	return u.String(), nil
}

// IsMieruSubscriptionURL reports a mieru:// or mierus:// URL, which is turned
// into an inline profile instead of being fetched.
func IsMieruSubscriptionURL(norm string) bool {
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

// buildMieruSubscriptionYAML returns a minimal full-profile YAML document so
// tryWriteMergedFullProfile treats it like a normal subscription body. Mihomo
// cannot fetch mieru:// over HTTP; Clash Verge-style flow is inline proxies
// (see enfein/mieru clash-verge-rev docs).
func buildMieruSubscriptionYAML(norm string) ([]byte, error) {
	u, err := url.Parse(norm)
	if err != nil {
		return nil, err
	}
	if !IsMieruSubscriptionURL(norm) {
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
	// `transport` is mieru's wire protocol to the server (TCP/UDP); `udp`
	// independently controls relaying the app's UDP traffic over that tunnel.
	// They are NOT linked: mieru's documented mihomo config is `transport: TCP`
	// + `udp: true`. Mihomo requires transport, so default it to TCP; never
	// derive it from udp.
	if _, ok := proxy["transport"]; !ok {
		proxy["transport"] = "TCP"
	}
	// Relay the app's UDP by default. Without `udp: true` mieru carries TCP
	// fine but drops UDP apps (e.g. voice/QUIC), which looks like "chat works,
	// calls die". Matches mieru's official mihomo example and other clients.
	if _, ok := proxy["udp"]; !ok {
		proxy["udp"] = true
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

// DecodeBase64Flexible decodes standard or URL-safe base64, padded or not.
func DecodeBase64Flexible(s string) ([]byte, error) {
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
