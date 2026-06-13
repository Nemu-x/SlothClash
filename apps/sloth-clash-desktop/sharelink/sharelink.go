// Package sharelink converts proxy "share links" (vless://, vmess://, ss://,
// trojan://, hysteria2://, tuic://) into mihomo (Clash.Meta) proxy entries.
//
// It is intentionally self-contained (no mihomo Go dependency): each parser
// maps a URI/encoded form to a map[string]any matching mihomo's `proxies:`
// schema. Unknown/extra fields are ignored rather than failing, and an
// unsupported scheme returns ErrUnsupportedScheme so callers can skip-and-report.
package sharelink

import (
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"net/url"
	"strconv"
	"strings"
)

// Proxy is a single mihomo proxy entry (marshals to a YAML mapping).
type Proxy map[string]any

// ErrUnsupportedScheme is returned by ParseLink for a scheme we don't handle.
var ErrUnsupportedScheme = errors.New("unsupported share-link scheme")

// SupportedSchemes lists the URI schemes ParseLink understands.
var SupportedSchemes = []string{"vless", "vmess", "ss", "trojan", "hysteria2", "hy2", "tuic"}

// HasSupportedScheme reports whether s begins with a supported share-link scheme.
func HasSupportedScheme(s string) bool {
	s = strings.TrimSpace(s)
	for _, sc := range SupportedSchemes {
		if strings.HasPrefix(s, sc+"://") {
			return true
		}
	}
	return false
}

// ParseLink converts a single share link into a mihomo Proxy.
func ParseLink(link string) (Proxy, error) {
	link = strings.TrimSpace(link)
	scheme, _, found := strings.Cut(link, "://")
	if !found {
		return nil, fmt.Errorf("not a share link: %q", truncate(link, 24))
	}
	switch strings.ToLower(scheme) {
	case "vless":
		return parseVless(link)
	case "vmess":
		return parseVmess(link)
	case "ss":
		return parseSS(link)
	case "trojan":
		return parseTrojan(link)
	case "hysteria2", "hy2":
		return parseHysteria2(link)
	case "tuic":
		return parseTuic(link)
	default:
		return nil, fmt.Errorf("%w: %s", ErrUnsupportedScheme, scheme)
	}
}

// ParseMany parses a block of text that may contain one share link per line.
// It returns the successfully-parsed proxies and the raw lines it could not
// convert (unsupported scheme or malformed). Blank lines and comments (#) are
// ignored. Proxy names are de-duplicated so the result is a valid mihomo list.
func ParseMany(text string) (proxies []Proxy, skipped []string) {
	for _, raw := range strings.Split(normalizeNewlines(text), "\n") {
		line := strings.TrimSpace(raw)
		if line == "" || strings.HasPrefix(line, "//") || strings.HasPrefix(line, "#") {
			continue
		}
		p, err := ParseLink(line)
		if err != nil {
			skipped = append(skipped, line)
			continue
		}
		proxies = append(proxies, p)
	}
	dedupeNames(proxies)
	return proxies, skipped
}

// LooksLikeShareLinks reports whether text (after an optional base64 unwrap)
// contains at least one supported share link. It is the detection primitive
// used to distinguish a V2Ray-style subscription from Clash YAML.
func LooksLikeShareLinks(text string) bool {
	for _, candidate := range []string{text, DecodeBase64Block(text)} {
		for _, raw := range strings.Split(normalizeNewlines(candidate), "\n") {
			if HasSupportedScheme(raw) {
				return true
			}
		}
	}
	return false
}

// DecodeBase64Block returns the base64-decoded text if the whole input is a
// single base64 blob (the V2Ray subscription envelope); otherwise it returns "".
func DecodeBase64Block(text string) string {
	t := strings.TrimSpace(text)
	if t == "" || strings.ContainsAny(t, " \n\r\t") {
		// A base64 envelope is one contiguous token; multi-line/space input is
		// already decoded text.
		// (still allow trailing newline trimmed above)
	}
	// Try standard and URL-safe, with and without padding.
	for _, enc := range []*base64.Encoding{
		base64.StdEncoding, base64.RawStdEncoding,
		base64.URLEncoding, base64.RawURLEncoding,
	} {
		if dec, err := enc.DecodeString(strings.TrimSpace(t)); err == nil && len(dec) > 0 {
			s := string(dec)
			if HasSupportedScheme(firstNonEmptyLine(s)) || strings.Contains(s, "://") {
				return s
			}
		}
	}
	return ""
}

