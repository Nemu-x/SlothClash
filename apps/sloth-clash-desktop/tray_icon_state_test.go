package main

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func TestTrayIconKindFor(t *testing.T) {
	cases := []struct {
		status, traffic string
		want            trayIconKind
	}{
		{"connected", "tun", trayIconTun},
		{"connected", "proxy", trayIconProxy},
		{"connected", "", trayIconProxy},
		{"Connected", "TUN", trayIconTun},
		// Anything short of an established connection stays idle — a stuck
		// "connecting" must not advertise a tunnel.
		{"connecting", "tun", trayIconIdle},
		{"error", "tun", trayIconIdle},
		{"disconnected", "proxy", trayIconIdle},
		{"", "", trayIconIdle},
	}
	for _, c := range cases {
		if got := trayIconKindFor(c.status, c.traffic); got != c.want {
			t.Fatalf("trayIconKindFor(%q,%q) = %v, want %v", c.status, c.traffic, got, c.want)
		}
	}
}

func TestResolveTrayIconStyle(t *testing.T) {
	if got := resolveTrayIconStyle("", "darwin"); got != trayIconStyleMono {
		t.Fatalf("darwin default = %q, want mono", got)
	}
	if got := resolveTrayIconStyle("", "windows"); got != trayIconStyleColorful {
		t.Fatalf("windows default = %q, want colorful", got)
	}
	if got := resolveTrayIconStyle(" Mono ", "windows"); got != trayIconStyleMono {
		t.Fatalf("explicit mono on windows = %q", got)
	}
	if got := resolveTrayIconStyle("colorful", "darwin"); got != trayIconStyleColorful {
		t.Fatalf("explicit colorful on darwin = %q", got)
	}
	if got := resolveTrayIconStyle("neon", "windows"); got != trayIconStyleColorful {
		t.Fatalf("unknown style must fall back to the platform default, got %q", got)
	}
	if got := normalizeTrayIconStyle("neon"); got != "" {
		t.Fatalf("normalizeTrayIconStyle(neon) = %q, want empty", got)
	}
}

func TestTrayTooltipFor(t *testing.T) {
	s := trayStringsEN
	if got := trayTooltipFor(trayIconIdle, s); got != "Sloth Clash · Disconnected" {
		t.Fatalf("idle tooltip = %q", got)
	}
	if got := trayTooltipFor(trayIconProxy, s); !strings.Contains(got, "Proxy") {
		t.Fatalf("proxy tooltip = %q", got)
	}
	if got := trayTooltipFor(trayIconTun, s); !strings.Contains(got, "TUN") {
		t.Fatalf("tun tooltip = %q", got)
	}
	// Every locale must carry the three mode strings — the tray cannot fall
	// back to react-i18next.
	for name, tbl := range map[string]trayStrings{"ru": trayStringsRU, "zh": trayStringsZH} {
		if tbl.ModeIdle == "" || tbl.ModeProxy == "" || tbl.ModeTun == "" {
			t.Fatalf("%s tray strings missing mode labels", name)
		}
	}
}

// Every style×state combination the tray can ask for must exist as a generated
// asset for the platform this test runs on; otherwise the embed pattern would
// silently ship a build whose tray never changes.
func TestTrayIconAssetsGeneratedForThisPlatform(t *testing.T) {
	var dir, ext string
	switch runtime.GOOS {
	case "windows":
		dir, ext = filepath.Join("build", "windows", "tray"), ".ico"
	case "darwin":
		dir, ext = filepath.Join("trayicons", "state"), ".png"
	default:
		t.Skip("no native tray on this platform")
	}
	for _, style := range []string{trayIconStyleColorful, trayIconStyleMono} {
		for _, kind := range []trayIconKind{trayIconIdle, trayIconProxy, trayIconTun} {
			p := filepath.Join(dir, trayIconAssetBase(style, kind)+ext)
			st, err := os.Stat(p)
			if err != nil || st.Size() == 0 {
				t.Fatalf("missing tray asset %s (run: pnpm run icons:windows): %v", p, err)
			}
		}
	}
}
