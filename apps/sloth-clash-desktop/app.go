package main

import (
	"context"
	"embed"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"runtime"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/wailsapp/wails/v2/pkg/options"
	wailsrt "github.com/wailsapp/wails/v2/pkg/runtime"
)

const coreModeApplyTimeout = 12 * time.Second

// App struct
type App struct {
	ctx                 context.Context
	mu                  sync.RWMutex
	state               AppState
	profiles            []Profile
	update              UpdateState
	bundle              embed.FS
	coreSecret          string
	coreListen          string
	coreOverPipe        bool // Windows: mihomo started by sloth_clash_service; API on coreListen named pipe
	coreCmd             *exec.Cmd
	coreCancel          context.CancelFunc
	coreStopIntentional bool
	coreProcToken       uint64
	systemProxyLeased   bool // Windows: we set HKCU system proxy to mixed-port; clear on disconnect/stop
	systemProxySnapshot map[string]SystemProxyServiceSnapshot
	tunTakenOver        []tunServiceTakeover
	connectGen          atomic.Uint64 // bumped when starting async connect or on Disconnect; invalidates in-flight worker
	reconnectInFlight   atomic.Bool
	reconnectQueued     atomic.Bool
	closeToTray         bool
	quitRequested       bool

	emitStateMu           sync.Mutex
	emitStateTimer        *time.Timer
	insightRefreshRunning atomic.Bool
}

// NewApp creates a new App application struct
func NewApp(bundle embed.FS) *App {
	now := time.Now().Unix()
	return &App{
		bundle: bundle,
		state: AppState{
			Connection: ConnectionState{Status: "disconnected", Health: ""},
			Mode:       ModeState{Current: "rule", LastNonDirectMode: "rule"},
			Traffic:    "proxy",
			Profile: ProfileState{
				Profiles: []Profile{},
			},
			Proxy:   ProxyState{Groups: []ProxyGroup{}},
			Service: ServiceState{},
			Core: CoreState{
				Version:   "stopped",
				Lifecycle: "stopped",
			},
			Insight: HomeInsight{},
			UI: UIState{
				ActiveScreen: "home",
			},
			UpdatedAt: now,
		},
		profiles: []Profile{},
		update: UpdateState{
			Channel:        "stable",
			CurrentVersion: AppVersion,
		},
		closeToTray: true,
	}
}

// startup is called when the app starts. The context is saved
// so we can call the runtime methods
func (a *App) startup(ctx context.Context) {
	a.ctx = ctx
	installDockReopenHook()
	a.loadProfilesFromDisk()
	a.refreshServiceStatus()
	if trayRuntimeEnabled() {
		startAppTray(a)
	}
	go a.startProfileAutoUpdateLoop(ctx)
	go a.updateCheckLoop(ctx)
	a.emitAppStateChanged()
	// Deep link may arrive before the webview attaches EventsOn — short delay on cold start only.
	args := os.Args[1:]
	if len(args) > 0 && findSlothclashInstallConfigURL(args) != "" {
		go func() {
			time.Sleep(450 * time.Millisecond)
			a.tryInstallConfigFromArgs(args)
		}()
	}
}

func (a *App) shutdown(ctx context.Context) {
	_ = ctx
	a.emitStateMu.Lock()
	if a.emitStateTimer != nil {
		a.emitStateTimer.Stop()
		a.emitStateTimer = nil
	}
	a.emitStateMu.Unlock()
	// Do not tear down the macOS menu bar tray here. Wails may invoke shutdown during
	// lifecycle transitions that are not a full process exit; removing the status item
	// makes the tray "flicker" away while the app is still running.
	a.connectGen.Add(1)
	a.mu.Lock()
	a.stopCoreLocked()
	a.mu.Unlock()
}

func trayEnabled() bool {
	if v := strings.TrimSpace(strings.ToLower(os.Getenv("SLOTH_DISABLE_TRAY"))); v == "1" || v == "true" || v == "yes" {
		return false
	}
	if v := strings.TrimSpace(strings.ToLower(os.Getenv("SLOTH_ENABLE_EXPERIMENTAL_TRAY"))); v != "" {
		return v == "1" || v == "true" || v == "yes"
	}
	// Default on for macOS when backend exists.
	return runtime.GOOS == "darwin"
}

func trayRuntimeEnabled() bool {
	return trayEnabled() && trayBackendAvailable()
}

func (a *App) GetTrayAvailability() bool {
	return trayRuntimeEnabled() && trayIsReady()
}

func (a *App) SetCloseToTrayPreference(enabled bool) AppState {
	a.mu.Lock()
	a.closeToTray = enabled
	a.state.UpdatedAt = time.Now().Unix()
	out := a.state
	a.mu.Unlock()
	return out
}

func (a *App) MarkQuitIntent() {
	a.mu.Lock()
	a.quitRequested = true
	a.mu.Unlock()
}

func (a *App) beforeClose(ctx context.Context) (prevent bool) {
	if !trayRuntimeEnabled() || !trayIsReady() {
		return false
	}
	a.mu.Lock()
	closeToTray := a.closeToTray
	quitRequested := a.quitRequested
	if quitRequested {
		a.quitRequested = false
	}
	a.mu.Unlock()
	if quitRequested || !closeToTray {
		return false
	}
	go wailsrt.WindowHide(ctx)
	return true
}

