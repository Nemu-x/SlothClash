package main

import "testing"

// parseClashDocToMapReport must keep the good nodes AND report the share-link
// lines it could not convert, so import paths can say "imported N, skipped M"
// instead of silently dropping nodes (the original bug: skipped was discarded).
func TestParseClashDocReportsSkippedShareLinks(t *testing.T) {
	text := "vless://11111111-2222-3333-4444-555555555555@a.example.com:443?type=tcp#ok\n" +
		"garbage://nope\n" +
		"ss://bad\n"
	doc, skipped, err := parseClashDocToMapReport([]byte(text))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	px, _ := doc["proxies"].([]any)
	if len(px) != 1 {
		t.Fatalf("imported proxies = %d, want 1", len(px))
	}
	if len(skipped) != 2 {
		t.Fatalf("skipped = %d (%v), want 2", len(skipped), skipped)
	}
}

// A clean Clash-YAML doc reports no skipped lines (skipped is share-link-only).
func TestParseClashDocCleanYAMLReportsNoSkipped(t *testing.T) {
	yaml := "proxies:\n  - {name: a, type: ss, server: s.example.com, port: 443, cipher: aes-128-gcm, password: pw}\n"
	doc, skipped, err := parseClashDocToMapReport([]byte(yaml))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if doc == nil {
		t.Fatal("doc is nil")
	}
	if len(skipped) != 0 {
		t.Fatalf("skipped = %v, want none for clean YAML", skipped)
	}
}