// ---- per-scheme parsers ----

func parseVless(link string) (Proxy, error) {
	u, err := url.Parse(link)
	if err != nil {
		return nil, err
	}
	host, port, err := hostPort(u)
	if err != nil {
		return nil, err
	}
	uuid := u.User.Username()
	if uuid == "" {
		return nil, errors.New("vless: missing uuid")
	}
	q := u.Query()
	p := Proxy{
		"name":   nameFrom(u, host, port),
		"type":   "vless",
		"server": host,
		"port":   port,
		"uuid":   uuid,
		"udp":    true,
	}
	network := orDefault(q.Get("type"), "tcp")
	p["network"] = network
	if flow := q.Get("flow"); flow != "" {
		p["flow"] = flow
	}
	// packet-encoding (xudp/packetaddr) — our nodes set xudp; without it UDP
	// over the proxy degrades vs the YAML profile.
	if pe := q.Get("packetEncoding"); pe != "" {
		p["packet-encoding"] = pe
	}
	security := q.Get("security")
	if security == "tls" || security == "reality" || q.Get("sni") != "" {
		p["tls"] = true
		if sni := q.Get("sni"); sni != "" {
			p["servername"] = sni
		}
		if fp := q.Get("fp"); fp != "" {
			p["client-fingerprint"] = fp
		}
		if alpn := q.Get("alpn"); alpn != "" {
			p["alpn"] = splitCSV(alpn)
		}
	}
	if security == "reality" {
		ro := map[string]any{}
		if pbk := q.Get("pbk"); pbk != "" {
			ro["public-key"] = pbk
		}
		if sid := q.Get("sid"); sid != "" {
			ro["short-id"] = sid
		}
		if len(ro) > 0 {
			p["reality-opts"] = ro
		}
	}
	applyTransport(p, network, q)
	return p, nil
}

func parseVmess(link string) (Proxy, error) {
	raw := strings.TrimPrefix(link, "vmess://")
	dec, err := decodeFlexible(raw)
	if err != nil {
		return nil, fmt.Errorf("vmess: bad base64: %w", err)
	}
	var v map[string]any
	if err := json.Unmarshal(dec, &v); err != nil {
		return nil, fmt.Errorf("vmess: bad json: %w", err)
	}
	server := str(v["add"])
	port := intish(v["port"])
	if server == "" || port == 0 {
		return nil, errors.New("vmess: missing server/port")
	}
	name := str(v["ps"])
	if name == "" {
		name = fmt.Sprintf("%s:%d", server, port)
	}
	network := orDefault(str(v["net"]), "tcp")
	p := Proxy{
		"name":    name,
		"type":    "vmess",
		"server":  server,
		"port":    port,
		"uuid":    str(v["id"]),
		"alterId": intish(v["aid"]),
		"cipher":  orDefault(str(v["scy"]), "auto"),
		"network": network,
		"udp":     true,
	}
	tls := str(v["tls"])
	if tls == "tls" || str(v["sni"]) != "" {
		p["tls"] = true
		if sni := firstNonEmpty(str(v["sni"]), str(v["host"])); sni != "" {
			p["servername"] = sni
		}
		if alpn := str(v["alpn"]); alpn != "" {
			p["alpn"] = splitCSV(alpn)
		}
	}
	switch network {
	case "ws":
		ws := map[string]any{}
		if path := str(v["path"]); path != "" {
			ws["path"] = path
		}
		if host := str(v["host"]); host != "" {
			ws["headers"] = map[string]any{"Host": host}
		}
		if len(ws) > 0 {
			p["ws-opts"] = ws
		}
	case "grpc":
		if sn := firstNonEmpty(str(v["path"]), str(v["serviceName"])); sn != "" {
			p["grpc-opts"] = map[string]any{"grpc-service-name": sn}
		}
	}
	return p, nil
}