func queryWindowsServiceStatus(name string) (installed bool, running bool, lastErr string) {
	if runtime.GOOS != "windows" {
		return false, false, ""
	}
	cmd := exec.Command("sc.exe", "query", name)
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
	default:
		return
	}
	a.mu.Lock()
	a.state.Service.Installed = installed
	a.state.Service.Running = running
	a.state.Service.LastError = strings.TrimSpace(lastErr)
	a.state.UpdatedAt = time.Now().Unix()
	a.mu.Unlock()
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

// OnSecondInstance is wired from main.go when SingleInstanceLock fires (e.g. slothclash:// opened while running).
func (a *App) OnSecondInstance(data options.SecondInstanceData) {
	a.tryInstallConfigFromArgs(data.Args)
	if a.ctx != nil {
		wailsrt.WindowShow(a.ctx)
		wailsrt.WindowUnminimise(a.ctx)
	}
}

func (a *App) tryInstallConfigFromArgs(args []string) {
	raw := findSlothclashInstallConfigURL(args)
	if raw == "" {
		return
	}
	go a.handleInstallConfigURL(raw)
}

func (a *App) handleInstallConfigURL(raw string) {
	name, subURL, err := ParseInstallConfigURL(raw)
	if err != nil {
		a.emitInstallConfigResult(false, err.Error(), "", "")
		return
	}
	st, err := a.ImportProfileFromURL(name, subURL)
	if err != nil {
		a.emitInstallConfigResult(false, err.Error(), "", "")
		return
	}
	a.emitAppStateChanged()
	pid := strings.TrimSpace(st.Profile.ActiveProfileID)
	var pname string
	for _, p := range st.Profile.Profiles {
		if p.ID == pid {
			pname = p.Name
			break
		}
	}
	a.emitInstallConfigResult(true, "Subscription added", pid, pname)
}

func (a *App) emitInstallConfigResult(success bool, message, profileID, profileName string) {
	if a.ctx == nil {
		return
	}
	payload := map[string]any{
		"success": success,
		"message": message,
	}
	if profileID != "" {
		payload["profileId"] = profileID
	}
	if profileName != "" {
		payload["profileName"] = profileName
	}
	go wailsrt.EventsEmit(a.ctx, "app:install-config", payload)
}

func (a *App) emitAppStateChanged() {
	if a.ctx == nil {
		return
	}
	ctx := a.ctx
	a.emitStateMu.Lock()
	if a.emitStateTimer != nil {
		a.emitStateTimer.Stop()
	}
	a.emitStateTimer = time.AfterFunc(48*time.Millisecond, func() {
		a.emitStateMu.Lock()
		a.emitStateTimer = nil
		a.emitStateMu.Unlock()
		if ctx == nil {
			return
		}
		go wailsrt.EventsEmit(ctx, "app:state")
	})
	a.emitStateMu.Unlock()
}

func (a *App) GetAppState() AppState {
	a.mu.RLock()
	defer a.mu.RUnlock()
	return a.state
}

// GetPreferredLanguage returns installer/system preferred UI language for first-run.
func (a *App) GetPreferredLanguage() string {
	lang := strings.TrimSpace(detectPreferredLanguage())
	if lang == "ru" || lang == "zh" || lang == "en" {
		return lang
	}
	return ""
}

func (a *App) Connect() (AppState, error) {
	a.mu.Lock()
	if len(a.profiles) == 0 {
		a.mu.Unlock()
		return a.GetAppState(), errors.New("no profiles — import a subscription first")
	}
	if a.state.Profile.ActiveProfileID == "" {
		a.mu.Unlock()
		return a.GetAppState(), errors.New("no active profile — pick a profile under Profiles")
	}
	var active Profile
	found := false
	for _, p := range a.profiles {
		if p.ID == a.state.Profile.ActiveProfileID {
			active = p
			found = true
			break
		}
	}
	if !found {
		a.mu.Unlock()
		return a.GetAppState(), errors.New("active profile not found")
	}
	switch strings.TrimSpace(a.state.Connection.Status) {
	case "connected":
		a.mu.Unlock()
		return a.GetAppState(), nil
	case "connecting":
		// Idempotent: second tap while the async job runs (Verge-style UX, no scary error banner).
		a.mu.Unlock()
		return a.GetAppState(), nil
	}
	a.state.Connection.Status = "connecting"
	a.state.Connection.Health = ""
	a.state.Connection.LastError = ""
	a.state.Connection.LastWarning = ""
	a.state.UpdatedAt = time.Now().Unix()
	gen := a.connectGen.Add(1)
	a.mu.Unlock()

	go a.runConnectJob(active, gen)
	a.emitAppStateChanged()
	return a.GetAppState(), nil
}

func (a *App) runConnectJob(active Profile, gen uint64) {
	if err := a.startEmbeddedCore(active, gen); err != nil {
		a.finishConnectJobFailed(gen, err)
		return
	}
	if a.connectGen.Load() != gen {
		return
	}
	// Verge-style: treat "core is listening" as connected immediately. Pulling /proxies can block
	// for a long time while providers fetch; do not keep the UI stuck in "connecting".
	a.finishConnectJobOK(gen)
	if a.connectGen.Load() != gen {
		return
	}
	if err := a.connectAfterCoreStarts(gen); err != nil {
		if errors.Is(err, errConnectAborted) {
			return
		}
		a.finishPostConnectWarmupFailed(gen, err)
		return
	}
	a.emitAppStateChanged()
	go func() { _, _ = a.RefreshHomeInsight() }()
}

