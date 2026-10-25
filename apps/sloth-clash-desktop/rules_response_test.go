package main

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestRulesOverviewPreservesLargeJSON(t *testing.T) {
	rules := make([]map[string]any, 2000)
	for i := range rules {
		rules[i] = map[string]any{"type": "DomainSuffix", "payload": strings.Repeat("домен", 25) + ".example", "proxy": "DIRECT"}
	}
	rulesBody, _ := json.Marshal(map[string]any{"rules": rules})
	providerBody, _ := json.Marshal(map[string]any{"providers": map[string]any{"large-provider": map[string]any{"ruleCount": 2000, "description": strings.Repeat("provider ", 2500)}}})
	if len(rulesBody) <= 256*1024 || len(providerBody) <= 10000 {
		t.Fatal("regression fixture must exceed both former truncation limits")
	}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer local-test" {
			t.Error("missing controller authorization")
		}
		if r.URL.Path == "/rules" {
			_, _ = w.Write(rulesBody)
		} else if r.URL.Path == "/providers/rules" {
			_, _ = w.Write(providerBody)
		} else {
			http.NotFound(w, r)
		}
	}))
	defer server.Close()
	t.Setenv("SLOTH_CLASH_CONTROLLER", server.URL)
	t.Setenv("SLOTH_CLASH_SECRET", "local-test")
	for _, embedded := range []bool{true, false} {
		a := &App{}
		if embedded {
			a.state.Connection.Status = "connected"
			a.core.listen = strings.TrimPrefix(server.URL, "http://")
			a.core.secret = "local-test"
		}
		out := a.FetchRulesOverview()
		if !out.Reachable || out.LastError != "" || out.RulesBody != string(rulesBody) || out.RuleProvidersBody != string(providerBody) {
			t.Fatalf("embedded=%t: incomplete JSON (rules=%d providers=%d error=%s)", embedded, len(out.RulesBody), len(out.RuleProvidersBody), out.LastError)
		}
		var decoded struct {
			Rules []json.RawMessage `json:"rules"`
		}
		if err := json.Unmarshal([]byte(out.RulesBody), &decoded); err != nil || len(decoded.Rules) != len(rules) {
			t.Fatalf("frontend cannot parse all rules: %d %v", len(decoded.Rules), err)
		}
	}
}

func TestRulesOverviewReportsInvalidAndOversizedResponses(t *testing.T) {
	if _, err := readRulesOverviewJSON(strings.NewReader(`{"rules":[`)); err == nil || err.Error() != "core returned invalid rules JSON" {
		t.Fatalf("incomplete JSON: %v", err)
	}
	// A VALID document at and past the cap: only the size check can reject the
	// oversized one, so this fails if that check is broken.
	doc := []byte(`{"rules":[{"type":"Match","payload":"","proxy":"DIRECT"}]}`)
	if got, err := readRulesOverviewJSON(bytes.NewReader(padJSON(t, doc, rulesOverviewMaxJSONBytes))); err != nil || len(got) != rulesOverviewMaxJSONBytes {
		t.Fatalf("at limit: %d bytes, %v", len(got), err)
	}
	if _, err := readRulesOverviewJSON(bytes.NewReader(padJSON(t, doc, rulesOverviewMaxJSONBytes+1))); err == nil || err.Error() != "rules response exceeds the 8 MiB limit" {
		t.Fatalf("oversized valid JSON: %v", err)
	}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/rules" {
			_, _ = w.Write([]byte(`{"rules":[]}`))
		} else {
			_, _ = w.Write([]byte(`{"providers":`))
		}
	}))
	defer server.Close()
	out := (&App{}).rulesOverviewFetch(strings.TrimPrefix(server.URL, "http://"), "")
	if !out.Reachable || out.RulesBody != `{"rules":[]}` || !strings.Contains(out.LastError, "GET /providers/rules: core returned invalid rules JSON") {
		t.Fatalf("lost the usable rules or provider error: %+v", out)
	}
}
