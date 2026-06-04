package sharelink

import (
	"encoding/base64"
	"strings"
	"testing"
)

func b64(s string) string { return base64.StdEncoding.EncodeToString([]byte(s)) }

func TestParseVless(t *testing.T) {
	link := "vless://11111111-2222-3333-4444-555555555555@example.com:443?type=ws&security=tls&sni=example.com&path=/ws&host=h.example.com&flow=xtls-rprx-vision#My Vless"
	p, err := ParseLink(link)
	if err != nil {
		t.Fatalf("err: %v", err)
	}
	if p["type"] != "vless" || p["server"] != "example.com" || p["port"] != 443 {
		t.Fatalf("base fields wrong: %#v", p)
	}
	if p["uuid"] != "11111111-2222-3333-4444-555555555555" {
		t.Fatalf("uuid: %v", p["uuid"])
	}
	if p["tls"] != true || p["servername"] != "example.com" {
		t.Fatalf("tls/sni: %#v", p)
	}
	if p["network"] != "ws" {
		t.Fatalf("network: %v", p["network"])
	}
	ws, ok := p["ws-opts"].(map[string]any)
	if !ok || ws["path"] != "/ws" {
		t.Fatalf("ws-opts: %#v", p["ws-opts"])
	}
	if p["name"] != "My Vless" {
		t.Fatalf("name: %v", p["name"])
	}
	if p["flow"] != "xtls-rprx-vision" {
		t.Fatalf("flow: %v", p["flow"])
	}
}

func TestParseVlessReality(t *testing.T) {
	link := "vless://uuid@1.2.3.4:443?security=reality&pbk=PUBKEY&sid=ab12&fp=chrome&type=grpc&serviceName=gsvc#R"
	p, err := ParseLink(link)
	if err != nil {
		t.Fatalf("err: %v", err)
	}
	ro, ok := p["reality-opts"].(map[string]any)
	if !ok || ro["public-key"] != "PUBKEY" || ro["short-id"] != "ab12" {
		t.Fatalf("reality-opts: %#v", p["reality-opts"])
	}
	if p["client-fingerprint"] != "chrome" {
		t.Fatalf("fp: %v", p["client-fingerprint"])
	}
	grpc, ok := p["grpc-opts"].(map[string]any)
	if !ok || grpc["grpc-service-name"] != "gsvc" {
		t.Fatalf("grpc-opts: %#v", p["grpc-opts"])
	}
}

func TestParseVmess(t *testing.T) {
	j := `{"v":"2","ps":"My Vmess","add":"example.com","port":"443","id":"uuid-x","aid":"0","scy":"auto","net":"ws","host":"h.example.com","path":"/p","tls":"tls"}`
	link := "vmess://" + b64(j)
	p, err := ParseLink(link)
	if err != nil {
		t.Fatalf("err: %v", err)
	}
	if p["type"] != "vmess" || p["server"] != "example.com" || p["port"] != 443 {
		t.Fatalf("base: %#v", p)
	}
	if p["uuid"] != "uuid-x" || p["cipher"] != "auto" || p["alterId"] != 0 {
		t.Fatalf("vmess fields: %#v", p)
	}
	if p["tls"] != true || p["servername"] != "h.example.com" {
		t.Fatalf("tls: %#v", p)
	}
	ws, ok := p["ws-opts"].(map[string]any)
	if !ok || ws["path"] != "/p" {
		t.Fatalf("ws-opts: %#v", p["ws-opts"])
	}
}

func TestParseSSSIP002(t *testing.T) {
	link := "ss://" + b64("aes-256-gcm:secretpass") + "@example.com:8388#My SS"
	p, err := ParseLink(link)
	if err != nil {
		t.Fatalf("err: %v", err)
	}
	if p["type"] != "ss" || p["cipher"] != "aes-256-gcm" || p["password"] != "secretpass" {
		t.Fatalf("ss fields: %#v", p)
	}
	if p["server"] != "example.com" || p["port"] != 8388 {
		t.Fatalf("server/port: %#v", p)
	}
	if p["name"] != "My SS" {
		t.Fatalf("name: %v", p["name"])
	}
}

