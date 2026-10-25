package main

import (
	"testing"

	"SlothClashDesktop/internal/desync"
	"SlothClashDesktop/internal/runtimecfg"
	"SlothClashDesktop/internal/runtimecfg/runtimecfgtest"
)

// finalizeRuntimeConfigPipeline runs runtimecfg.Finalize with no script and
// the env captured from the live (test-controlled) desktop prefs, so tests here
// assert the prefs -> generated config chain end to end.
func finalizeRuntimeConfigPipeline(
	m map[string]any,
	dataDir string,
	mixedPort, ctrlPort int,
	secret, traffic string,
	withExternalController bool,
	enableTun bool,
) error {
	return runtimecfg.Finalize(m, runtimecfg.Input{
		DataDir:                dataDir,
		CtrlPort:               ctrlPort,
		MixedPort:              mixedPort,
		Secret:                 secret,
		WithExternalController: withExternalController,
		Traffic:                traffic,
		EnableTun:              enableTun,
		Env:                    currentRuntimeConfigEnv(),
	}, nil)
}

// withRuntimeConfigPrefs swaps the live desktop prefs for the duration of a
// test and restores them, and the live corp split, afterwards, so a generation
// through currentRuntimeConfigEnv sees exactly what the test set.
func withRuntimeConfigPrefs(t *testing.T, p DesktopPrefs) {
	t.Helper()
	prefsMu.Lock()
	prev := prefsCurrent
	prefsCurrent = p
	prefsMu.Unlock()
	prevSplit := currentCorpVpnSplit()
	t.Cleanup(func() {
		setCorpVpnSplit(prevSplit)
		prefsMu.Lock()
		prefsCurrent = prev
		prefsMu.Unlock()
	})
}

func configStringList(t *testing.T, v any, what string) []string {
	t.Helper()
	list, ok := v.([]any)
	if !ok {
		t.Fatalf("%s is %T, want a list", what, v)
	}
	out := make([]string, 0, len(list))
	for _, e := range list {
		s, _ := e.(string)
		out = append(out, s)
	}
	return out
}

func containsString(list []string, want string) bool {
	for _, s := range list {
		if s == want {
			return true
		}
	}
	return false
}

// The corp split the sidecar reported must reach the generation through the
// env capture: under TUN its subnets leave the tunnel and its domains stay out
// of the fake-ip pool.
func TestCorpVpnSplitReachesGeneration(t *testing.T) {
	withRuntimeConfigPrefs(t, DesktopPrefs{})
	setCorpVpnSplit(runtimecfg.CorpVpnSplit{
		Routes:     []string{"10.42.0.0/16"},
		DNSServers: []string{"10.42.0.53"},
		DNSDomains: []string{"corp.example"},
	})
	m := runtimecfgtest.RepresentativeFullProfile()
	if err := finalizeRuntimeConfigPipeline(m, t.TempDir(), 7890, 9090, "secret", "tun", true, true); err != nil {
		t.Fatalf("pipeline error: %v", err)
	}
	tun, _ := m["tun"].(map[string]any)
	if excl := configStringList(t, tun["route-exclude-address"], "tun.route-exclude-address"); !containsString(excl, "10.42.0.0/16") {
		t.Errorf("tun.route-exclude-address = %v, want the corp subnet", excl)
	}
	d := runtimecfgtest.DNSMap(t, m)
	if filter := configStringList(t, d["fake-ip-filter"], "dns.fake-ip-filter"); !containsString(filter, "+.corp.example") {
		t.Errorf("dns.fake-ip-filter = %v, want the corp domain", filter)
	}
}

// desyncYouTubeDefault generates with the live prefs and returns the
// default-selected candidate of the managed YouTube group.
func desyncYouTubeDefault(t *testing.T) string {
	t.Helper()
	m := runtimecfgtest.RepresentativeFullProfile()
	if err := finalizeRuntimeConfigPipeline(m, t.TempDir(), 7890, 9090, "secret", "proxy", true, false); err != nil {
		t.Fatalf("pipeline error: %v", err)
	}
	groups, _ := m["proxy-groups"].([]any)
	for _, raw := range groups {
		if g, ok := raw.(map[string]any); ok && g["name"] == desync.YouTubeRoute {
			s, _ := g["default-selected"].(string)
			return s
		}
	}
	t.Fatalf("no %s group generated", desync.YouTubeRoute)
	return ""
}

// The desync winner cached for the current network must be what the generated
// group selects by default; a winner cached for another network must not be.
func TestDesyncCachedWinnerReachesGeneration(t *testing.T) {
	const winner = "r8"
	defaultFirst := desync.Candidates("youtube")[0].ID
	if defaultFirst == winner {
		t.Fatalf("the winner must not already be the first candidate (%s)", defaultFirst)
	}
	enabled := DesyncSettings{Enabled: true, YouTube: true, DiscordTTL: 4}

	t.Run("current network", func(t *testing.T) {
		withRuntimeConfigPrefs(t, DesktopPrefs{Desync: enabled})
		saveDesyncSelection(desyncNetworkKey(), "youtube", winner)
		if got, want := desyncYouTubeDefault(t), (desync.Candidate{ID: winner}).Name(desync.YouTubeRoute); got != want {
			t.Fatalf("default-selected = %q, want the cached winner %q", got, want)
		}
	})
	t.Run("other network", func(t *testing.T) {
		withRuntimeConfigPrefs(t, DesktopPrefs{Desync: enabled})
		saveDesyncSelection("not-"+desyncNetworkKey(), "youtube", winner)
		if got, want := desyncYouTubeDefault(t), (desync.Candidate{ID: defaultFirst}).Name(desync.YouTubeRoute); got != want {
			t.Fatalf("default-selected = %q, want the default first candidate %q", got, want)
		}
	})
}

// The app version a profile script sees comes from the env capture.
func TestAppVersionReachesProfileScript(t *testing.T) {
	withRuntimeConfigPrefs(t, DesktopPrefs{})
	m := runtimecfgtest.RepresentativeFullProfile()
	var result runtimecfg.ScriptResult
	script := `function main(config, ctx) {
        config["x-test-app-version"] = ctx.appVersion
        return config
    }`
	if err := runtimecfg.Finalize(m, runtimecfg.Input{
		DataDir:                t.TempDir(),
		Script:                 script,
		CtrlPort:               9090,
		MixedPort:              7890,
		Secret:                 "secret",
		WithExternalController: true,
		Traffic:                "proxy",
		Env:                    currentRuntimeConfigEnv(),
	}, &result); err != nil {
		t.Fatal(err)
	}
	if !result.Applied {
		t.Fatalf("script did not run: %s", result.Err)
	}
	if got := m["x-test-app-version"]; got != AppVersion {
		t.Fatalf("script saw appVersion %v, want %q", got, AppVersion)
	}
}