func parseSS(link string) (Proxy, error) {
	rest := strings.TrimPrefix(link, "ss://")
	// Split off #fragment (name) first.
	frag := ""
	if i := strings.IndexByte(rest, '#'); i >= 0 {
		frag = rest[i+1:]
		rest = rest[:i]
	}
	var method, password, host string
	var port int
	if at := strings.LastIndexByte(rest, '@'); at >= 0 {
		// SIP002: ss://base64(method:password)@host:port[/?plugin=...]
		userinfo := rest[:at]
		hostpart := rest[at+1:]
		mp, err := decodeFlexible(userinfo)
		if err != nil {
			// userinfo may be plain method:password (rare)
			mp = []byte(userinfo)
		}
		method, password = splitColon(string(mp))
		hp, _, _ := strings.Cut(hostpart, "/")
		hp, _, _ = strings.Cut(hp, "?")
		host, port = splitHostPort(hp)
	} else {
		// Legacy: ss://base64(method:password@host:port)
		dec, err := decodeFlexible(rest)
		if err != nil {
			return nil, fmt.Errorf("ss: bad base64: %w", err)
		}
		body := string(dec)
		creds, hp := "", ""
		if at := strings.LastIndexByte(body, '@'); at >= 0 {
			creds, hp = body[:at], body[at+1:]
		} else {
			return nil, errors.New("ss: malformed legacy link")
		}
		method, password = splitColon(creds)
		host, port = splitHostPort(hp)
	}
	if host == "" || port == 0 || method == "" {
		return nil, errors.New("ss: missing cipher/server/port")
	}
	name := decodeFragment(frag)
	if name == "" {
		name = fmt.Sprintf("%s:%d", host, port)
	}
	p := Proxy{
		"name":     name,
		"type":     "ss",
		"server":   host,
		"port":     port,
		"cipher":   method,
		"password": password,
		"udp":      true,
	}
	// plugin (?plugin=obfs-local;obfs=http;obfs-host=...)
	if i := strings.IndexByte(link, '?'); i >= 0 {
		if q, err := url.ParseQuery(stripFragment(link[i+1:])); err == nil {
			if plug := q.Get("plugin"); plug != "" {
				applySSPlugin(p, plug)
			}
		}
	}
	return p, nil
}

func parseTrojan(link string) (Proxy, error) {
	u, err := url.Parse(link)
	if err != nil {
		return nil, err
	}
	host, port, err := hostPort(u)
	if err != nil {
		return nil, err
	}
	password := u.User.Username()
	if password == "" {
		return nil, errors.New("trojan: missing password")
	}
	q := u.Query()
	p := Proxy{
		"name":     nameFrom(u, host, port),
		"type":     "trojan",
		"server":   host,
		"port":     port,
		"password": password,
		"udp":      true,
	}
	if sni := firstNonEmpty(q.Get("sni"), q.Get("peer")); sni != "" {
		p["sni"] = sni
	}
	if q.Get("allowInsecure") == "1" || q.Get("insecure") == "1" {
		p["skip-cert-verify"] = true
	}
	if alpn := q.Get("alpn"); alpn != "" {
		p["alpn"] = splitCSV(alpn)
	}
	network := q.Get("type")
	if network != "" && network != "tcp" {
		p["network"] = network
		applyTransport(p, network, q)
	}
	return p, nil
}

