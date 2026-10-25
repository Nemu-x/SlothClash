package runtimecfg

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"SlothClashDesktop/internal/procattr"
)

// Preflight validates dataDir/config.yaml against the core at binPath before it
// is launched: the last-mile DNS repair (repairConfigDNS with p), then the
// core's own `-t` test mode, falling back to a short startup probe on cores
// without it. No binPath, or no config.yaml, is a no-op.
func Preflight(binPath, dataDir string, p Prefs) error {
	binPath = strings.TrimSpace(binPath)
	if binPath == "" {
		return nil
	}
	cfgPath := filepath.Join(dataDir, "config.yaml")
	if st, err := os.Stat(cfgPath); err != nil || st.Size() == 0 {
		return nil
	}
	// Last-mile hardening: normalize DNS invariants in the written file before preflight.
	// This guards against edge cases from user-edited YAML / merge templates.
	if err := repairConfigDNS(cfgPath, p); err != nil {
		return fmt.Errorf("configuration preflight failed: cannot normalize dns invariants: %w", err)
	}

	// Clash/Mihomo supports `-t` test mode; use it before actual launch so users see
	// parse errors immediately instead of waiting for a failed start.
	argVariants := [][]string{
		{"-d", dataDir, "-t"},
		{"-t", "-d", dataDir},
	}
	sawUnsupportedFlag := false
	for _, args := range argVariants {
		ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
		cmd := exec.CommandContext(ctx, binPath, args...)
		cmd.Dir = dataDir
		if attr := procattr.HideWindow(); attr != nil {
			cmd.SysProcAttr = attr
		}
		out, err := cmd.CombinedOutput()
		cancel()
		text := strings.TrimSpace(string(out))
		if err == nil {
			return nil
		}
		lower := strings.ToLower(text + "\n" + err.Error())
		if strings.Contains(lower, "unknown flag") ||
			strings.Contains(lower, "flag provided but not defined") ||
			strings.Contains(lower, "unknown shorthand flag") {
			sawUnsupportedFlag = true
			continue
		}
		if idx := strings.Index(strings.ToLower(text), "parse config error:"); idx >= 0 {
			msg := strings.TrimSpace(text[idx:])
			return fmt.Errorf("configuration preflight failed: %s", msg)
		}
		if text == "" {
			text = err.Error()
		}
		return fmt.Errorf("configuration preflight failed: %s", strings.TrimSpace(text))
	}
	if sawUnsupportedFlag {
		// Older binaries may not support test mode; run a short startup probe to still
		// catch fatal parse errors before regular launch.
		return runConfigStartupProbe(binPath, dataDir)
	}
	return nil
}

func runConfigStartupProbe(binPath, dataDir string) error {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, binPath, "-d", dataDir)
	cmd.Dir = dataDir
	if attr := procattr.HideWindow(); attr != nil {
		cmd.SysProcAttr = attr
	}
	out, err := cmd.CombinedOutput()
	text := strings.TrimSpace(string(out))
	lower := strings.ToLower(text)
	if idx := strings.Index(lower, "parse config error:"); idx >= 0 {
		return fmt.Errorf("configuration preflight failed: %s", strings.TrimSpace(text[idx:]))
	}
	if err != nil {
		// deadline exceeded without parse error usually means the core started; accept.
		if errors.Is(ctx.Err(), context.DeadlineExceeded) {
			return nil
		}
		if text == "" {
			text = err.Error()
		}
		if strings.Contains(strings.ToLower(text), "parse config error") {
			return fmt.Errorf("configuration preflight failed: %s", text)
		}
	}
	return nil
}
