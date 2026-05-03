//go:build linux

package main

import (
	"bufio"
	"os"
	"strings"
)

func rawStableMachineIDPlatform() string {
	for _, p := range []string{"/etc/machine-id", "/var/lib/dbus/machine-id"} {
		b, err := os.ReadFile(p)
		if err != nil {
			continue
		}
		id := strings.TrimSpace(string(b))
		if id != "" {
			return "machine-id:" + id
		}
	}
	return ""
}

func hostOSVersionLabelPlatform() string {
	f, err := os.Open("/etc/os-release")
	if err != nil {
		return "linux"
	}
	defer f.Close()
	var name, ver, pretty string
	sc := bufio.NewScanner(f)
	for sc.Scan() {
		line := strings.TrimSpace(sc.Text())
		switch {
		case strings.HasPrefix(line, "PRETTY_NAME="):
			pretty = parseOSReleaseQuoted(strings.TrimPrefix(line, "PRETTY_NAME="))
		case strings.HasPrefix(line, "NAME=") && !strings.HasPrefix(line, "PRETTY_NAME="):
			name = parseOSReleaseQuoted(strings.TrimPrefix(line, "NAME="))
		case strings.HasPrefix(line, "VERSION="):
			ver = parseOSReleaseQuoted(strings.TrimPrefix(line, "VERSION="))
		}
	}
	if pretty != "" {
		return pretty
	}
	if name != "" && ver != "" {
		return name + " " + ver
	}
	if name != "" {
		return name
	}
	return "linux"
}

func parseOSReleaseQuoted(s string) string {
	s = strings.TrimSpace(s)
	if len(s) >= 2 && s[0] == '"' && s[len(s)-1] == '"' {
		s = s[1 : len(s)-1]
	}
	s = strings.ReplaceAll(s, `\"`, `"`)
	return strings.TrimSpace(s)
}

func hostDeviceModelLabelPlatform() string {
	for _, p := range []string{
		"/sys/devices/virtual/dmi/id/product_name",
		"/sys/firmware/devicetree/base/model",
	} {
		b, err := os.ReadFile(p)
		if err != nil {
			continue
		}
		s := strings.TrimSpace(string(b))
		s = strings.TrimRight(s, "\x00")
		if s != "" {
			return s
		}
	}
	return "PC"
}
