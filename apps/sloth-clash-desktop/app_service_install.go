package main

import (
	"embed"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"runtime"
	"strconv"
	"strings"
	"time"

	"SlothClashDesktop/internal/runtimecfg"

	wailsrt "github.com/wailsapp/wails/v2/pkg/runtime"
)

func queryWindowsServiceStatus(name string) (installed bool, running bool, lastErr string) {
	if runtime.GOOS != "windows" {
		return false, false, ""
	}
	cmd := exec.Command(system32Exe("sc.exe"), "query", name)
	if attr := hideWindowSysProcAttr(); attr != nil {
		cmd.SysProcAttr = attr
	}
	out, err := cmd.CombinedOutput()
	text := strings.TrimSpace(string(out))
	if err != nil {
		lt := strings.ToLower(text)
		if strings.Contains(lt, "does not exist") || strings.Contains(lt, "1060") {
			return false, false, ""
		}
		return false, false, text
	}
	// Parse numeric STATE code to avoid locale-dependent "RUNNING" text parsing.
	// Example line: "STATE              : 4  RUNNING"
	re := regexp.MustCompile(`(?mi)^\s*STATE\s*:\s*([0-9]+)\b`)
	m := re.FindStringSubmatch(text)
	if len(m) >= 2 {
		n, convErr := strconv.Atoi(strings.TrimSpace(m[1]))
		if convErr == nil {
			return true, n == 4, ""
		}
	}
	// Fallback for unusual outputs.
	upper := strings.ToUpper(text)
	return true, strings.Contains(upper, "RUNNING"), ""
}

func (a *App) refreshServiceStatus() {
	var installed, running bool
	var lastErr string
	switch runtime.GOOS {
	case "windows":
		installed, running, lastErr = queryWindowsServiceStatus("sloth_clash_service")
	case "darwin":
		installed, running, lastErr = queryDarwinServiceStatus()
	case "linux":
		installed, running, lastErr = queryLinuxServiceStatus()
	default:
		return
	}
	a.mu.Lock()
	prevErr := strings.TrimSpace(a.state.Service.LastError)
	a.state.Service.Installed = installed
	a.state.Service.Running = running
	newErr := strings.TrimSpace(lastErr)
	a.state.Service.LastError = newErr
	a.state.UpdatedAt = time.Now().Unix()
	a.mu.Unlock()
	if newErr != "" && newErr != prevErr {
		a.appendRuntimeDiag("ipc.error", "service status: "+newErr)
	}
}

func queryDarwinServiceStatus() (installed bool, running bool, lastErr string) {
	plist := "/Library/LaunchDaemons/dev.slothclash.desktop.ipc.service.plist"
	bundleBin := "/Library/PrivilegedHelperTools/dev.slothclash.desktop.ipc.service.bundle/Contents/MacOS/sloth-clash-service"
	if _, err := os.Stat(plist); err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return false, false, ""
		}
		return false, false, err.Error()
	}
	if _, err := os.Stat(bundleBin); err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return false, false, ""
		}
		return false, false, err.Error()
	}
	installed = true
	cmd := exec.Command("launchctl", "print", "system/dev.slothclash.desktop.ipc.service")
	out, err := cmd.CombinedOutput()
	if err == nil {
		s := strings.ToLower(string(out))
		running = strings.Contains(s, "state = running") || strings.Contains(s, "\"state\" => \"running\"")
		return installed, running, ""
	}
	txt := strings.TrimSpace(string(out))
	lt := strings.ToLower(txt)
	if strings.Contains(lt, "could not find service") || strings.Contains(lt, "unknown service") {
		return installed, false, ""
	}
	return installed, false, txt
}

