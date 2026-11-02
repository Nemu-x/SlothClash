package runtimecfg

import "testing"

func xdriveServiceProfile() map[string]any {
	return map[string]any{
		"proxies": []any{
			xdriveProxy("s3-node", "s3", nil),
			xdriveProxy("template-node", "template", map[string]any{"template": map[string]any{"put": map[string]any{"url": "https://storage.invalid/{name}"}}}),
			xdriveProxy("local-node", "local", nil),
			xdriveProxy("padded-node", " google-drive ", nil),
			xdriveProxy("case-node", "Google-Drive", nil),
			xdriveProxy("drive-node", "google-drive", map[string]any{"secrets": []any{"id", "secret", "refresh"}}),
			xdriveProxy("alias-node", "Google Drive", map[string]any{"secrets": []any{"id", "secret", "refresh"}}),
			map[string]any{
				"name": "no-service", "type": "vless", "server": "127.0.0.1", "port": 443, "network": "xdrive",
				"xdrive-opts": map[string]any{"remote-folder": "f"},
			},
			map[string]any{"name": "no-opts", "type": "vless", "server": "127.0.0.1", "port": 443, "network": "xdrive"},
			map[string]any{
				"name": "bad-opts", "type": "vless", "server": "127.0.0.1", "port": 443, "network": "xdrive",
				"xdrive-opts": "s3",
			},
			map[string]any{
				"name": "numeric-service", "type": "vless", "server": "127.0.0.1", "port": 443, "network": "xdrive",
				"xdrive-opts": map[string]any{"service": 3, "remote-folder": "f"},
			},
			// A second spelling the decoder could pick must not hide an
			// unsupported service behind an allowed one.
			map[string]any{
				"name": "two-spellings", "type": "vless", "server": "127.0.0.1", "port": 443, "Network": "XDrive",
				"xdrive-opts": map[string]any{"service": "google-drive", "Service": "s3", "remote-folder": "f"},
			},
			map[string]any{
				"name": "sneaky", "type": "vless", "server": "127.0.0.1", "port": 443, "network": "xdrive",
				"Xdrive_Opts": map[string]any{"service": "s3", "remote-folder": "f"},
			},
			map[string]any{"name": "plain-vless", "type": "vless", "server": "127.0.0.1", "port": 443, "network": "tcp"},
			map[string]any{"name": "plain-ss", "type": "ss", "server": "127.0.0.1", "port": 8388, "service": "s3"},
		},
		"proxy-providers": map[string]any{
			"inline": map[string]any{"type": "inline", "Payload": []any{
				xdriveProxy("inline-s3", "s3", nil),
				xdriveProxy("inline-drive", "google-drive", nil),
			}},
		},
		"proxy-groups": []any{
			map[string]any{"name": "PROXY", "type": "select", "proxies": []any{"s3-node", "drive-node"}, "use": []any{"inline"}},
		},
		"rules": []any{"MATCH,PROXY"},
	}
}

var (
	xdriveRejectedNodes = []string{
		"s3-node", "template-node", "local-node", "padded-node", "case-node",
		"no-service", "no-opts", "bad-opts", "numeric-service", "two-spellings", "sneaky",
	}
	xdriveKeptNodes = []string{"drive-node", "alias-node", "plain-vless", "plain-ss"}
)

func assertXDriveServiceAllowList(t *testing.T, m map[string]any) {
	t.Helper()
	byName := map[string]map[string]any{}
	for _, it := range m["proxies"].([]any) {
		pm := it.(map[string]any)
		byName[pm["name"].(string)] = pm
	}
	for _, name := range xdriveRejectedNodes {
		if pm := byName[name]; pm == nil || pm["type"] != "reject" || len(pm) != 2 {
			t.Errorf("%s: want a bare REJECT proxy of the same name, got %#v", name, pm)
		}
	}
	for _, name := range xdriveKeptNodes {
		if pm := byName[name]; pm == nil || pm["type"] == "reject" {
			t.Errorf("%s must pass untouched, got %#v", name, pm)
		}
	}
	inline := m["proxy-providers"].(map[string]any)["inline"].(map[string]any)["Payload"].([]any)
	if p := inline[0].(map[string]any); p["type"] != "reject" || p["name"] != "inline-s3" {
		t.Errorf("inline s3 node not rejected: %#v", p)
	}
	if p := inline[1].(map[string]any); p["type"] != "vless" || p["name"] != "inline-drive" {
		t.Errorf("inline google-drive node changed: %#v", p)
	}
}

func TestXDriveServiceAllowList(t *testing.T) {
	m := xdriveServiceProfile()
	applyXDriveServiceAllowList(m)
	assertXDriveServiceAllowList(t, m)
}

func TestXDriveServiceAllowListLeavesConfigsWithoutXDrive(t *testing.T) {
	m := map[string]any{"proxies": []any{map[string]any{"name": "a", "type": "ss"}}, "proxy-providers": "not-a-map"}
	applyXDriveServiceAllowList(m)
	if pm := m["proxies"].([]any)[0].(map[string]any); len(pm) != 2 || pm["type"] != "ss" {
		t.Fatalf("non-XDrive proxy changed: %#v", pm)
	}
	applyXDriveServiceAllowList(map[string]any{})
}

func TestXDriveServiceAllowListInPipeline(t *testing.T) {
	m := xdriveServiceProfile()
	if err := finalizeRuntimeConfigPipeline(m, t.TempDir(), 54333, 9097, "secret", "tun", true, true); err != nil {
		t.Fatalf("pipeline: %v", err)
	}
	assertXDriveServiceAllowList(t, m)
}

// A profile script runs after the overlay; it must not be able to bring an
// unsupported XDrive node back.
func TestXDriveServiceAllowListSurvivesScript(t *testing.T) {
	m := xdriveServiceProfile()
	var res ScriptResult
	script := `
function main(config) {
  config.proxies.push({ name: 'script-s3', type: 'vless', server: '127.0.0.1', port: 443,
    uuid: '27848739-7e62-4138-9fd3-098a63964b6b', network: 'xdrive', encryption: '` + testXDriveEncryption + `',
    'xdrive-opts': { service: 's3', 'remote-folder': 'f' } })
  return config
}`
	if err := finalizeRuntimeConfigPipelineWithScript(m, t.TempDir(), 54333, 9097, "secret", "tun", true, true, script, &res, testEnv()); err != nil {
		t.Fatalf("pipeline: %v", err)
	}
	if !res.Applied {
		t.Fatalf("script did not apply: %s", res.Err)
	}
	found := false
	for _, it := range m["proxies"].([]any) {
		pm := it.(map[string]any)
		if pm["name"] == "script-s3" {
			found = true
			if pm["type"] != "reject" {
				t.Errorf("script-s3 survived as %#v", pm)
			}
		}
	}
	if !found {
		t.Fatal("script-s3 missing from the result")
	}
}