func parseHysteria2(link string) (Proxy, error) {
	u, err := url.Parse(link)
	if err != nil {
		return nil, err
	}
	host, port, err := hostPort(u)
	if err != nil {
		return nil, err
	}
	q := u.Query()
	// auth may be in userinfo (password) or as ?auth=
	password := u.User.Username()
	if pw, ok := u.User.Password(); ok && pw != "" {
		password = password + ":" + pw
	}
	if password == "" {
		password = q.Get("auth")
	}
	p := Proxy{
		"name":     nameFrom(u, host, port),
		"type":     "hysteria2",
		"server":   host,
		"port":     port,
		"password": password,
	}
	if sni := q.Get("sni"); sni != "" {
		p["sni"] = sni
	}
	if q.Get("insecure") == "1" {
		p["skip-cert-verify"] = true
	}
	if obfs := q.Get("obfs"); obfs != "" {
		p["obfs"] = obfs
		if op := q.Get("obfs-password"); op != "" {
			p["obfs-password"] = op
		}
	}
	if alpn := q.Get("alpn"); alpn != "" {
		p["alpn"] = splitCSV(alpn)
	}
	return p, nil
}

func parseTuic(link string) (Proxy, error) {
	u, err := url.Parse(link)
	if err != nil {
		return nil, err
	}
	host, port, err := hostPort(u)
	if err != nil {
		return nil, err
	}
	uuid := u.User.Username()
	password, _ := u.User.Password()
	if uuid == "" {
		return nil, errors.New("tuic: missing uuid")
	}
	q := u.Query()
	p := Proxy{
		"name":     nameFrom(u, host, port),
		"type":     "tuic",
		"server":   host,
		"port":     port,
		"uuid":     uuid,
		"password": password,
	}
	if sni := q.Get("sni"); sni != "" {
		p["sni"] = sni
	}
	if alpn := q.Get("alpn"); alpn != "" {
		p["alpn"] = splitCSV(alpn)
	}
	if cc := q.Get("congestion_control"); cc != "" {
		p["congestion-controller"] = cc
	}
	if rm := q.Get("udp_relay_mode"); rm != "" {
		p["udp-relay-mode"] = rm
	}
	if q.Get("allow_insecure") == "1" {
		p["skip-cert-verify"] = true
	}
	return p, nil
}

// ---- helpers ----

func applyTransport(p Proxy, network string, q url.Values) {
	switch network {
	case "ws":
		ws := map[string]any{}
		if path := q.Get("path"); path != "" {
			ws["path"] = path
		}
		if host := q.Get("host"); host != "" {
			ws["headers"] = map[string]any{"Host": host}
		}
		if len(ws) > 0 {
			p["ws-opts"] = ws
		}
	case "grpc":
		if sn := q.Get("serviceName"); sn != "" {
			p["grpc-opts"] = map[string]any{"grpc-service-name": sn}
		}
	case "xhttp":
		// XHTTP (a.k.a. SplitHTTP) — heavily used by our own nodes. Without
		// mapping these opts the proxy parses but cannot connect.
		xo := map[string]any{}
		if path := q.Get("path"); path != "" {
			xo["path"] = path
		}
		if mode := q.Get("mode"); mode != "" {
			xo["mode"] = mode
		}
		if host := q.Get("host"); host != "" {
			xo["host"] = host
		}
		if len(xo) > 0 {
			p["xhttp-opts"] = xo
		}
	case "h2":
		h2 := map[string]any{}
		if path := q.Get("path"); path != "" {
			h2["path"] = path
		}
		if host := q.Get("host"); host != "" {
			h2["host"] = splitCSV(host) // mihomo h2-opts.host is a list
		}
		if len(h2) > 0 {
			p["h2-opts"] = h2
		}
	}
}

func applySSPlugin(p Proxy, plugin string) {
	parts := strings.Split(plugin, ";")
	name := parts[0]
	opts := map[string]any{}
	for _, kv := range parts[1:] {
		k, v := splitEq(kv)
		if k == "" {
			continue
		}
		opts[k] = v
	}
	switch {
	case strings.Contains(name, "obfs"):
		p["plugin"] = "obfs"
		mode := str(opts["obfs"])
		po := map[string]any{}
		if mode != "" {
			po["mode"] = mode
		}
		if h := str(opts["obfs-host"]); h != "" {
			po["host"] = h
		}
		p["plugin-opts"] = po
	case strings.Contains(name, "v2ray"):
		p["plugin"] = "v2ray-plugin"
		p["plugin-opts"] = opts
	default:
		p["plugin"] = name
		if len(opts) > 0 {
			p["plugin-opts"] = opts
		}
	}
}

