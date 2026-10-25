package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"gopkg.in/yaml.v3"
)

const testXDriveEncryption = "mlkem768x25519plus.native.0rtt.XKbaq1BuzJKKKPxZbwgRpn-gYQbnhrYketgezASCL1I"

func xdriveProxy(name, service string, extra map[string]any) map[string]any {
	opts := map[string]any{"service": service, "remote-folder": "/tmp/sloth-xdrive-" + name}
	for k, v := range extra {
		opts[k] = v
	}
	return map[string]any{
		"name": name, "type": "vless", "server": "127.0.0.1", "port": 443,
		"uuid": "27848739-7e62-4138-9fd3-098a63964b6b", "network": "xdrive",
		"encryption": testXDriveEncryption, "xdrive-opts": opts,
	}
}

func xdriveHardeningProfile() map[string]any {
	// Spellings the core's decoder accepts must not hide a node from the check.
	sneaky := map[string]any{
		"name": "sneaky", "type": "vless", "server": "127.0.0.1", "port": 443,
		"uuid": "27848739-7e62-4138-9fd3-098a63964b6b", "Network": "XDrive",
		"encryption":  testXDriveEncryption,
		"Xdrive_Opts": map[string]any{"Service": " Local ", "remote-folder": "/tmp/x"},
	}
	return map[string]any{
		"proxies": []any{
			xdriveProxy("local-node", "local", nil),
			xdriveProxy("drive-node", "google-drive", map[string]any{"secrets": []any{"id", "secret", "refresh"}}),
			sneaky,
		},
		"proxy-providers": map[string]any{
			"inline": map[string]any{"type": "inline", "Payload": []any{xdriveProxy("inline-local", "local", nil)}},
		},
		"listeners": []any{
			map[string]any{"name": "xin", "type": "vless", "port": 10001, "xdrive-config": map[string]any{"service": "google-drive"}},
			map[string]any{"name": "mixed-in", "type": "mixed", "port": 10002},
		},
		"proxy-groups": []any{
			map[string]any{"name": "PROXY", "type": "select", "proxies": []any{"local-node", "drive-node", "sneaky"}, "use": []any{"inline"}},
		},
		"rules": []any{"MATCH,PROXY"},
	}
}

func TestXDriveHardeningInPipeline(t *testing.T) {
	withConnectionPrefs(t, ConnectionSettings{})
	m := xdriveHardeningProfile()
	if err := finalizeRuntimeConfigPipeline(m, t.TempDir(), 54333, 9097, "secret", "tun", true, true); err != nil {
		t.Fatalf("pipeline: %v", err)
	}
	assertXDriveHardened(t, m)
}

// A profile script runs after the overlay; it must not be able to bring the
// XDrive surface back.
func TestXDriveHardeningSurvivesScript(t *testing.T) {
	withConnectionPrefs(t, ConnectionSettings{})
	m := xdriveHardeningProfile()
	var res scriptResult
	script := `
function main(config) {
  config.listeners = [{ name: 'back', type: 'vless', port: 10003, 'xdrive-config': { service: 'google-drive' } }]
  config.proxies.push({ name: 'script-local', type: 'vless', server: '127.0.0.1', port: 443,
    uuid: '27848739-7e62-4138-9fd3-098a63964b6b', network: 'xdrive', encryption: '` + testXDriveEncryption + `',
    'xdrive-opts': { service: 'local', 'remote-folder': '/tmp/s' } })
  return config
}`
	if err := finalizeRuntimeConfigPipelineWithScript(m, t.TempDir(), 54333, 9097, "secret", "tun", true, true, script, &res, currentRuntimeConfigPrefs()); err != nil {
		t.Fatalf("pipeline: %v", err)
	}
	if !res.Applied {
		t.Fatalf("script did not apply: %s", res.Err)
	}
	if _, ok := m["listeners"]; ok {
		t.Errorf("script re-added an XDrive listener: %#v", m["listeners"])
	}
	for _, it := range m["proxies"].([]any) {
		pm := it.(map[string]any)
		if pm["name"] == "script-local" && pm["type"] != "reject" {
			t.Errorf("script-local survived as %#v", pm)
		}
	}
}