func (a *App) finishConnectJobFailed(gen uint64, err error) {
	if errors.Is(err, errConnectAborted) {
		return
	}
	var notify bool
	a.mu.Lock()
	if a.connectGen.Load() == gen && a.state.Connection.Status == "connecting" {
		a.stopCoreLocked()
		a.state.Connection.Status = "error"
		a.state.Connection.Health = ""
		a.state.Connection.LastError = err.Error()
		a.state.Core.LastError = err.Error()
		a.state.Core.Lifecycle = "degraded"
		a.state.UpdatedAt = time.Now().Unix()
		notify = true
	}
	a.mu.Unlock()
	if notify {
		a.emitAppStateChanged()
	}
}

func (a *App) finishConnectJobOK(gen uint64) {
	var notify bool
	a.mu.Lock()
	if a.connectGen.Load() == gen && a.state.Connection.Status == "connecting" {
		a.state.Connection.Status = "connected"
		a.markConnectionReadyLocked()
		a.state.Connection.Since = time.Now().Unix()
		a.state.Connection.LastError = ""
		a.state.UpdatedAt = time.Now().Unix()
		notify = true
	}
	a.mu.Unlock()
	if notify {
		a.emitAppStateChanged()
	}
}

// finishPostConnectWarmupFailed runs after we already marked "connected" but non-critical
// warmup steps (mode/proxy sync/system proxy) failed. Keep session alive and surface warning.
func (a *App) finishPostConnectWarmupFailed(gen uint64, err error) {
	var notify bool
	a.mu.Lock()
	if a.connectGen.Load() == gen && a.state.Connection.Status == "connected" {
		msg := strings.TrimSpace(err.Error())
		if msg != "" && !isIgnorableWarmupWarning(msg) {
			a.markConnectionDegradedLocked("Post-connect warmup issue: " + msg)
		}
		a.state.UpdatedAt = time.Now().Unix()
		notify = true
	}
	a.mu.Unlock()
	if notify {
		a.emitAppStateChanged()
	}
}

func appendConnectionWarningLocked(current string, next string) string {
	msg := strings.TrimSpace(next)
	if msg == "" || isIgnorableWarmupWarning(msg) {
		return current
	}
	if strings.TrimSpace(current) == "" {
		return msg
	}
	return strings.TrimSpace(current) + " | " + msg
}

func (a *App) markConnectionReadyLocked() {
	a.state.Connection.Health = "ready"
	a.state.Core.Lifecycle = "running"
}

func (a *App) markConnectionDegradedLocked(reason string) {
	if strings.TrimSpace(reason) != "" {
		a.state.Connection.LastWarning = appendConnectionWarningLocked(a.state.Connection.LastWarning, reason)
	}
	if strings.TrimSpace(a.state.Connection.LastWarning) == "" {
		return
	}
	a.state.Connection.Health = "degraded"
	a.state.Core.Lifecycle = "degraded"
}

func isIgnorableWarmupWarning(msg string) bool {
	s := strings.ToLower(strings.TrimSpace(msg))
	if s == "" {
		return true
	}
	if strings.Contains(s, "exit status 4") {
		return true
	}
	if strings.Contains(s, "parameters were not valid") {
		return true
	}
	return false
}

func formatTunTakeoverWarning(err error) string {
	msg := strings.TrimSpace(err.Error())
	if msg == "" {
		msg = "unknown error"
	}
	return "TUN: could not stop the other app's Windows service (often needs admin rights, or that app restarts the service). Staying connected — if routing is wrong, switch to System proxy or stop the conflicting service in Windows Services. Technical: " + msg
}

