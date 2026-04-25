package main

import (
	"context"
	"strings"
	"testing"

	"gopkg.in/yaml.v3"
)

func TestBuildMieruSubscriptionYAMLSimple(t *testing.T) {
	t.Parallel()
	raw := "mieru://testuser:test12345@136.244.111.222:9000"
	norm, err := normalizeSubscriptionURL(raw)
	if err != nil {
		t.Fatal(err)
	}
	b, err := buildMieruSubscriptionYAML(norm)
	if err != nil {
		t.Fatal(err)
	}
	var doc map[string]any
	if err := yaml.Unmarshal(b, &doc); err != nil {
		t.Fatal(err)
	}
	proxies, _ := doc["proxies"].([]any)
	if len(proxies) != 1 {
		t.Fatalf("proxies len: %d", len(proxies))
	}
	p0, _ := proxies[0].(map[string]any)
	if p0["type"] != "mieru" || p0["server"] != "136.244.111.222" || p0["port"] != 9000 {
		t.Fatalf("proxy: %#v", p0)
	}
	if p0["username"] != "testuser" || p0["password"] != "test12345" {
		t.Fatalf("auth: %#v", p0)
	}
	if p0["transport"] != "TCP" {
		t.Fatalf("expected default transport TCP, got %#v", p0["transport"])
	}
}

func TestBuildMieruSubscriptionYAMLUDPFromQuery(t *testing.T) {
	t.Parallel()
	raw := "mieru://u:p@192.0.2.3:9000?udp=true"
	norm, err := normalizeSubscriptionURL(raw)
	if err != nil {
		t.Fatal(err)
	}
	b, err := buildMieruSubscriptionYAML(norm)
	if err != nil {
		t.Fatal(err)
	}
	var doc map[string]any
	if err := yaml.Unmarshal(b, &doc); err != nil {
		t.Fatal(err)
	}
	proxies, _ := doc["proxies"].([]any)
	p0, _ := proxies[0].(map[string]any)
	if p0["transport"] != "UDP" {
		t.Fatalf("expected UDP from ?udp=true, got %#v", p0["transport"])
	}
	if u, ok := p0["udp"].(bool); !ok || !u {
		t.Fatalf("expected udp true with UDP transport, got %#v", p0["udp"])
	}
}

func TestBuildMieruSubscriptionYAMLUDPBareUdpQuery(t *testing.T) {
	t.Parallel()
	raw := "mieru://u:p@192.0.2.4:9000?udp"
	norm, err := normalizeSubscriptionURL(raw)
	if err != nil {
		t.Fatal(err)
	}
	b, err := buildMieruSubscriptionYAML(norm)
	if err != nil {
		t.Fatal(err)
	}
	var doc map[string]any
	if err := yaml.Unmarshal(b, &doc); err != nil {
		t.Fatal(err)
	}
	proxies, _ := doc["proxies"].([]any)
	p0, _ := proxies[0].(map[string]any)
	if p0["transport"] != "UDP" {
		t.Fatalf("expected UDP for bare ?udp, got %#v", p0["transport"])
	}
}

func TestBuildMieruSubscriptionYAMLPasswordWithAt(t *testing.T) {
	t.Parallel()
	raw := "mieru://slothclash:1qaz@WSX3edc@136.244.111.222:9000"
	norm, err := normalizeSubscriptionURL(raw)
	if err != nil {
		t.Fatal(err)
	}
	b, err := buildMieruSubscriptionYAML(norm)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(b), "1qaz@WSX3edc") {
		t.Fatalf("expected password in yaml: %s", string(b))
	}
}

func TestFetchSubscriptionBodyMieruSkipsHTTP(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	b, err := fetchSubscriptionBody(ctx, "mieru://a:b@192.0.2.1:9001")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(b), "type: mieru") {
		t.Fatalf("expected mieru proxy yaml: %s", string(b))
	}
}

func TestPeekSubscriptionMieruNoHTTP(t *testing.T) {
	t.Parallel()
	p, err := peekSubscription(context.Background(), "mierus://u:p@192.0.2.2:9002")
	if err != nil {
		t.Fatal(err)
	}
	if p.HTTPStatus != 200 || p.SuggestedName == "" {
		t.Fatalf("peek: %+v", p)
	}
}