func assertXDriveHardened(t *testing.T, m map[string]any) {
	t.Helper()
	byName := map[string]map[string]any{}
	for _, it := range m["proxies"].([]any) {
		pm := it.(map[string]any)
		byName[pm["name"].(string)] = pm
	}
	for _, name := range []string{"local-node", "sneaky"} {
		pm := byName[name]
		if pm == nil || pm["type"] != "reject" || len(pm) != 2 {
			t.Errorf("%s: want a bare REJECT proxy of the same name, got %#v", name, pm)
		}
	}
	if drive := byName["drive-node"]; drive == nil || drive["network"] != "xdrive" {
		t.Errorf("a remote-storage XDrive node must pass untouched: %#v", drive)
	}
	inline := m["proxy-providers"].(map[string]any)["inline"].(map[string]any)["Payload"].([]any)
	if p := inline[0].(map[string]any); p["type"] != "reject" || p["name"] != "inline-local" {
		t.Errorf("inline provider payload not neutralised: %#v", p)
	}
	listeners, _ := m["listeners"].([]any)
	if len(listeners) != 1 || listeners[0].(map[string]any)["name"] != "mixed-in" {
		t.Errorf("listeners = %#v, want only mixed-in", listeners)
	}
}

func TestXDriveHardeningDropsEmptyListeners(t *testing.T) {
	m := map[string]any{"listeners": []any{map[string]any{"name": "x", "XDrive_Config": map[string]any{}}}}
	applyXDriveHardening(m)
	if _, ok := m["listeners"]; ok {
		t.Fatalf("listeners left behind: %#v", m["listeners"])
	}
}

func TestRedactXDriveCredentials(t *testing.T) {
	template := map[string]any{
		"auth": map[string]any{"type": "static", "token": "T0KEN", "extra": []any{"a", "b"}},
		"put": map[string]any{
			"url":     "https://store.example/upload?name={name}&access_token=SECRET1&sig=SECRET2",
			"headers": map[string]any{"Authorization": "Bearer SECRET3", "X-Api-Key": "SECRET4", "Content-Type": "application/octet-stream"},
			"body":    `{"client_secret":"SECRET5","data":"{data}"}`,
		},
		"get": map[string]any{"url": "https://store.example/{name}", "body": "plain {data}"},
	}
	m := map[string]any{
		"proxies": []any{
			xdriveProxy("drive", "google-drive", map[string]any{"secrets": []any{"CLIENT_ID", "CLIENT_SECRET", "REFRESH"}}),
			xdriveProxy("tpl", "template", map[string]any{"template": template}),
			map[string]any{"name": "plain", "type": "ss", "password": "keep-me"},
		},
		"proxy-providers": map[string]any{
			"p": map[string]any{"type": "inline", "payload": []any{xdriveProxy("inline", "google-drive", map[string]any{"secrets": []any{"INLINE"}})}},
		},
	}
	before, _ := yaml.Marshal(m)

	red, changed := redactXDriveCredentials(m)
	if !changed {
		t.Fatal("expected a change")
	}
	after, _ := yaml.Marshal(m)
	if string(before) != string(after) {
		t.Fatal("redaction modified its input")
	}
	text, _ := yaml.Marshal(red)
	for _, secret := range []string{"CLIENT_ID", "CLIENT_SECRET", "REFRESH", "T0KEN", "SECRET1", "SECRET2", "SECRET3", "SECRET4", "SECRET5", "INLINE"} {
		if strings.Contains(string(text), secret) {
			t.Errorf("%s still visible:\n%s", secret, text)
		}
	}
	for _, kept := range []string{"keep-me", "static", "application/octet-stream", "name={name}", "plain {data}", "https://store.example/{name}"} {
		if !strings.Contains(string(text), kept) {
			t.Errorf("%q should stay readable:\n%s", kept, text)
		}
	}

	if _, changed := redactXDriveCredentials(map[string]any{"proxies": []any{map[string]any{"name": "a", "type": "ss"}}}); changed {
		t.Error("a config without XDrive must come back unchanged")
	}
}