// connectAfterCoreStarts runs pull-proxies, mode API, and system-proxy steps without holding a.mu
// during network I/O (avoids deadlocking GetAppState / Wails bridge).
func (a *App) connectAfterCoreStarts(gen uint64) error {
	if a.connectGen.Load() != gen {
		return errConnectAborted
	}
	var listen, secret, mode, traffic string
	a.mu.Lock()
	listen = a.effectiveCoreEndpointLocked()
	if listen == "" {
		a.mu.Unlock()
		return errors.New("core not running")
	}
	secret = a.coreSecret
	mode = strings.TrimSpace(a.state.Mode.Current)
	traffic = strings.TrimSpace(a.state.Traffic)
	if mode != "rule" && mode != "global" && mode != "direct" {
		mode = "rule"
	}
	a.mu.Unlock()
	if a.connectGen.Load() != gen {
		return errConnectAborted
	}

	if traffic == "tun" {
		stopped, err := takeoverConflictingTunServices()
		if a.connectGen.Load() != gen {
			return errConnectAborted
		}
		a.mu.Lock()
		if err != nil {
			// Do not tear down the session: stopping another vendor's service often needs admin
			// rights; users can still use proxy path or stop Verge's service manually.
			a.markConnectionDegradedLocked(formatTunTakeoverWarning(err))
		} else {
			a.state.Connection.LastWarning = ""
			if len(stopped) > 0 {
				a.tunTakenOver = append([]tunServiceTakeover(nil), stopped...)
			}
		}
		a.mu.Unlock()
	} else {
		a.mu.Lock()
		a.state.Connection.LastWarning = ""
		a.mu.Unlock()
	}

	// /proxies is often empty right after the core starts (providers still loading). Retry briefly
	// so Active group / node are not blank until unrelated UI (e.g. warnings) triggers another tick.
	// Do not fail whole connect flow on transient /proxies errors (some providers temporarily return
	// backend-specific errors like "exit status 4" during warmup).
	var proxiesWarmupErr error
	for attempt := 0; ; attempt++ {
		if a.connectGen.Load() != gen {
			return errConnectAborted
		}
		if err := a.pullProxiesIntoState(); err != nil {
			proxiesWarmupErr = err
			if attempt >= 5 {
				break
			}
			time.Sleep(220 * time.Millisecond)
			continue
		}
		if a.connectGen.Load() != gen {
			return errConnectAborted
		}
		proxiesWarmupErr = nil
		a.mu.RLock()
		n := len(a.state.Proxy.Groups)
		a.mu.RUnlock()
		if n > 0 || attempt >= 5 {
			break
		}
		time.Sleep(220 * time.Millisecond)
	}
	if a.connectGen.Load() != gen {
		return errConnectAborted
	}
	if proxiesWarmupErr != nil {
		a.mu.Lock()
		msg := "Proxy groups are still warming up: " + strings.TrimSpace(proxiesWarmupErr.Error())
		a.markConnectionDegradedLocked(msg)
		a.mu.Unlock()
	}

	a.mu.Lock()
	_ = a.autoSelectProxyGroupLocked()
	activeGroup := strings.TrimSpace(a.state.Proxy.ActiveGroup)
	a.state.UpdatedAt = time.Now().Unix()
	a.mu.Unlock()
	// Push proxies + active group before mode/system-proxy steps (can be slow); fixes empty Home until warmup ends.
	a.emitAppStateChanged()
	if a.connectGen.Load() != gen {
		return errConnectAborted
	}

	modeCtx, modeCancel := context.WithTimeout(context.Background(), coreModeApplyTimeout)
	errMode := applyCoreModeHTTPWithGlobal(modeCtx, listen, secret, mode, activeGroup)
	modeCancel()
	if errMode != nil {
		a.mu.Lock()
		a.markConnectionDegradedLocked("Could not apply core mode immediately: " + strings.TrimSpace(errMode.Error()))
		a.mu.Unlock()
	}
	if a.connectGen.Load() != gen {
		return errConnectAborted
	}
	if err := a.pullProxiesIntoState(); err != nil {
		a.mu.Lock()
		msg := "Could not refresh proxy groups after mode apply: " + strings.TrimSpace(err.Error())
		a.markConnectionDegradedLocked(msg)
		a.mu.Unlock()
	}
	a.mu.Lock()
	if mode == "global" {
		a.state.Proxy.ActiveGroup = "GLOBAL"
	} else if strings.TrimSpace(a.state.Proxy.ActiveGroup) == "" {
		_ = a.autoSelectProxyGroupLocked()
	}
	a.state.UpdatedAt = time.Now().Unix()
	a.mu.Unlock()
	// Second pull carries updated `now` selections — push before system-proxy (can be slow).
	a.emitAppStateChanged()
	if a.connectGen.Load() != gen {
		return errConnectAborted
	}

	if err := a.clearSystemProxyFromSnapshot(); err != nil {
		a.mu.Lock()
		a.markConnectionDegradedLocked("Could not clear system proxy snapshot: " + strings.TrimSpace(err.Error()))
		a.mu.Unlock()
	}
	if a.connectGen.Load() != gen {
		return errConnectAborted
	}

	if err := a.applySystemProxyFromSnapshot(); err != nil {
		a.mu.Lock()
		a.markConnectionDegradedLocked("Could not apply system proxy snapshot: " + strings.TrimSpace(err.Error()))
		a.mu.Unlock()
	}
	return nil
}

func (a *App) Disconnect() AppState {
	a.connectGen.Add(1)
	a.mu.Lock()
	a.stopCoreLocked()
	a.state.Connection.Status = "disconnected"
	a.state.Connection.Health = ""
	a.state.Connection.Since = 0
	a.state.Connection.LastError = ""
	a.state.Connection.LastWarning = ""
	a.state.Proxy.Groups = []ProxyGroup{}
	a.state.Insight = HomeInsight{}
	a.state.UpdatedAt = time.Now().Unix()
	a.mu.Unlock()
	a.emitAppStateChanged()
	return a.GetAppState()
}

func (a *App) SetMode(mode string) (AppState, error) {
	if mode != "rule" && mode != "global" && mode != "direct" {
		return a.GetAppState(), errors.New("invalid mode")
	}

	a.mu.Lock()
	connected := a.state.Connection.Status == "connected"
	listen := a.effectiveCoreEndpointLocked()
	secret := a.coreSecret
	activeGroup := strings.TrimSpace(a.state.Proxy.ActiveGroup)
	a.mu.Unlock()

	if connected && listen != "" {
		modeCtx, modeCancel := context.WithTimeout(context.Background(), coreModeApplyTimeout)
		err := applyCoreModeHTTPWithGlobal(modeCtx, listen, secret, mode, activeGroup)
		modeCancel()
		if err != nil {
			return a.GetAppState(), err
		}
		if err := a.pullProxiesIntoState(); err != nil {
			return a.GetAppState(), err
		}
	}

	a.mu.Lock()
	defer a.mu.Unlock()
	if mode != "direct" {
		a.state.Mode.LastNonDirectMode = mode
	}
	a.state.Mode.Current = mode
	if mode == "global" {
		a.state.Proxy.ActiveGroup = "GLOBAL"
	} else {
		// Leaving global mode: UI focus should not stay on "GLOBAL" in rule/direct.
		if strings.EqualFold(strings.TrimSpace(a.state.Proxy.ActiveGroup), "GLOBAL") {
			a.state.Proxy.ActiveGroup = ""
			_ = a.autoSelectProxyGroupLocked()
		} else if strings.TrimSpace(a.state.Proxy.ActiveGroup) == "" {
			_ = a.autoSelectProxyGroupLocked()
		}
	}
	a.state.UpdatedAt = time.Now().Unix()
	return a.state, nil
}