func TestParseSSLegacy(t *testing.T) {
	link := "ss://" + b64("aes-128-gcm:pw@example.com:8388") + "#Legacy"
	p, err := ParseLink(link)
	if err != nil {
		t.Fatalf("err: %v", err)
	}
	if p["cipher"] != "aes-128-gcm" || p["password"] != "pw" || p["port"] != 8388 {
		t.Fatalf("legacy ss: %#v", p)
	}
}

func TestParseTrojan(t *testing.T) {
	link := "trojan://mypassword@example.com:443?sni=example.com&allowInsecure=1#T"
	p, err := ParseLink(link)
	if err != nil {
		t.Fatalf("err: %v", err)
	}
	if p["type"] != "trojan" || p["password"] != "mypassword" || p["sni"] != "example.com" {
		t.Fatalf("trojan: %#v", p)
	}
	if p["skip-cert-verify"] != true {
		t.Fatalf("insecure: %#v", p)
	}
}

func TestParseHysteria2(t *testing.T) {
	link := "hysteria2://pass@example.com:443?sni=example.com&obfs=salamander&obfs-password=xyz&insecure=1#H"
	p, err := ParseLink(link)
	if err != nil {
		t.Fatalf("err: %v", err)
	}
	if p["type"] != "hysteria2" || p["password"] != "pass" || p["obfs"] != "salamander" {
		t.Fatalf("hy2: %#v", p)
	}
	if p["obfs-password"] != "xyz" || p["skip-cert-verify"] != true {
		t.Fatalf("hy2 opts: %#v", p)
	}
}

func TestParseTuic(t *testing.T) {
	link := "tuic://uuid-1:password-1@example.com:443?sni=example.com&congestion_control=bbr&udp_relay_mode=native#U"
	p, err := ParseLink(link)
	if err != nil {
		t.Fatalf("err: %v", err)
	}
	if p["type"] != "tuic" || p["uuid"] != "uuid-1" || p["password"] != "password-1" {
		t.Fatalf("tuic: %#v", p)
	}
	if p["congestion-controller"] != "bbr" || p["udp-relay-mode"] != "native" {
		t.Fatalf("tuic opts: %#v", p)
	}
}

func TestParseManyAndDedupe(t *testing.T) {
	text := strings.Join([]string{
		"trojan://pw@a.com:443#dup",
		"trojan://pw@b.com:443#dup",
		"garbage://nope",
		"# comment",
		"",
	}, "\n")
	proxies, skipped := ParseMany(text)
	if len(proxies) != 2 {
		t.Fatalf("want 2 proxies, got %d", len(proxies))
	}
	if proxies[0]["name"] == proxies[1]["name"] {
		t.Fatalf("names not deduped: %v / %v", proxies[0]["name"], proxies[1]["name"])
	}
	if len(skipped) != 1 || !strings.HasPrefix(skipped[0], "garbage://") {
		t.Fatalf("skipped: %#v", skipped)
	}
}

func TestLooksLikeShareLinksAndBase64Block(t *testing.T) {
	plain := "vless://uuid@a.com:443#x\ntrojan://pw@b.com:443#y"
	if !LooksLikeShareLinks(plain) {
		t.Fatal("plain should look like share links")
	}
	enc := base64.StdEncoding.EncodeToString([]byte(plain))
	if !LooksLikeShareLinks(enc) {
		t.Fatal("base64 envelope should look like share links")
	}
	if got := DecodeBase64Block(enc); !strings.Contains(got, "vless://") {
		t.Fatalf("decode block: %q", got)
	}
	if LooksLikeShareLinks("proxies:\n  - {}") {
		t.Fatal("clash yaml should not look like share links")
	}
}

func TestUnsupportedScheme(t *testing.T) {
	if _, err := ParseLink("ssr://whatever"); err == nil {
		t.Fatal("expected error for ssr")
	}
}