func TestRedactXDriveCredentialsYAMLKeepsUnrelatedTextByteForByte(t *testing.T) {
	text := "proxies:\n  - {name: a, type: ss, password: p}   # odd layout kept\n"
	if got := redactXDriveCredentialsYAML(text); got != text {
		t.Fatalf("text without xdrive changed:\n%s", got)
	}
	withX := "proxies:\n- name: d\n  type: vless\n  network: xdrive\n  xdrive-opts:\n    service: google-drive\n    secrets: [ID, SEC, REF]\n"
	got := redactXDriveCredentialsYAML(withX)
	if strings.Contains(got, "SEC") || !strings.Contains(got, xdriveRedacted) {
		t.Fatalf("secrets not redacted:\n%s", got)
	}
}

func TestRedactCredentialQuery(t *testing.T) {
	cases := map[string]string{
		`[XDRIVE] session discovery: Get "https://s.example/list?prefix=a&access_token=abc123&X-Amz-Signature=deadbeef": EOF`: `[XDRIVE] session discovery: Get "https://s.example/list?prefix=a&access_token=[redacted]&X-Amz-Signature=[redacted]": EOF`,
		"https://h/x?api%5Fkey=zzz&page=2":      "https://h/x?api%5Fkey=[redacted]&page=2",
		"no url here, key=value":                "no url here, key=value",
		"https://h/x?token=&next=1":             "https://h/x?token=&next=1",
		"level=info msg=\"GET /sub?token=abc\"": "level=info msg=\"GET /sub?token=[redacted]\"",
	}
	for in, want := range cases {
		if got := redactCredentialQuery(in); got != want {
			t.Errorf("redactCredentialQuery(%q)\n got %q\nwant %q", in, got, want)
		}
	}
}

// TestXDriveLocalStorageRefusedByShippedCore checks core patch 0016 on the
// built core: a local-storage XDrive node is accepted (one bad node must not
// fail a subscription) and the core does not create its folder. A core with
// the gate open creates the folder while loading the config (measured with a
// gate-open build, 2026-10-23), so this catches a regression of the patch.
// The config goes straight to the core, past the app's own hardening, the way
// a proxy-provider fetched by the core would.
func TestXDriveLocalStorageRefusedByShippedCore(t *testing.T) {
	bin := testCoreBinaryForPassthrough()
	if bin == "" {
		t.Skip("no core binary (set SLOTH_MIHOMO_BIN or run prebuild)")
	}
	root := t.TempDir()

	// A core without the XDrive patch accepts an xdrive node without options
	// (it does not know the transport); the patched core refuses it.
	probe := filepath.Join(root, "probe")
	writeTestConfig(t, probe, "proxies:\n- {name: p, type: vless, server: 127.0.0.1, port: 443, uuid: 27848739-7e62-4138-9fd3-098a63964b6b, network: xdrive, encryption: "+testXDriveEncryption+"}\nrules:\n- MATCH,p\n")
	if err := runConfigPreflight(bin, probe); err == nil {
		t.Skip("core has no XDrive support (stock build?)")
	} else if !strings.Contains(err.Error(), "xdrive") {
		t.Fatalf("probe config failed for another reason: %v", err)
	}

	folder := filepath.Join(root, "storage")
	dir := filepath.Join(root, "cfg")
	writeTestConfig(t, dir, "proxies:\n- name: local-node\n  type: vless\n  server: 127.0.0.1\n  port: 443\n  uuid: 27848739-7e62-4138-9fd3-098a63964b6b\n  network: xdrive\n  encryption: "+testXDriveEncryption+"\n  xdrive-opts:\n    service: local\n    remote-folder: "+filepath.ToSlash(folder)+"\nrules:\n- MATCH,local-node\n")
	if err := runConfigPreflight(bin, dir); err != nil {
		t.Fatalf("core rejected a config with a local XDrive node: %v", err)
	}
	if _, err := os.Stat(folder); !os.IsNotExist(err) {
		t.Fatalf("core created the local XDrive folder %s (stat err %v): patch 0016 is not in effect", folder, err)
	}
}

func writeTestConfig(t *testing.T, dir, body string) {
	t.Helper()
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "config.yaml"), []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
}