func (a *App) SetTrafficMode(mode string) (AppState, error) {
	a.mu.Lock()
	if mode != "proxy" && mode != "tun" {
		a.mu.Unlock()
		return a.GetAppState(), errors.New("invalid traffic mode")
	}
	if mode == "tun" && !a.state.Service.Installed {
		a.mu.Unlock()
		return a.GetAppState(), errors.New("service required")
	}
	prev := a.state.Traffic
	connected := a.state.Connection.Status == "connected"
	needsCoreRestart := connected && prev != mode
	a.state.Traffic = mode
	// When reconnecting the core for a traffic-mode change, still clear OS proxy
	// before tear-down (otherwise proxy→TUN could leave HKCU proxy stuck and block).
	if needsCoreRestart && prev == "proxy" {
		a.clearSystemProxyLocked()
	}
	if connected && !needsCoreRestart {
		if prev == "proxy" && mode == "tun" {
			a.clearSystemProxyLocked()
		}
		if mode == "proxy" {
			_ = a.applySystemProxyIfNeededLocked()
		}
	}
	a.state.UpdatedAt = time.Now().Unix()
	a.mu.Unlock()

	if needsCoreRestart {
		// Disconnect may block on Windows IPC stop; run off the Wails bridge thread.
		go a.reconnectActiveProfile()
		return a.GetAppState(), nil
	}
	return a.GetAppState(), nil
}

func (a *App) EnsureTunReady() TunSetupResult {
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.state.Service.Installed {
		a.state.Traffic = "tun"
		a.state.UpdatedAt = time.Now().Unix()
		return TunSetupResult{Success: true, Message: "TUN enabled", InstallAction: false}
	}
	return TunSetupResult{
		Success:       false,
		Message:       "Service required. Install service to continue or use Proxy mode.",
		InstallAction: true,
	}
}

func (a *App) ListProfiles() []Profile {
	a.mu.RLock()
	defer a.mu.RUnlock()
	return a.profiles
}

func (a *App) ImportProfileFromURL(name string, rawURL string) (AppState, error) {
	rawURL = strings.TrimSpace(rawURL)
	if rawURL == "" {
		return a.GetAppState(), errors.New("subscription url is required")
	}

	norm, err := normalizeSubscriptionURL(rawURL)
	if err != nil {
		return a.GetAppState(), err
	}

	ctx, cancel := context.WithTimeout(context.Background(), 26*time.Second)
	defer cancel()

	finalName, peek, err := resolveSubscriptionName(ctx, name, norm)
	if err != nil {
		return a.GetAppState(), err
	}

	a.mu.Lock()
	defer a.mu.Unlock()

	p := Profile{
		ID:                        "profile-" + time.Now().Format("20060102150405"),
		Name:                      finalName,
		Type:                      "subscription",
		URL:                       norm,
		SubscriptionInfo:          strings.TrimSpace(peek.SubscriptionInfo),
		LastUpdated:               time.Now().Unix(),
		AutoUpdateEnabled:         true,
		AutoUpdateIntervalMinutes: defaultProfileAutoUpdateMinutes,
	}
	a.profiles = append(a.profiles, p)
	a.state.Profile.Profiles = a.profiles
	if a.state.Profile.ActiveProfileID == "" {
		a.state.Profile.ActiveProfileID = p.ID
	}
	a.state.UpdatedAt = time.Now().Unix()
	if err := a.persistProfilesLocked(); err != nil {
		return a.state, err
	}
	return a.state, nil
}

func (a *App) ActivateProfile(profileID string) (AppState, error) {
	a.mu.Lock()
	if profileID == "" {
		a.mu.Unlock()
		return a.state, errors.New("profile id is required")
	}
	found := false
	for _, p := range a.profiles {
		if p.ID == profileID {
			found = true
			break
		}
	}
	if !found {
		a.mu.Unlock()
		return a.state, errors.New("profile not found")
	}
	connected := a.state.Connection.Status == "connected"
	a.state.Profile.ActiveProfileID = profileID
	a.state.UpdatedAt = time.Now().Unix()
	if err := a.persistProfilesLocked(); err != nil {
		a.mu.Unlock()
		return a.state, err
	}
	a.mu.Unlock()
	if connected {
		go a.reconnectActiveProfile()
	}
	a.emitAppStateChanged()
	return a.GetAppState(), nil
}

func (a *App) DeleteProfile(profileID string) (AppState, error) {
	profileID = strings.TrimSpace(profileID)
	if profileID == "" {
		return a.GetAppState(), errors.New("profile id is required")
	}

	a.mu.Lock()
	idx := -1
	for i := range a.profiles {
		if a.profiles[i].ID == profileID {
			idx = i
			break
		}
	}
	if idx < 0 {
		a.mu.Unlock()
		return a.GetAppState(), errors.New("profile not found")
	}

	wasActive := a.state.Profile.ActiveProfileID == profileID
	a.profiles = append(a.profiles[:idx], a.profiles[idx+1:]...)
	a.state.Profile.Profiles = a.profiles
	if wasActive {
		if len(a.profiles) > 0 {
			a.state.Profile.ActiveProfileID = a.profiles[0].ID
		} else {
			a.state.Profile.ActiveProfileID = ""
		}
	}
	a.state.UpdatedAt = time.Now().Unix()
	connected := a.state.Connection.Status == "connected"
	nextActiveID := strings.TrimSpace(a.state.Profile.ActiveProfileID)
	if err := a.persistProfilesLocked(); err != nil {
		a.mu.Unlock()
		return a.GetAppState(), err
	}
	a.mu.Unlock()

	if wasActive && connected {
		if nextActiveID != "" {
			go a.reconnectActiveProfile()
		} else {
			go func() { _ = a.Disconnect() }()
		}
	}
	a.emitAppStateChanged()
	return a.GetAppState(), nil
}

