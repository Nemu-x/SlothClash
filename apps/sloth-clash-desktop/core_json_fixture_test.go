package main

import (
	"encoding/json"
	"fmt"
	"strings"
	"time"
)

// Fixtures shaped like mihomo's controller output (tunnel/statistic TrackerInfo
// and adapter Proxy.MarshalJSON). Every field the core emits is present, with
// deliberately long-but-plausible values, so the measured sizes are an upper
// estimate of what a real TUN session produces.

func realisticMihomoConnection(i int) map[string]any {
	start := time.Date(2026, 10, 25, 12, 34, 56, 789012345, time.FixedZone("", 3*3600))
	host := fmt.Sprintf("rr%d---sn-4g5lznek-%04d.googlevideo.com", i%10, i%10000)
	return map[string]any{
		"id": fmt.Sprintf("8f2b6c1e-%04x-4d3a-9b7e-%012x", i%0xffff, i),
		"metadata": map[string]any{
			"network":           "tcp",
			"type":              "Tun",
			"sourceIP":          "198.18.0.1",
			"destinationIP":     fmt.Sprintf("142.250.%d.%d", i%256, (i/256)%256),
			"sourceGeoIP":       nil,
			"destinationGeoIP":  []string{"US"},
			"sourceIPASN":       "",
			"destinationIPASN":  "AS15169 Google LLC",
			"sourcePort":        fmt.Sprint(49152 + i%16384),
			"destinationPort":   "443",
			"inboundIP":         "",
			"inboundPort":       "0",
			"inboundName":       "DEFAULT-TUN",
			"inboundUser":       "",
			"host":              host,
			"dnsMode":           "fake-ip",
			"uid":               0,
			"process":           "chrome.exe",
			"processPath":       `C:\Program Files\Google\Chrome\Application\chrome.exe`,
			"specialProxy":      "",
			"specialRules":      "",
			"remoteDestination": fmt.Sprintf("142.250.%d.%d", i%256, (i/256)%256),
			"dscp":              0,
			"sniffHost":         host,
		},
		"upload":         int64(1234567 + i),
		"download":       int64(987654321 + i),
		"start":          start.Format(time.RFC3339Nano),
		"chains":         []string{fmt.Sprintf("🇩🇪 Germany Frankfurt VLESS Reality %03d", i%500), "🚀 Proxy"},
		"providerChains": []string{"subscription-main", ""},
		"rule":           "RuleSet",
		"rulePayload":    "geosite-youtube",
	}
}

func realisticMihomoConnectionsBody(n int) []byte {
	conns := make([]map[string]any, n)
	for i := range conns {
		conns[i] = realisticMihomoConnection(i)
	}
	body, err := json.Marshal(map[string]any{
		"downloadTotal": int64(123456789012),
		"uploadTotal":   int64(9876543210),
		"connections":   conns,
		"memory":        int64(187654321),
	})
	if err != nil {
		panic(err)
	}
	return body
}

// mihomo keeps the last 10 delay results per proxy (defaultHistoriesNum).
func realisticMihomoHistory() []map[string]any {
	h := make([]map[string]any, 10)
	at := time.Date(2026, 10, 25, 12, 0, 0, 123456789, time.FixedZone("", 3*3600))
	for i := range h {
		h[i] = map[string]any{"time": at.Add(time.Duration(i) * time.Minute).Format(time.RFC3339Nano), "delay": 120 + i}
	}
	return h
}

func realisticMihomoProxyName(i int) string {
	return fmt.Sprintf("🇩🇪 Germany Frankfurt VLESS Reality %05d", i)
}

func realisticMihomoProxy(i int) map[string]any {
	history := realisticMihomoHistory()
	return map[string]any{
		"alive":        true,
		"dialer-proxy": "",
		"extra": map[string]any{
			"https://www.gstatic.com/generate_204":  map[string]any{"alive": true, "history": history},
			"http://cp.cloudflare.com/generate_204": map[string]any{"alive": true, "history": history},
		},
		"history":      history,
		"id":           fmt.Sprintf("3c9a1f0e-%04x-4b2d-8e6f-%012x", i%0xffff, i),
		"interface":    "",
		"mptcp":        false,
		"name":         realisticMihomoProxyName(i),
		"routing-mark": 0,
		"smux":         false,
		"tfo":          false,
		"type":         "Vless",
		"udp":          true,
		"uot":          false,
		"xudp":         true,
	}
}

func realisticMihomoGroup(name, kind string, members []string) map[string]any {
	g := realisticMihomoProxy(0)
	g["name"] = name
	g["type"] = kind
	g["all"] = members
	g["now"] = members[0]
	g["testUrl"] = "https://www.gstatic.com/generate_204"
	g["expectedStatus"] = "*"
	g["fixed"] = ""
	g["hidden"] = false
	g["icon"] = "https://raw.githubusercontent.com/Koolson/Qure/master/IconSet/Color/Proxy.png"
	return g
}

// realisticMihomoProxiesBody returns a /proxies body with n leaf proxies and
// the usual subscription layout: a selector plus auto/fallback/load-balance
// groups that each list every node.
func realisticMihomoProxiesBody(n int) []byte {
	proxies := make(map[string]any, n+8)
	members := make([]string, n)
	for i := 0; i < n; i++ {
		name := realisticMihomoProxyName(i)
		members[i] = name
		proxies[name] = realisticMihomoProxy(i)
	}
	for _, builtin := range []string{"DIRECT", "REJECT", "REJECT-DROP", "PASS", "COMPATIBLE"} {
		p := realisticMihomoProxy(0)
		p["name"], p["type"] = builtin, strings.ToTitle(builtin[:1])+strings.ToLower(builtin[1:])
		proxies[builtin] = p
	}
	proxies["⚡ Auto"] = realisticMihomoGroup("⚡ Auto", "URLTest", members)
	proxies["🛟 Fallback"] = realisticMihomoGroup("🛟 Fallback", "Fallback", members)
	proxies["⚖️ Balance"] = realisticMihomoGroup("⚖️ Balance", "LoadBalance", members)
	proxies["🚀 Proxy"] = realisticMihomoGroup("🚀 Proxy", "Selector", append([]string{"⚡ Auto", "🛟 Fallback", "⚖️ Balance", "DIRECT"}, members...))
	proxies["GLOBAL"] = realisticMihomoGroup("GLOBAL", "Selector", append([]string{"🚀 Proxy", "⚡ Auto", "DIRECT"}, members...))
	body, err := json.Marshal(map[string]any{"proxies": proxies})
	if err != nil {
		panic(err)
	}
	return body
}
