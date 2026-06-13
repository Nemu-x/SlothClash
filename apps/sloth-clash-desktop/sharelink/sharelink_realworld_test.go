package sharelink

// Real-world share-link tests modeled on SlothClash's OWN production nodes
// (yachiru.xyz: vless + reality + xtls-rprx-vision, xhttp transport, xudp
// packet-encoding). These encode the CORRECT mihomo output we expect.
//
// Tests that currently FAIL are not noise — they are the proven bug list for
// the share-link parser (see architecture/sharelink-parsing.md fragility
// findings). They should go green together with the parser fix on the
// fix/sharelink-parsing branch.

import "testing"

// helper: parse and require no error
func mustParse(t *testing.T, link string) Proxy {
	t.Helper()
	p, err := ParseLink(link)
	if err != nil {
		t.Fatalf("ParseLink(%q) error: %v", link, err)
	}
	return p
}

func wantField(t *testing.T, p Proxy, key string, want any) {
	t.Helper()
	got, ok := p[key]
	if !ok {
		t.Errorf("missing field %q (want %v)", key, want)
		return
	}
	if got != want {
		t.Errorf("field %q = %v (%T), want %v (%T)", key, got, got, want, want)
	}
}

// vless + reality + xtls-rprx-vision over TCP — the "🚀" nodes. Core fields
// MUST all map. (Expected GREEN with the current parser.)
func TestRealWorldVlessRealityVisionTCP(t *testing.T) {
	link := "vless://2dcc5c5a-7ad4-441f-85b8-f2e82bc85dec@r1-ro.yachiru.xyz:443" +
		"?type=tcp&security=reality&pbk=ky3amiQnMnNmwVY2oCMTvNgn0eISsdtz49hPEta3Ij4" +
		"&sid=8d7e666a4421c908&flow=xtls-rprx-vision&fp=chrome&sni=r1-ro.yachiru.xyz#RO"
	p := mustParse(t, link)
	wantField(t, p, "type", "vless")
	wantField(t, p, "server", "r1-ro.yachiru.xyz")
	wantField(t, p, "port", 443)
	wantField(t, p, "uuid", "2dcc5c5a-7ad4-441f-85b8-f2e82bc85dec")
	wantField(t, p, "network", "tcp")
	wantField(t, p, "flow", "xtls-rprx-vision")
	wantField(t, p, "tls", true)
	wantField(t, p, "servername", "r1-ro.yachiru.xyz")
	wantField(t, p, "client-fingerprint", "chrome")
	ro, ok := p["reality-opts"].(map[string]any)
	if !ok {
		t.Fatalf("reality-opts missing/wrong type: %#v", p["reality-opts"])
	}
	if ro["public-key"] != "ky3amiQnMnNmwVY2oCMTvNgn0eISsdtz49hPEta3Ij4" {
		t.Errorf("reality public-key = %v", ro["public-key"])
	}
	if ro["short-id"] != "8d7e666a4421c908" {
		t.Errorf("reality short-id = %v", ro["short-id"])
	}
}

// vless + reality over XHTTP — the "🔒" nodes. The transport opts MUST survive.
// (Expected RED today: applyTransport ignores xhttp → xhttp-opts dropped.)
func TestRealWorldVlessRealityXHTTP(t *testing.T) {
	link := "vless://2dcc5c5a-7ad4-441f-85b8-f2e82bc85dec@r2-ro.yachiru.xyz:443" +
		"?type=xhttp&security=reality&pbk=ky3amiQnMnNmwVY2oCMTvNgn0eISsdtz49hPEta3Ij4" +
		"&sid=8d7e666a4421c908&fp=chrome&sni=r2-ro.yachiru.xyz&path=%2F&mode=auto#RO-xhttp"
	p := mustParse(t, link)
	wantField(t, p, "network", "xhttp")
	opts, ok := p["xhttp-opts"].(map[string]any)
	if !ok {
		t.Fatalf("xhttp-opts missing/wrong type: %#v (xhttp transport silently dropped)", p["xhttp-opts"])
	}
	if opts["path"] != "/" {
		t.Errorf("xhttp-opts.path = %v, want /", opts["path"])
	}
	if opts["mode"] != "auto" {
		t.Errorf("xhttp-opts.mode = %v, want auto", opts["mode"])
	}
}

// packet-encoding=xudp must carry through for vless (their nodes set it; UDP
// degrades without it). (Expected RED today: packetEncoding not parsed.)
func TestRealWorldVlessPacketEncodingXudp(t *testing.T) {
	link := "vless://uuid-x@r1-nl.yachiru.xyz:443?type=tcp&security=reality" +
		"&pbk=PBK&sid=SID&flow=xtls-rprx-vision&sni=r1-nl.yachiru.xyz&packetEncoding=xudp#NL"
	p := mustParse(t, link)
	wantField(t, p, "packet-encoding", "xudp")
}

// ss with an IPv6 literal server (SIP002). The naive host:port splitter used
// to mangle the address; net.SplitHostPort handles the brackets.
func TestRealWorldSSIPv6SIP002(t *testing.T) {
	link := "ss://" + b64("aes-256-gcm:secretpass") + "@[2001:db8::1]:8388#v6"
	p := mustParse(t, link)
	wantField(t, p, "server", "2001:db8::1")
	wantField(t, p, "port", 8388)
	wantField(t, p, "cipher", "aes-256-gcm")
	wantField(t, p, "password", "secretpass")
}

// ss password containing ':' must survive (method is the token before the
// FIRST colon; the rest is the password). Guards against a regression to a
// last-colon split.
func TestRealWorldSSPasswordWithColon(t *testing.T) {
	link := "ss://" + b64("aes-256-gcm:pa:ss:word") + "@h.example.com:8388#c"
	p := mustParse(t, link)
	wantField(t, p, "cipher", "aes-256-gcm")
	wantField(t, p, "password", "pa:ss:word")
}

// vmess with JSON boolean tls + sni. PASSES because the sni branch sets
// tls=true — NOT because we handle the bool. The pure bool-without-sni case is
// a known minor gap (rare: real tls nodes carry sni). Kept to lock in the
// realistic case; see architecture/sharelink-parsing.md.
func TestRealWorldVmessTLSBoolean(t *testing.T) {
	// {"v":"2","ps":"vm","add":"vm.example.com","port":"443","id":"u","aid":"0","net":"ws","tls":true,"sni":"vm.example.com","path":"/p"}
	link := "vmess://eyJ2IjoiMiIsInBzIjoidm0iLCJhZGQiOiJ2bS5leGFtcGxlLmNvbSIsInBvcnQiOiI0NDMiLCJpZCI6InUiLCJhaWQiOiIwIiwibmV0Ijoid3MiLCJ0bHMiOnRydWUsInNuaSI6InZtLmV4YW1wbGUuY29tIiwicGF0aCI6Ii9wIn0="
	p := mustParse(t, link)
	wantField(t, p, "tls", true)
}