// RenameProfile updates the display name only (subscription URL unchanged).
func (a *App) RenameProfile(profileID string, newName string) (AppState, error) {
	return a.UpdateProfileInfo(profileID, newName, "")
}

// UpdateProfileInfo updates display name and optionally the subscription URL (empty url = leave unchanged).
func (a *App) UpdateProfileInfo(profileID string, displayName string, subscriptionURL string) (AppState, error) {
	displayName = strings.TrimSpace(displayName)
	if profileID == "" {
		return a.GetAppState(), errors.New("profile id is required")
	}
	if displayName == "" {
		return a.GetAppState(), errors.New("name is required")
	}
	subscriptionURL = strings.TrimSpace(subscriptionURL)

	a.mu.Lock()
	defer a.mu.Unlock()
	found := false
	for i := range a.profiles {
		if a.profiles[i].ID != profileID {
			continue
		}
		found = true
		a.profiles[i].Name = displayName
		if subscriptionURL != "" {
			norm, err := normalizeSubscriptionURL(subscriptionURL)
			if err != nil {
				return a.state, err
			}
			a.profiles[i].URL = norm
		}
		a.profiles[i].LastUpdated = time.Now().Unix()
		break
	}
	if !found {
		return a.state, errors.New("profile not found")
	}
	a.state.Profile.Profiles = a.profiles
	a.state.UpdatedAt = time.Now().Unix()
	if err := a.persistProfilesLocked(); err != nil {
		return a.state, err
	}
	return a.state, nil
}

// SetProfileMergeTemplate stores the Verge-style merge YAML for a profile and clears manual-config pinning.
func (a *App) SetProfileMergeTemplate(profileID string, template string) (AppState, error) {
	profileID = strings.TrimSpace(profileID)
	if profileID == "" {
		return a.GetAppState(), errors.New("profile id is required")
	}
	a.mu.Lock()
	active := false
	connected := a.state.Connection.Status == "connected"
	found := false
	for i := range a.profiles {
		if a.profiles[i].ID != profileID {
			continue
		}
		found = true
		a.profiles[i].MergeTemplate = template
		a.profiles[i].SkipAutoConfig = false
		a.profiles[i].LastUpdated = time.Now().Unix()
		break
	}
	if !found {
		a.mu.Unlock()
		return a.GetAppState(), errors.New("profile not found")
	}
	a.state.Profile.Profiles = a.profiles
	a.state.UpdatedAt = time.Now().Unix()
	active = a.state.Profile.ActiveProfileID == profileID
	if err := a.persistProfilesLocked(); err != nil {
		a.mu.Unlock()
		return a.GetAppState(), err
	}
	a.mu.Unlock()

	if active && connected {
		go a.reconnectActiveProfile()
	}
	a.emitAppStateChanged()
	return a.GetAppState(), nil
}

// SetProfileProxyTemplate stores proxy-groups editor YAML (separate from extend config).
func (a *App) SetProfileProxyTemplate(profileID string, template string) (AppState, error) {
	profileID = strings.TrimSpace(profileID)
	if profileID == "" {
		return a.GetAppState(), errors.New("profile id is required")
	}
	a.mu.Lock()
	active := false
	connected := a.state.Connection.Status == "connected"
	found := false
	for i := range a.profiles {
		if a.profiles[i].ID != profileID {
			continue
		}
		found = true
		a.profiles[i].ProxyTemplate = template
		a.profiles[i].SkipAutoConfig = false
		a.profiles[i].LastUpdated = time.Now().Unix()
		break
	}
	if !found {
		a.mu.Unlock()
		return a.GetAppState(), errors.New("profile not found")
	}
	a.state.Profile.Profiles = a.profiles
	a.state.UpdatedAt = time.Now().Unix()
	active = a.state.Profile.ActiveProfileID == profileID
	if err := a.persistProfilesLocked(); err != nil {
		a.mu.Unlock()
		return a.GetAppState(), err
	}
	a.mu.Unlock()
	if active && connected {
		go a.reconnectActiveProfile()
	}
	a.emitAppStateChanged()
	return a.GetAppState(), nil
}

// SetProfileRulesTemplate stores rules editor YAML (separate from extend config).
func (a *App) SetProfileRulesTemplate(profileID string, template string) (AppState, error) {
	profileID = strings.TrimSpace(profileID)
	if profileID == "" {
		return a.GetAppState(), errors.New("profile id is required")
	}
	a.mu.Lock()
	active := false
	connected := a.state.Connection.Status == "connected"
	found := false
	for i := range a.profiles {
		if a.profiles[i].ID != profileID {
			continue
		}
		found = true
		a.profiles[i].RulesTemplate = template
		a.profiles[i].SkipAutoConfig = false
		a.profiles[i].LastUpdated = time.Now().Unix()
		break
	}
	if !found {
		a.mu.Unlock()
		return a.GetAppState(), errors.New("profile not found")
	}
	a.state.Profile.Profiles = a.profiles
	a.state.UpdatedAt = time.Now().Unix()
	active = a.state.Profile.ActiveProfileID == profileID
	if err := a.persistProfilesLocked(); err != nil {
		a.mu.Unlock()
		return a.GetAppState(), err
	}
	a.mu.Unlock()
	if active && connected {
		go a.reconnectActiveProfile()
	}
	a.emitAppStateChanged()
	return a.GetAppState(), nil
}

