package runtimecfg

import "testing"

func TestParseRealityClientVersion(t *testing.T) {
	for in, want := range map[string]string{
		"": "", "  ": "", "26.9.9": "26.9.9", "v1.9.0": "1.9.0", "025.01.1": "25.1.1", "255.255.255": "255.255.255",
	} {
		got, err := ParseRealityClientVersion(in)
		if err != nil || got != want {
			t.Errorf("parseRealityClientVersion(%q) = %q, %v; want %q", in, got, err, want)
		}
	}
	for _, in := range []string{"1.9", "1.9.0.0", "256.0.0", "-1.0.0", "1.x.0", "1.9.0-rc1", "1..0"} {
		if _, err := ParseRealityClientVersion(in); err == nil {
			t.Errorf("parseRealityClientVersion(%q) accepted an invalid version", in)
		}
	}
}

func TestRealityMLKEMMapping(t *testing.T) {
	for in, want := range map[string]string{
		"": "auto", "auto": "auto", "Always": "on", "on": "on", "never": "off", "OFF": "off", "bogus": "auto",
	} {
		if got := coreRealityMLKEM(in); got != want {
			t.Errorf("coreRealityMLKEM(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestApplyRealityOverlay(t *testing.T) {
	cases := []struct {
		name    string
		sub     map[string]any
		prefs   RealitySettings
		mlkem   string
		version string // "" = key absent
	}{
		{"defaults", map[string]any{}, RealitySettings{}, "auto", ""},
		{"user wins", map[string]any{"reality-mlkem": "off", "reality-client-version": "1.8.2"},
			RealitySettings{MLKEM: "always", ClientVersion: "26.9.9"}, "on", "26.9.9"},
		{"subscription version inherited", map[string]any{"reality-client-version": "1.9.0"}, RealitySettings{}, "auto", "1.9.0"},
		{"bad subscription values dropped", map[string]any{"reality-mlkem": "always", "reality-client-version": "1.300.0"},
			RealitySettings{}, "auto", ""},
	}
	for _, c := range cases {
		applyRealityOverlay(c.sub, c.prefs)
		if got := c.sub["reality-mlkem"]; got != c.mlkem {
			t.Errorf("%s: reality-mlkem = %v, want %s", c.name, got, c.mlkem)
		}
		got, has := c.sub["reality-client-version"]
		if c.version == "" && has {
			t.Errorf("%s: reality-client-version = %v, want absent", c.name, got)
		}
		if c.version != "" && got != c.version {
			t.Errorf("%s: reality-client-version = %v, want %s", c.name, got, c.version)
		}
	}
}