func hostPort(u *url.URL) (string, int, error) {
	host := u.Hostname()
	port, _ := strconv.Atoi(u.Port())
	if host == "" || port == 0 {
		return "", 0, errors.New("missing server/port")
	}
	return host, port, nil
}

func nameFrom(u *url.URL, host string, port int) string {
	if n := decodeFragment(u.Fragment); n != "" {
		return n
	}
	return fmt.Sprintf("%s:%d", host, port)
}

func decodeFragment(frag string) string {
	if frag == "" {
		return ""
	}
	if dec, err := url.QueryUnescape(frag); err == nil {
		return strings.TrimSpace(dec)
	}
	return strings.TrimSpace(frag)
}

func decodeFlexible(s string) ([]byte, error) {
	s = strings.TrimSpace(s)
	for _, enc := range []*base64.Encoding{
		base64.StdEncoding, base64.RawStdEncoding,
		base64.URLEncoding, base64.RawURLEncoding,
	} {
		if dec, err := enc.DecodeString(s); err == nil {
			return dec, nil
		}
	}
	return nil, fmt.Errorf("not base64")
}

func dedupeNames(proxies []Proxy) {
	seen := map[string]int{}
	for _, p := range proxies {
		n, _ := p["name"].(string)
		if n == "" {
			n = "proxy"
			p["name"] = n
		}
		if c, ok := seen[n]; ok {
			seen[n] = c + 1
			p["name"] = fmt.Sprintf("%s #%d", n, c+1)
		} else {
			seen[n] = 1
		}
	}
}

func splitColon(s string) (string, string) {
	a, b, _ := strings.Cut(s, ":")
	return a, b
}

func splitEq(s string) (string, string) {
	a, b, _ := strings.Cut(s, "=")
	return strings.TrimSpace(a), strings.TrimSpace(b)
}

func splitHostPort(s string) (string, int) {
	// net.SplitHostPort handles IPv6 literals (`[2001:db8::1]:8388` → host
	// `2001:db8::1`, port 8388) as well as `host:port`. A naive Cut on the
	// first ':' would mangle IPv6 servers in legacy/SIP002 ss links.
	if h, p, err := net.SplitHostPort(s); err == nil {
		port, _ := strconv.Atoi(p)
		return h, port
	}
	// No port present (or unparseable) — strip any brackets and report port 0
	// so the caller's "missing server/port" guard fires.
	return strings.Trim(s, "[]"), 0
}

func splitCSV(s string) []string {
	var out []string
	for _, p := range strings.Split(s, ",") {
		if p = strings.TrimSpace(p); p != "" {
			out = append(out, p)
		}
	}
	return out
}

func orDefault(v, def string) string {
	if strings.TrimSpace(v) == "" {
		return def
	}
	return v
}

func firstNonEmpty(vals ...string) string {
	for _, v := range vals {
		if strings.TrimSpace(v) != "" {
			return v
		}
	}
	return ""
}

func firstNonEmptyLine(s string) string {
	for _, l := range strings.Split(normalizeNewlines(s), "\n") {
		if t := strings.TrimSpace(l); t != "" {
			return t
		}
	}
	return ""
}

func normalizeNewlines(s string) string {
	return strings.ReplaceAll(strings.ReplaceAll(s, "\r\n", "\n"), "\r", "\n")
}

func stripFragment(s string) string {
	if i := strings.IndexByte(s, '#'); i >= 0 {
		return s[:i]
	}
	return s
}

func str(v any) string {
	switch t := v.(type) {
	case string:
		return t
	case float64:
		return strconv.FormatFloat(t, 'f', -1, 64)
	case int:
		return strconv.Itoa(t)
	case nil:
		return ""
	default:
		return fmt.Sprintf("%v", t)
	}
}

func intish(v any) int {
	switch t := v.(type) {
	case float64:
		return int(t)
	case int:
		return t
	case string:
		n, _ := strconv.Atoi(strings.TrimSpace(t))
		return n
	default:
		return 0
	}
}

func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n] + "…"
}
