// Package runtimecfgtest holds the config-generation test fixtures shared by
// package main's tests and internal/runtimecfg's own: representative and
// realistic subscription bodies, a subscription test server and the core
// binary lookup for real-core acceptance checks. Only tests import it.
package runtimecfgtest

import (
	"os"
	"testing"

	"gopkg.in/yaml.v3"
)

// RepresentativeFullProfile models a real provider subscription (like the
// 2026-06-12 case): proxies + groups + rules + a custom dns block + a tun block
// + sniffer + geo fields. dns.listen is intentionally ABSENT so we exercise our
// default. This is the input to the parity audit (config-divergence-audit).
func RepresentativeFullProfile() map[string]any {
	return map[string]any{
		"mode":           "rule",
		"ipv6":           true,
		"tcp-concurrent": true,
		"unified-delay":  true,
		// sub-provided geo cadence — must be preserved (not overridden).
		"geodata-mode":        true,
		"geo-auto-update":     true,
		"geo-update-interval": 72,
		"dns": map[string]any{
			"enable":                  true,
			"enhanced-mode":           "fake-ip",
			"fake-ip-range":           "198.18.0.1/16",
			"respect-rules":           true,
			"ipv6":                    true,
			"use-hosts":               true,
			"fake-ip-filter":          []any{"+.ru", "*.lan"},
			"nameserver":              []any{"https://1.1.1.1/dns-query", "https://77.88.8.8/dns-query"},
			"proxy-server-nameserver": []any{"1.1.1.1", "8.8.8.8", "77.88.8.8"},
			"nameserver-policy": map[string]any{
				"+.ru": []any{"https://77.88.8.8/dns-query"},
			},
			// NOTE: no "listen" — exercises our default.
		},
		"tun": map[string]any{
			"stack":                 "mixed",
			"auto-route":            true,
			"auto-detect-interface": true,
			"strict-route":          true,
			"dns-hijack":            []any{"any:53"},
			"enable":                false,
		},
		"sniffer": map[string]any{"enable": true},
		"proxies": []any{
			map[string]any{"name": "n1", "type": "ss", "server": "s.example.com", "port": 443, "cipher": "aes-128-gcm", "password": "pw"},
		},
		"proxy-groups": []any{
			map[string]any{"name": "PROXY", "type": "select", "proxies": []any{"n1", "DIRECT"}},
		},
		"rules": []any{"MATCH,PROXY"},
	}
}

// DNSMap returns m's dns block, failing t when it is missing.
func DNSMap(t *testing.T, m map[string]any) map[string]any {
	t.Helper()
	d, ok := m["dns"].(map[string]any)
	if !ok {
		t.Fatalf("dns block missing/wrong type: %#v", m["dns"])
	}
	return d
}

// ReadYAMLMap reads and parses the YAML mapping at p, failing t on error.
func ReadYAMLMap(t *testing.T, p string) map[string]any {
	t.Helper()
	b, err := os.ReadFile(p)
	if err != nil {
		t.Fatalf("read config failed: %v", err)
	}
	var out map[string]any
	if err := yaml.Unmarshal(b, &out); err != nil {
		t.Fatalf("yaml unmarshal failed: %v", err)
	}
	return out
}
