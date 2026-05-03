//go:build darwin

package main

import (
	"bytes"
	"context"
	"os/exec"
	"strings"
	"time"
)

func rawStableMachineIDPlatform() string {
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, "/usr/sbin/ioreg", "-rd1", "-c", "IOPlatformExpertDevice")
	out, err := cmd.Output()
	if err != nil || len(out) == 0 {
		return ""
	}
	for _, line := range bytes.Split(out, []byte{'\n'}) {
		if !bytes.Contains(line, []byte("IOPlatformUUID")) {
			continue
		}
		eq := bytes.Index(line, []byte("="))
		if eq < 0 {
			continue
		}
		val := bytes.TrimSpace(line[eq+1:])
		val = bytes.Trim(val, `"'`)
		u := strings.TrimSpace(string(val))
		if u != "" {
			return "iouuid:" + strings.ToLower(u)
		}
	}
	return ""
}

func hostOSVersionLabelPlatform() string {
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	out, err := exec.CommandContext(ctx, "/usr/bin/sw_vers", "-productVersion").Output()
	if err != nil {
		return "darwin"
	}
	v := strings.TrimSpace(string(out))
	if v == "" {
		return "darwin"
	}
	return "macOS " + v
}

func hostDeviceModelLabelPlatform() string {
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	out, err := exec.CommandContext(ctx, "/usr/sbin/sysctl", "-n", "hw.model").Output()
	if err != nil {
		return "Mac"
	}
	m := strings.TrimSpace(string(out))
	if m == "" {
		return "Mac"
	}
	return m
}
