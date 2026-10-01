package main

import (
	"reflect"
	"strings"
	"testing"
)

func TestParseSystemctlShowState(t *testing.T) {
	cases := []struct {
		out            string
		loaded, active bool
	}{
		{"LoadState=loaded\nActiveState=active\n", true, true},
		{"LoadState=loaded\nActiveState=inactive\n", true, false},
		{"LoadState=loaded\nActiveState=failed\n", true, false},
		{"LoadState=loaded\nActiveState=activating\n", true, true},
		{"LoadState=not-found\nActiveState=inactive\n", false, false},
		{"", false, false},
		{"garbage", false, false},
		// CRLF / spacing tolerance.
		{"LoadState = loaded\r\nActiveState = ACTIVE\r\n", true, true},
	}
	for _, c := range cases {
		l, a := parseSystemctlShowState(c.out)
		if l != c.loaded || a != c.active {
			t.Fatalf("parseSystemctlShowState(%q) = (%v,%v), want (%v,%v)", c.out, l, a, c.loaded, c.active)
		}
	}
}

func TestLinuxElevatedInstallArgv(t *testing.T) {
	got := linuxElevatedInstallArgv("/usr/bin/pkexec", "/home/u/.config/SlothClash/service-install/sloth-clash-service-install", "aa,bb")
	want := []string{"/usr/bin/pkexec", "/home/u/.config/SlothClash/service-install/sloth-clash-service-install", "--core-sha256", "aa,bb"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("argv = %v, want %v", got, want)
	}
	got = linuxElevatedInstallArgv("/usr/bin/pkexec", "/x/install", "  ")
	if len(got) != 2 {
		t.Fatalf("no pins → no --core-sha256, got %v", got)
	}
}

func TestLinuxManualInstallCommand(t *testing.T) {
	cmd := linuxManualInstallCommand("/home/u/.config/SlothClash/service-install/sloth-clash-service-install", "aa,bb")
	if cmd != "sudo /home/u/.config/SlothClash/service-install/sloth-clash-service-install --core-sha256 aa,bb" {
		t.Fatalf("unexpected manual command: %q", cmd)
	}
	// A path with a space (e.g. a localized home) must be quoted.
	cmd = linuxManualInstallCommand("/home/my user/x/install", "")
	if !strings.Contains(cmd, "'/home/my user/x/install'") {
		t.Fatalf("path with space must be single-quoted: %q", cmd)
	}
	if strings.Contains(cmd, "--core-sha256") {
		t.Fatalf("no pins → no flag: %q", cmd)
	}
}

func TestExplainPkexecExit(t *testing.T) {
	if explainPkexecExit(126) == "" || explainPkexecExit(127) == "" {
		t.Fatal("polkit exit codes 126/127 must be explained")
	}
	if explainPkexecExit(1) != "" {
		t.Fatal("installer's own exit codes are not polkit's")
	}
}

func TestShellQuote(t *testing.T) {
	if shellQuote("/usr/bin/x-1_2.3") != "/usr/bin/x-1_2.3" {
		t.Fatal("safe path must stay unquoted")
	}
	if shellQuote("it's") != `'it'\''s'` {
		t.Fatalf("quote escaping wrong: %q", shellQuote("it's"))
	}
	if shellQuote("") != "''" {
		t.Fatal("empty must quote to ''")
	}
}
