package runtimecfgtest

import (
	"bytes"
	"encoding/base64"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
)

// Realistic large-subscription fixtures used to size subscriptionMaxBodyBytes.
// Shapes follow what commercial panels (Remnawave, Marzban, 3x-ui exports)
// actually ship: block-style YAML with every transport option spelled out,
// long flag-prefixed names, several groups that each list every node, and an
// inline rule list. Each node has unique credentials so nothing compresses
// into repetition the way a toy fixture would.

var realisticSubscriptionCountries = []string{
	"🇩🇪 Germany Frankfurt", "🇳🇱 Netherlands Amsterdam", "🇫🇮 Finland Helsinki",
	"🇺🇸 United States New York", "🇯🇵 Japan Tokyo", "🇸🇬 Singapore",
	"🇬🇧 United Kingdom London", "🇰🇿 Kazakhstan Almaty", "🇹🇷 Türkiye Istanbul",
	"🇵🇱 Poland Warsaw",
}

func realisticSubscriptionNodeName(i int) string {
	kinds := []string{"VLESS Reality", "VMess WS TLS", "Shadowsocks 2022", "Hysteria2"}
	return fmt.Sprintf("%s %04d | %s | x1.5", realisticSubscriptionCountries[i%len(realisticSubscriptionCountries)], i, kinds[i%len(kinds)])
}

func realisticSubscriptionUUID(i int) string {
	return fmt.Sprintf("%08x-%04x-4%03x-a%03x-%012x", 0x5f3a0000+i, i%0xffff, i%0xfff, (i*7)%0xfff, 0x1a2b3c4d0000+i*31)
}

func realisticSubscriptionNode(b *strings.Builder, i int) {
	name := realisticSubscriptionNodeName(i)
	host := fmt.Sprintf("node-%04d.edge-%02d.example-provider.net", i, i%37)
	switch i % 4 {
	case 0:
		fmt.Fprintf(b, "  - name: %q\n    type: vless\n    server: %s\n    port: 443\n    uuid: %s\n"+
			"    network: tcp\n    tls: true\n    udp: true\n    flow: xtls-rprx-vision\n"+
			"    servername: www.microsoft.com\n    client-fingerprint: chrome\n    skip-cert-verify: false\n"+
			"    reality-opts:\n      public-key: %s\n      short-id: %08x\n",
			name, host, realisticSubscriptionUUID(i),
			base64.RawURLEncoding.EncodeToString([]byte(fmt.Sprintf("reality-public-key-%026d", i))), i*2654435761%0xffffffff)
	case 1:
		fmt.Fprintf(b, "  - name: %q\n    type: vmess\n    server: %s\n    port: 443\n    uuid: %s\n"+
			"    alterId: 0\n    cipher: auto\n    udp: true\n    tls: true\n    skip-cert-verify: false\n"+
			"    servername: %s\n    network: ws\n    ws-opts:\n      path: /api/v2/stream/%08x?ed=2048\n"+
			"      headers:\n        Host: %s\n",
			name, host, realisticSubscriptionUUID(i), host, i*40503, host)
	case 2:
		fmt.Fprintf(b, "  - name: %q\n    type: ss\n    server: %s\n    port: %d\n    cipher: 2022-blake3-aes-256-gcm\n"+
			"    password: %s\n    udp: true\n    udp-over-tcp: false\n",
			name, host, 20000+i%40000,
			base64.StdEncoding.EncodeToString([]byte(fmt.Sprintf("ss2022-server-key-%014d:user%013d", i, i*13))))
	default:
		fmt.Fprintf(b, "  - name: %q\n    type: hysteria2\n    server: %s\n    port: 8443\n    ports: 20000-50000\n"+
			"    password: %s\n    up: 50 Mbps\n    down: 200 Mbps\n    sni: %s\n    skip-cert-verify: false\n"+
			"    obfs: salamander\n    obfs-password: %s\n    alpn:\n      - h3\n",
			name, host, realisticSubscriptionUUID(i), host, realisticSubscriptionUUID(i*3+1))
	}
}

// RealisticSubscriptionYAML builds a full-profile subscription with n nodes,
// five groups that each list every node, plus per-country url-test groups and
// the inline rule list panels ship (geosite/geoip + a few thousand domains).
func RealisticSubscriptionYAML(n int) []byte {
	var b strings.Builder
	b.WriteString("mixed-port: 7890\nallow-lan: false\nmode: rule\nlog-level: info\nipv6: true\n")
	b.WriteString("dns:\n  enable: true\n  enhanced-mode: fake-ip\n  nameserver:\n    - https://1.1.1.1/dns-query\n    - https://8.8.8.8/dns-query\n")
	b.WriteString("proxies:\n")
	for i := 0; i < n; i++ {
		realisticSubscriptionNode(&b, i)
	}
	b.WriteString("proxy-groups:\n")
	for _, g := range []struct{ name, kind string }{
		{"🚀 Proxy", "select"}, {"⚡ Auto", "url-test"}, {"🔁 Fallback", "fallback"},
		{"⚖ Balance", "load-balance"}, {"🎬 Streaming", "select"},
	} {
		fmt.Fprintf(&b, "  - name: %q\n    type: %s\n", g.name, g.kind)
		if g.kind != "select" {
			b.WriteString("    url: https://www.gstatic.com/generate_204\n    interval: 300\n    tolerance: 50\n")
		}
		b.WriteString("    proxies:\n")
		for i := 0; i < n; i++ {
			fmt.Fprintf(&b, "      - %q\n", realisticSubscriptionNodeName(i))
		}
	}
	for c, country := range realisticSubscriptionCountries {
		fmt.Fprintf(&b, "  - name: %q\n    type: url-test\n    url: https://www.gstatic.com/generate_204\n    interval: 300\n    proxies:\n", country)
		for i := c; i < n; i += len(realisticSubscriptionCountries) {
			fmt.Fprintf(&b, "      - %q\n", realisticSubscriptionNodeName(i))
		}
	}
	b.WriteString("rules:\n")
	for i := 0; i < 3000; i++ {
		fmt.Fprintf(&b, "  - DOMAIN-SUFFIX,service-%04d.example-cdn.com,🚀 Proxy\n", i)
	}
	b.WriteString("  - GEOSITE,category-ads-all,REJECT\n  - GEOIP,private,DIRECT,no-resolve\n  - MATCH,🚀 Proxy\n")
	return []byte(b.String())
}

// PadYAML right-pads a YAML document with a trailing comment line to exactly
// size bytes, so the padded body still parses to the same document.
func PadYAML(t *testing.T, body []byte, size int) []byte {
	t.Helper()
	if len(body)+2 > size || !bytes.HasSuffix(body, []byte("\n")) {
		t.Fatalf("fixture of %d bytes cannot be padded to %d", len(body), size)
	}
	out := append(append([]byte(nil), body...), '#')
	out = append(out, bytes.Repeat([]byte("x"), size-len(body)-2)...)
	return append(out, '\n')
}

// ServeSubscription serves whatever *body holds at the moment of the request.
func ServeSubscription(t *testing.T, body *atomic.Pointer[[]byte]) string {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "text/yaml")
		_, _ = w.Write(*body.Load())
	}))
	t.Cleanup(srv.Close)
	return srv.URL + "/sub"
}
