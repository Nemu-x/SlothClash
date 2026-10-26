package runtimecfg

import (
	"errors"
	"path/filepath"
	"strings"
	"time"

	"SlothClashDesktop/internal/fsutil"
)

// errDesyncOnlyInput rejects a Desync-only generation handed a subscription,
// a template or a script: the profile kind has none (main refuses to store
// them), so one reaching here is a bug that must not silently produce
// something other than "Desync + direct".
var errDesyncOnlyInput = errors.New("desync-only config: subscription, templates and script must be empty")

// desyncOnlyBase is the config of a profile without a subscription: no
// proxies, no groups, every connection DIRECT. The Desync overlay then puts
// its generated routes in front of MATCH,DIRECT, so a route's PASS member
// (nothing works) ends in the plain direct path. Mode is Rule: the routes are
// rules. Everything else (ports, controller, LAN, TUN, DNS defaults, geodata)
// is the shared runtime overlay, exactly as for a subscription that ships
// none of it.
func desyncOnlyBase() map[string]any {
	return map[string]any{
		"mode":      "rule",
		"log-level": "info",
		"rules":     []any{"MATCH,DIRECT"},
	}
}

// writeDesyncOnly is Write for a Desync-only profile: no fetch, no body
// cache, no templates.
func writeDesyncOnly(in Input) error {
	if strings.TrimSpace(in.SubURL) != "" || strings.TrimSpace(in.AgeKey) != "" ||
		strings.TrimSpace(in.ExtendTemplate) != "" || strings.TrimSpace(in.ProxyTemplate) != "" ||
		strings.TrimSpace(in.RulesTemplate) != "" || strings.TrimSpace(in.Script) != "" {
		return errDesyncOnlyInput
	}
	start := time.Now()
	m := desyncOnlyBase()
	if err := finalizeRuntimeConfigPipelineWithScript(
		m,
		in.DataDir,
		in.MixedPort,
		in.CtrlPort,
		in.Secret,
		in.Traffic,
		in.WithExternalController,
		in.EnableTun,
		"",
		nil,
		in.Env,
	); err != nil {
		traceEvent("pipeline.desync_only.finalize", "fail", time.Since(start), map[string]any{"dataDir": in.DataDir, "error": err.Error()})
		return err
	}
	out, err := MarshalYAML(m)
	if err != nil {
		return err
	}
	if err := fsutil.AtomicWriteFile(filepath.Join(in.DataDir, "config.yaml"), out, 0o644); err != nil {
		traceEvent("pipeline.desync_only.write", "fail", time.Since(start), map[string]any{"dataDir": in.DataDir, "error": err.Error()})
		return err
	}
	traceEvent("pipeline.desync_only.write", "ok", time.Since(start), map[string]any{
		"dataDir":   in.DataDir,
		"enableTun": in.EnableTun,
		"desync":    in.Env.Prefs.Desync.Enabled,
		"yaml_size": len(out),
	})
	return nil
}