func (a *App) InstallService() (TunSetupResult, error) {
	tmpDir, err := os.MkdirTemp("", "sloth-clash-service-*")
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

	var out []byte
	var runErr error
	if runtime.GOOS == "windows" {
		if a.ctx != nil {
			wailsrt.WindowMinimise(a.ctx)
		}
		out, runErr = installServiceElevatedWindows(installPath, tmpDir)
		if a.ctx != nil {
			wailsrt.WindowShow(a.ctx)
			wailsrt.WindowUnminimise(a.ctx)
		}
	} else if runtime.GOOS == "darwin" {
		out, runErr = installServiceElevatedDarwin(installPath, tmpDir)
	} else {
		cmd := exec.Command(installPath)
		cmd.Dir = tmpDir
		out, runErr = cmd.CombinedOutput()
	}
	if runErr != nil {
		_ = os.RemoveAll(tmpDir)
		msg := strings.TrimSpace(string(out))
		if msg == "" {
			msg = runErr.Error()
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

	if runtime.GOOS == "windows" || runtime.GOOS == "darwin" {
		a.refreshServiceStatus()
	} else {
		a.mu.Lock()
		a.state.Service.Installed = true
		a.state.Service.Running = true
		a.state.Service.LastError = ""
		a.state.UpdatedAt = time.Now().Unix()
		a.mu.Unlock()
	}

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

func (a *App) SelectProxyGroup(name string) (AppState, error) {
	a.mu.Lock()
	defer a.mu.Unlock()
	if name == "" {
		return a.state, errors.New("group name is required")
	}
	upper := strings.ToUpper(name)
	if upper == "REJECT" || upper == "DIRECT" {
		return a.state, errors.New("unsafe auto group")
	}
	a.state.Proxy.ActiveGroup = name
	a.state.Proxy.LastGoodGroup = name
	a.state.UpdatedAt = time.Now().Unix()
	return a.state, nil
}

func (a *App) autoSelectProxyGroupLocked() error {
	if a.state.Proxy.LastGoodGroup != "" {
		if !isUnsafeGroup(a.state.Proxy.LastGoodGroup) && !strings.EqualFold(a.state.Proxy.LastGoodGroup, "GLOBAL") {
			a.state.Proxy.ActiveGroup = a.state.Proxy.LastGoodGroup
			a.state.UpdatedAt = time.Now().Unix()
			return nil
		}
	}
	for _, group := range a.state.Proxy.Groups {
		if !isUnsafeGroup(group.Name) && !strings.EqualFold(group.Name, "GLOBAL") {
			a.state.Proxy.ActiveGroup = group.Name
			a.state.Proxy.LastGoodGroup = group.Name
			a.state.UpdatedAt = time.Now().Unix()
			return nil
		}
	}
	if len(a.state.Proxy.Groups) == 0 {
		return errors.New("no proxy groups yet (wait for subscription health check)")
	}
	return errors.New("no safe proxy group found")
}

func (a *App) AutoSelectProxyGroup() (AppState, error) {
	a.mu.Lock()
	defer a.mu.Unlock()
	if err := a.autoSelectProxyGroupLocked(); err != nil {
		return a.state, err
	}
	return a.state, nil
}

func (a *App) RefreshProxies() (AppState, error) {
	if err := a.pullProxiesIntoState(); err != nil {
		return a.GetAppState(), err
	}
	a.mu.Lock()
	defer a.mu.Unlock()
	a.state.UpdatedAt = time.Now().Unix()
	return a.state, nil
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
	root, err := slothDataRoot()
	if err != nil {
		return ServiceLogPeek{LastError: err.Error()}
	}
	runtimeDir := filepath.Join(root, "runtime", pid)
	candidates := []string{
		filepath.Join(runtimeDir, "logs", "service_latest.log"),
		filepath.Join(runtimeDir, "core.log"),
		filepath.Join(runtimeDir, "logs", "service.log"),
	}
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
		return ServiceLogPeek{Path: p, Text: string(b)}
	}

	if _, err := f.Seek(-int64(maxBytes), io.SeekEnd); err != nil {
		return ServiceLogPeek{Path: p, LastError: err.Error()}
	}
	b, err := io.ReadAll(io.LimitReader(f, int64(maxBytes)+1))
	if err != nil {
		return ServiceLogPeek{Path: p, LastError: err.Error()}
	}
	return ServiceLogPeek{Path: p, Text: string(b), Truncated: true}
}

func (a *App) SetProxyNode(groupName, proxyName string) (AppState, error) {
	groupName = strings.TrimSpace(groupName)
	proxyName = strings.TrimSpace(proxyName)
	if groupName == "" || proxyName == "" {
		return a.GetAppState(), errors.New("group and proxy name are required")
	}

	var listen, secret string
	a.mu.Lock()
	listen = a.effectiveCoreEndpointLocked()
	if listen == "" {
		a.mu.Unlock()
		return a.GetAppState(), errors.New("core not running")
	}
	secret = a.coreSecret
	a.mu.Unlock()

	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	if err := putProxySelectionAt(ctx, listen, secret, groupName, proxyName); err != nil {
		return a.GetAppState(), err
	}
	if err := a.pullProxiesIntoState(); err != nil {
		return a.GetAppState(), err
	}

	a.mu.Lock()
	defer a.mu.Unlock()
	a.state.UpdatedAt = time.Now().Unix()
	return a.state, nil
}

func (a *App) GetUpdateState() UpdateState {
	a.mu.RLock()
	defer a.mu.RUnlock()
	return a.update
}

func (a *App) SetUpdateChannel(channel string) (UpdateState, error) {
	a.mu.Lock()
	defer a.mu.Unlock()
	if channel != "stable" {
		return a.update, errors.New("invalid update channel")
	}
	a.update.Channel = channel
	return a.update, nil
}

func (a *App) GetTunStatus() ServiceState {
	a.refreshServiceStatus()
	a.mu.RLock()
	defer a.mu.RUnlock()
	return a.state.Service
}

// FetchRulesOverview reads rules from the embedded Sloth core when connected; otherwise
// falls back to SLOTH_CLASH_CONTROLLER / SLOTH_CLASH_SECRET (e.g. external Verge).
func (a *App) FetchRulesOverview() RulesOverview {
	a.mu.RLock()
	conn := strings.TrimSpace(a.state.Connection.Status)
	running := a.state.Core.Running
	ep := a.effectiveCoreEndpointLocked()
	secret := a.coreSecret
	a.mu.RUnlock()
	// Do not require Core.Running: it can lag in the UI; "connected" + controller endpoint is enough (Verge-style).
	if ep != "" && (conn == "connected" || running) {
		return a.rulesOverviewFetch(ep, secret)
	}

	base := strings.TrimSpace(os.Getenv("SLOTH_CLASH_CONTROLLER"))
	if base == "" {
		return RulesOverview{LastError: "connect Sloth or set SLOTH_CLASH_CONTROLLER for external core"}
	}
	if !strings.HasPrefix(base, "http://") && !strings.HasPrefix(base, "https://") {
		base = "http://" + base
	}
	base = strings.TrimRight(base, "/")
	envSecret := strings.TrimSpace(os.Getenv("SLOTH_CLASH_SECRET"))

	client := &http.Client{Timeout: 4 * time.Second}
	out := RulesOverview{Controller: base}

	req, err := http.NewRequestWithContext(context.Background(), http.MethodGet, base+"/rules", nil)
	if err != nil {
		out.LastError = err.Error()
		return out
	}
	if envSecret != "" {
		req.Header.Set("Authorization", "Bearer "+envSecret)
	}
	resp, err := client.Do(req)
	if err != nil {
		out.LastError = "GET /rules: " + err.Error()
		return out
	}
	func() {
		defer resp.Body.Close()
		if resp.StatusCode < 200 || resp.StatusCode >= 300 {
			b, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
			out.LastError = "GET /rules: HTTP " + strconv.Itoa(resp.StatusCode) + " " + strings.TrimSpace(string(b))
			return
		}
		body, rerr := io.ReadAll(io.LimitReader(resp.Body, 256*1024))
		if rerr != nil {
			out.LastError = "GET /rules: " + rerr.Error()
			return
		}
		out.Reachable = true
		out.RulesBody = truncateString(string(body), 14000)
	}()

	if out.LastError != "" {
		return out
	}

	req2, err := http.NewRequestWithContext(context.Background(), http.MethodGet, base+"/providers/rules", nil)
	if err != nil {
		return out
	}
	if envSecret != "" {
		req2.Header.Set("Authorization", "Bearer "+envSecret)
	}
	resp2, err := client.Do(req2)
	if err != nil {
		return out
	}
	defer resp2.Body.Close()
	if resp2.StatusCode < 200 || resp2.StatusCode >= 300 {
		return out
	}
	body2, err := io.ReadAll(io.LimitReader(resp2.Body, 256*1024))
	if err != nil {
		return out
	}
	out.RuleProvidersBody = truncateString(string(body2), 10000)
	return out
}

func truncateString(s string, maxRunes int) string {
	r := []rune(s)
	if len(r) <= maxRunes {
		return s
	}
	return string(r[:maxRunes]) + "\n…(truncated)"
}

func isUnsafeGroup(name string) bool {
	upper := strings.ToUpper(strings.TrimSpace(name))
	return upper == "REJECT" || upper == "DIRECT"
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

func installServiceElevatedWindows(installPath, workDir string) ([]byte, error) {
	psExe := filepath.Join(os.Getenv("SystemRoot"), "System32", "WindowsPowerShell", "v1.0", "powershell.exe")
	if _, err := os.Stat(psExe); err != nil {
		psExe = "powershell.exe"
	}
	esc := func(s string) string { return strings.ReplaceAll(s, "'", "''") }
	// Windows PowerShell 5.x: Start-Process has -FilePath, not -LiteralPath.
	script := fmt.Sprintf(
		"$ErrorActionPreference='Stop'; Start-Process -FilePath '%s' -WorkingDirectory '%s' -Verb RunAs -Wait; exit $LASTEXITCODE",
		esc(installPath),
		esc(workDir),
	)
	cmd := exec.Command(psExe, "-NoProfile", "-NonInteractive", "-ExecutionPolicy", "Bypass", "-Command", script)
	return cmd.CombinedOutput()
}

func installServiceElevatedDarwin(installPath, workDir string) ([]byte, error) {
	_ = os.Chmod(installPath, 0o755)
	esc := func(s string) string { return strings.ReplaceAll(s, "'", "'\\''") }
	// Some installer builds explicitly require launching through sudo/pkexec.
	// We use sudo under AppleScript elevation to satisfy that check reliably.
	shellCmd := fmt.Sprintf("cd '%s' && /usr/bin/sudo '%s'", esc(workDir), esc(installPath))
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