func (a *App) InstallService() (TunSetupResult, error) {
	// Platforms without an IPC transport (ipc_sloth_stub.go) must not run the
	// installer: it would add a root service nobody uses.
	if !privilegedServiceSupported(runtime.GOOS) {
		return TunSetupResult{
			Success:       false,
			Message:       privilegedServiceUnsupportedMessage,
			InstallAction: false,
		}, nil
	}
	var tmpDir string
	var err error
	if runtime.GOOS == "linux" {
		// Stable, exec-able staging dir (see linuxServiceStagingDir); kept on
		// failure so the manual `sudo ...` fallback still points at real files.
		tmpDir, err = linuxServiceStagingDir()
	} else {
		tmpDir, err = os.MkdirTemp("", "sloth-clash-service-*")
	}
	if err != nil {
		return TunSetupResult{}, err
	}

	extracted := false
	if err := extractEmbeddedDir(a.bundle, "build/resources", tmpDir); err == nil {
		extracted = true
	} else if !errors.Is(err, fs.ErrNotExist) {
		_ = os.RemoveAll(tmpDir)
		return TunSetupResult{}, err
	}
	if err := extractEmbeddedDir(a.bundle, "build/sidecar", tmpDir); err == nil {
		extracted = true
	} else if !errors.Is(err, fs.ErrNotExist) {
		_ = os.RemoveAll(tmpDir)
		return TunSetupResult{}, err
	}
	if !extracted {
		_ = os.RemoveAll(tmpDir)
		return TunSetupResult{
			Success:       false,
			Message:       "Service bundle missing. Run: pnpm run prebuild && pnpm run prepare:wails",
			InstallAction: true,
		}, nil
	}

	installPath, err := findServiceInstaller(tmpDir)
	if err != nil {
		_ = os.RemoveAll(tmpDir)
		return TunSetupResult{
			Success:       false,
			Message:       err.Error(),
			InstallAction: true,
		}, nil
	}

	// Pin the embedded core by content: the installer records these SHA-256(s) in
	// the service environment so it will only spawn a core whose bytes match,
	// even though the core lives in the user-writable _sidecar dir.
	coreHashesArg := strings.Join(a.pinnedCoreHashesFromEmbed(), ",")

	var out []byte
	var runErr error
	if runtime.GOOS == "windows" {
		if a.ctx != nil {
			wailsrt.WindowMinimise(a.ctx)
		}
		out, runErr = installServiceElevatedWindows(installPath, tmpDir, coreHashesArg)
		if a.ctx != nil {
			wailsrt.WindowShow(a.ctx)
			wailsrt.WindowUnminimise(a.ctx)
		}
	} else if runtime.GOOS == "darwin" {
		out, runErr = installServiceElevatedDarwin(installPath, tmpDir, coreHashesArg)
	} else {
		// Linux: pkexec (polkit). The error text already carries the manual
		// sudo fallback when polkit is missing or refused (issue #73).
		out, runErr = installServiceElevatedLinux(installPath, tmpDir, coreHashesArg)
	}
	if runErr != nil {
		if runtime.GOOS != "linux" {
			_ = os.RemoveAll(tmpDir)
		}
		msg := runErr.Error()
		if runtime.GOOS != "linux" {
			if o := strings.TrimSpace(string(out)); o != "" {
				msg = o
			}
		}
		hint := ""
		if runtime.GOOS == "windows" && (strings.Contains(strings.ToLower(msg), "access is denied") ||
			strings.Contains(strings.ToLower(msg), "os error 5")) {
			hint = " On Windows the installer must run elevated: accept the UAC prompt. If you denied it, try again."
		}
		return TunSetupResult{
			Success:       false,
			Message:       "Service install failed: " + msg + hint,
			InstallAction: true,
		}, nil
	}

	// A fresh (re)install records the current embedded core hashes and re-lays
	// the service binary, so any stale-pin or unreachable condition is now
	// resolved regardless of platform.
	a.mu.Lock()
	a.state.Service.CorePinMismatch = false
	a.state.Service.Unreachable = false
	a.mu.Unlock()

	// Every supported platform now has a real status query (sc / launchctl /
	// systemctl), so never assume "installed+running" on a successful exit.
	a.refreshServiceStatus()

	_ = os.RemoveAll(tmpDir)
	msg := "Service installed. If you also use another Clash client, stop its Windows service while using Sloth TUN to avoid conflicts."
	if strings.Contains(strings.ToLower(filepath.Base(installPath)), "sloth-clash-service") {
		msg = "Service installed (Sloth IPC helper). Stop the other Clash service if both are registered and you only want Sloth handling TUN."
	}
	return TunSetupResult{
		Success:       true,
		Message:       msg,
		InstallAction: false,
	}, nil
}

// ReadServiceLatestLog returns a tail of runtime logs for the active profile.
// Windows service mode writes logs/service_latest.log, while direct core mode (macOS/Linux)
// primarily writes core.log.
func (a *App) ReadServiceLatestLog(maxBytes int) ServiceLogPeek {
	if maxBytes <= 0 {
		maxBytes = 120_000
	}
	if maxBytes > 512*1024 {
		maxBytes = 512 * 1024
	}

	a.mu.RLock()
	pid := strings.TrimSpace(a.state.Profile.ActiveProfileID)
	a.mu.RUnlock()
	if pid == "" {
		return ServiceLogPeek{LastError: "no active profile"}
	}
	roots := []string{}
	if root, err := slothDataRoot(); err == nil && strings.TrimSpace(root) != "" {
		roots = append(roots, root)
	}
	// Windows can occasionally run elevated and resolve UserConfigDir under
	// Administrator, while previously generated runtime data still lives under
	// the standard user profile. Probe common Windows app-data roots so the
	// in-app log tail keeps working across privilege flips.
	if runtime.GOOS == "windows" {
		if appData := strings.TrimSpace(os.Getenv("APPDATA")); appData != "" {
			roots = append(roots, filepath.Join(appData, "SlothClash"))
		}
		if localAppData := strings.TrimSpace(os.Getenv("LOCALAPPDATA")); localAppData != "" {
			roots = append(roots, filepath.Join(localAppData, "SlothClash"))
		}
	}
	seenRoots := map[string]bool{}
	dedupRoots := make([]string, 0, len(roots))
	for _, r := range roots {
		if r == "" {
			continue
		}
		key := strings.ToLower(filepath.Clean(r))
		if seenRoots[key] {
			continue
		}
		seenRoots[key] = true
		dedupRoots = append(dedupRoots, filepath.Clean(r))
	}
	candidates := []string{}
	for _, root := range dedupRoots {
		runtimeDir := filepath.Join(root, "runtime", pid)
		candidates = append(candidates,
			filepath.Join(runtimeDir, "logs", "service_latest.log"),
			filepath.Join(runtimeDir, "core.log"),
			filepath.Join(runtimeDir, "logs", "service.log"),
		)
		// flexi_logger rotation sometimes leaves only timestamped files
		// (service_2026-04-24_23-43-01.log) if the process was killed
		// between the rename and the re-open of service_latest.log. Fall
		// back to the newest rotated file so diagnostics bundles still
		// carry core output instead of just "no runtime log file found".
		if rotated, _ := filepath.Glob(filepath.Join(runtimeDir, "logs", "service_*.log")); len(rotated) > 0 {
			newestRotated := ""
			var newestMod time.Time
			for _, r := range rotated {
				if info, err := os.Stat(r); err == nil && !info.IsDir() {
					if info.ModTime().After(newestMod) {
						newestMod = info.ModTime()
						newestRotated = r
					}
				}
			}
			if newestRotated != "" {
				candidates = append(candidates, newestRotated)
			}
		}
	}
	if runtime.GOOS == "windows" {
		sysDrive := strings.TrimSpace(os.Getenv("SystemDrive"))
		if sysDrive == "" {
			sysDrive = "C:"
		}
		// Cross-user fallback: if the app is currently elevated under a
		// different account, the active profile runtime can still exist under
		// the original desktop user tree.
		globs := []string{
			filepath.Join(sysDrive+`\`, "Users", "*", "AppData", "Roaming", "SlothClash", "runtime", pid, "logs", "service_latest.log"),
			filepath.Join(sysDrive+`\`, "Users", "*", "AppData", "Roaming", "SlothClash", "runtime", pid, "core.log"),
			filepath.Join(sysDrive+`\`, "Users", "*", "AppData", "Local", "SlothClash", "runtime", pid, "logs", "service_latest.log"),
			filepath.Join(sysDrive+`\`, "Users", "*", "AppData", "Local", "SlothClash", "runtime", pid, "core.log"),
		}
		for _, g := range globs {
			if matches, err := filepath.Glob(g); err == nil && len(matches) > 0 {
				candidates = append(candidates, matches...)
			}
		}
	}
	seenCand := map[string]bool{}
	dedupCandidates := make([]string, 0, len(candidates))
	for _, c := range candidates {
		if strings.TrimSpace(c) == "" {
			continue
		}
		key := strings.ToLower(filepath.Clean(c))
		if seenCand[key] {
			continue
		}
		seenCand[key] = true
		dedupCandidates = append(dedupCandidates, filepath.Clean(c))
	}
	candidates = dedupCandidates
	var p string
	var st os.FileInfo
	for _, cand := range candidates {
		info, serr := os.Stat(cand)
		if serr != nil || info.IsDir() {
			continue
		}
		p = cand
		st = info
		break
	}
	if p == "" {
		return ServiceLogPeek{
			Path:      candidates[0],
			LastError: "no runtime log file found (tried service_latest.log/core.log/service.log)",
		}
	}
	f, err := os.Open(p)
	if err != nil {
		return ServiceLogPeek{Path: p, LastError: err.Error()}
	}
	defer f.Close()

	size := st.Size()
	if size <= int64(maxBytes) {
		b, rerr := io.ReadAll(io.LimitReader(f, int64(maxBytes)+1))
		if rerr != nil {
			return ServiceLogPeek{Path: p, LastError: rerr.Error()}
		}
		return ServiceLogPeek{Path: p, Text: runtimecfg.RedactCredentialQuery(string(b))}
	}

	if _, err := f.Seek(-int64(maxBytes), io.SeekEnd); err != nil {
		return ServiceLogPeek{Path: p, LastError: err.Error()}
	}
	b, err := io.ReadAll(io.LimitReader(f, int64(maxBytes)+1))
	if err != nil {
		return ServiceLogPeek{Path: p, LastError: err.Error()}
	}
	return ServiceLogPeek{Path: p, Text: runtimecfg.RedactCredentialQuery(string(b)), Truncated: true}
}

// RefreshSlothServiceStatus runs a manual service/IPC status poll (UI "Retry").
func (a *App) RefreshSlothServiceStatus() ServiceState {
	a.refreshServiceStatus()
	a.appendRuntimeDiag("ipc.retry", "RefreshSlothServiceStatus")
	a.emitAppStateChanged()
	a.mu.RLock()
	defer a.mu.RUnlock()
	return a.state.Service
}

func extractEmbeddedDir(bundle embed.FS, prefix string, dest string) error {
	return fs.WalkDir(bundle, prefix, func(p string, d fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if d.IsDir() {
			return nil
		}
		rel, err := filepath.Rel(prefix, p)
		if err != nil || rel == "." || strings.HasPrefix(rel, "..") {
			return nil
		}
		target := filepath.Join(dest, rel)
		if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
			return err
		}
		data, err := bundle.ReadFile(p)
		if err != nil {
			return err
		}
		if err := os.WriteFile(target, data, 0o755); err != nil {
			return err
		}
		if runtime.GOOS != "windows" {
			_ = os.Chmod(target, 0o755)
		}
		return nil
	})
}

func installServiceElevatedWindows(installPath, workDir, coreHashes string) ([]byte, error) {
	psExe := filepath.Join(os.Getenv("SystemRoot"), "System32", "WindowsPowerShell", "v1.0", "powershell.exe")
	if _, err := os.Stat(psExe); err != nil {
		psExe = "powershell.exe"
	}
	esc := func(s string) string { return strings.ReplaceAll(s, "'", "''") }
	// The installer persists the pinned core hash(es) into the service's own
	// environment (SLOTH_CLASH_CORE_SHA256) so the service can content-verify the
	// core it spawns. Passed as an ARGUMENT, not an env var: a -Verb RunAs child
	// does not inherit this process's environment. Hex-only, so quoting is safe.
	var args []string
	if strings.TrimSpace(coreHashes) != "" {
		args = append(args, "'--core-sha256'", fmt.Sprintf("'%s'", esc(coreHashes)))
	}
	// Ask the installer to place the service next to this app (one Program
	// Files tree instead of two). The installer treats the value as untrusted
	// and only honours it when it resolves inside Program Files, so a
	// dev/portable exe dir is simply ignored in favour of its own fallback.
	// NOTE: Start-Process joins -ArgumentList with spaces WITHOUT re-quoting,
	// so the path (…\Sloth Clash) must carry its own double quotes or it would
	// reach the installer split across two argv entries.
	if exe, err := os.Executable(); err == nil {
		if dir := filepath.Dir(exe); dir != "" {
			args = append(args, "'--install-dir'", fmt.Sprintf(`'"%s"'`, esc(dir)))
		}
	}
	argList := ""
	if len(args) > 0 {
		argList = " -ArgumentList " + strings.Join(args, ",")
	}
	// Windows PowerShell 5.x: Start-Process has -FilePath, not -LiteralPath.
	script := fmt.Sprintf(
		"$ErrorActionPreference='Stop'; Start-Process -FilePath '%s' -WorkingDirectory '%s'%s -Verb RunAs -Wait; exit $LASTEXITCODE",
		esc(installPath),
		esc(workDir),
		argList,
	)
	cmd := exec.Command(psExe, "-NoProfile", "-NonInteractive", "-ExecutionPolicy", "Bypass", "-Command", script)
	if attr := hideWindowSysProcAttr(); attr != nil {
		cmd.SysProcAttr = attr
	}
	return cmd.CombinedOutput()
}

func installServiceElevatedDarwin(installPath, workDir, coreHashes string) ([]byte, error) {
	_ = os.Chmod(installPath, 0o755)
	esc := func(s string) string { return strings.ReplaceAll(s, "'", "'\\''") }
	// Pass the pinned core hash(es) so the installer records them in the launchd
	// plist EnvironmentVariables (SLOTH_CLASH_CORE_SHA256). Hex-only → quote-safe.
	coreArg := ""
	if strings.TrimSpace(coreHashes) != "" {
		coreArg = fmt.Sprintf(" --core-sha256 '%s'", esc(coreHashes))
	}
	// Some installer builds explicitly require launching through sudo/pkexec.
	// We use sudo under AppleScript elevation to satisfy that check reliably.
	shellCmd := fmt.Sprintf("cd '%s' && /usr/bin/sudo '%s'%s", esc(workDir), esc(installPath), coreArg)
	appleScript := fmt.Sprintf("do shell script %q with administrator privileges", shellCmd)
	cmd := exec.Command("osascript", "-e", appleScript)
	return cmd.CombinedOutput()
}

func findServiceInstaller(dir string) (string, error) {
	var candidates []string
	_ = filepath.WalkDir(dir, func(p string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return nil
		}
		base := strings.ToLower(filepath.Base(p))
		if strings.HasSuffix(base, ".gitkeep") {
			return nil
		}
		if strings.Contains(base, "sloth-clash-service-install") ||
			strings.Contains(base, "clash-verge-service-install") {
			candidates = append(candidates, p)
		}
		return nil
	})
	if len(candidates) == 0 {
		return "", errors.New("installer not found in embedded bundle")
	}
	// Prefer Sloth-named installer, then legacy Verge upstream bundle.
	for _, c := range candidates {
		if strings.EqualFold(filepath.Base(c), "sloth-clash-service-install.exe") {
			return c, nil
		}
	}
	for _, c := range candidates {
		if strings.EqualFold(filepath.Base(c), "clash-verge-service-install.exe") {
			return c, nil
		}
	}
	return candidates[0], nil
}
