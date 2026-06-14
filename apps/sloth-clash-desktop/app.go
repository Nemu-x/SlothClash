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
	coreOverPipe        bool // mihomo started by sloth_clash_service; stop goes via IPC. API still uses coreListen (TCP).
	coreCmd             *exec.Cmd
	coreCancel          context.CancelFunc
	coreStopIntentional bool
	coreProcToken       uint64
	// coreActiveProfileID is the profile the running core was started for.
	// In the reload model (v0.3+) Mihomo keeps running across Connect/Disconnect;
	// we only tear it down when the profile itself changes (ActivateProfile with a
	// different ID) or on app shutdown.
	coreActiveProfileID string
	coreLifecycleMu     sync.Mutex // serializes ensureCoreForProfile across concurrent callers
	systemProxyLeased   bool       // Windows: we set HKCU system proxy to mixed-port; clear on disconnect/stop
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

	diagMu     sync.Mutex
	diagEvents []RuntimeDiagEvent
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
	registerDarwinLifecycleApp(a)
	installDockReopenHook()
	loadDesktopPrefs()
	a.loadProfilesFromDisk()
	a.refreshServiceStatus()
	if trayRuntimeEnabled() {
		startAppTray(a)
	}
	go a.startProfileAutoUpdateLoop(ctx)
	go a.updateCheckLoop(ctx)
	go a.runRuntimeSupervisorLoop(ctx)
	a.emitAppStateChanged()
	// Warm the core in the background for the active profile (cold start with
	// tun.enable: false) so the first Connect only has to push a single
	// PUT /configs?force=true reload with the desired TUN state instead of
	// paying the 2-5s cold-start cost of spawning Mihomo. Failures here are
	// non-fatal — Connect will retry the full ensureCoreForProfile path.
	go a.bootActiveProfileCoreInBackground()
	// Deep link may arrive before the webview attaches EventsOn — short delay on cold start only.
	args := os.Args[1:]
	if len(args) > 0 && findSlothclashInstallConfigURL(args) != "" {
		go func() {
			time.Sleep(450 * time.Millisecond)
			a.tryInstallConfigFromArgs(args)
		}()
	}
}

// bootActiveProfileCoreInBackground starts Mihomo for the currently active
// profile if one is present. Any error is logged to the debug channel but not
// surfaced to the UI — the user still sees "disconnected" and the core will
// be started on demand when they click Connect.
func (a *App) bootActiveProfileCoreInBackground() {
	a.mu.RLock()
	activeID := strings.TrimSpace(a.state.Profile.ActiveProfileID)
	traffic := strings.TrimSpace(a.state.Traffic)
	var profile Profile
	found := false
	for _, p := range a.profiles {
		if p.ID == activeID {
			profile = p
			found = true
			break
		}
	}
	a.mu.RUnlock()
	if !found {
		return
	}
	// Boot cores with TUN disabled — the user has not clicked Connect yet.
	// Connect() will bring TUN up via applyRuntimeConfig+PUT /configs force-reload
	// (matches clash-verge-rev's init path: start_core with current verge state,
	// then toggle_tun_mode goes through update_config → reload_config).
	coldStarted, err := a.ensureCoreForProfileEx(profile, 0, false)
	if err != nil {
		debugLog(
			"startup",
			"H1",
			"app.go:bootActiveProfileCoreInBackground",
			"background core boot failed (not fatal; Connect will retry)",
			map[string]any{
				"profileId": profile.ID,
				"error":     err.Error(),
			},
		)
		return
	}
	// Only sync the runtime config when the core was REUSED (survived an unclean
	// OS shutdown / was already started by the service) — there its actual TUN
	// state may not match "disconnected", so we force tun.enable=false to realign.
	// A FRESH cold start already loaded the generated YAML (tun.enable=false baked
	// in by startEmbeddedCore), so re-applying it here would just trigger a second
	// full provider re-pull for no behaviour change — the redundant pull that made
	// Connect drag on heavy/unhealthy-provider profiles. Mirrors runConnectJob's
	// own cold-start skip.
	if coldStarted {
		return
	}
	if err := a.applyRuntimeConfig(profile, traffic, false); err != nil {
		debugLog(
			"startup",
			"H1",
			"app.go:bootActiveProfileCoreInBackground",
			"startup runtime sync failed (Connect will retry)",
			map[string]any{
				"profileId": profile.ID,
				"traffic":   traffic,
				"error":     err.Error(),
			},
		)
	}
}

func (a *App) shutdown(ctx context.Context) {
	_ = ctx
	unregisterDarwinLifecycleApp(a)
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

	a.drainTunAndStopCore()
}

// tunDrainSettle is how long we wait after asking mihomo to disable TUN before
// killing the core, giving it time to fully remove the wintun/utun adapter.
const tunDrainSettle = 1200 * time.Millisecond

// drainTunAndStopCore disables TUN via the core (so the wintun/utun adapter
// unwinds cleanly) and then stops the core. Used on graceful shutdown and before
// an in-app update launches the installer.
//
// Without the TUN drain the process is killed while TUN is still bound, which on
// macOS can leave the utun adapter in an "on" zombie state and on Windows leaves
// wintun up. The in-app updater's installer kills this process to replace it,
// bypassing shutdown(); if the core (and its TUN adapter) survive the update, the
// next launch's first Connect hits an already-up TUN. See fix-tun-teardown-on-update.
func (a *App) drainTunAndStopCore() {
	a.mu.RLock()
	listen := a.effectiveCoreEndpointLocked()
	secret := a.coreSecret
	shouldDrainTun := a.state.Connection.Status == "connected" && strings.TrimSpace(a.state.Traffic) == "tun"
	a.mu.RUnlock()
	if shouldDrainTun && strings.TrimSpace(listen) != "" {
		dctx, dcancel := context.WithTimeout(context.Background(), 3*time.Second)
		_ = coreSetTunEnabledAt(dctx, listen, secret, false)
		dcancel()
		// Let mihomo actually remove the wintun/utun adapter before we kill the
		// core. Without this settle the kill interrupts adapter teardown and the
		// device lingers (the "Meta" adapter stays up in Network Connections),
		// which trips the next launch's first Connect after a fast in-app update.
		time.Sleep(tunDrainSettle)
	}

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
	// Enabled by default on platforms where we ship a tray backend
	// (Windows: fyne systray + bundled .ico; macOS: NSStatusBar via cgo).
	// Stub builds return false from trayBackendAvailable().
	return runtime.GOOS == "darwin" || runtime.GOOS == "windows"
}

func trayRuntimeEnabled() bool {
	return trayEnabled() && trayBackendAvailable()
}

func (a *App) GetTrayAvailability() bool {
	return trayRuntimeEnabled() && trayIsReady()
}

// NavigateUIScreen switches the web UI to a main navigation screen. Used by the
// native macOS menu bar tray; the frontend listens for `app:navigate`.
func (a *App) NavigateUIScreen(screen string) {
	screen = strings.ToLower(strings.TrimSpace(screen))
	switch screen {
	case "home", "proxies", "profiles", "connections", "rules", "logs", "advanced", "settings":
	default:
		return
	}
	a.mu.Lock()
	a.state.UI.ActiveScreen = screen
	a.state.UpdatedAt = time.Now().Unix()
	a.mu.Unlock()
	if a.ctx != nil {
		wailsrt.WindowShow(a.ctx)
		wailsrt.WindowUnminimise(a.ctx)
		wailsrt.EventsEmit(a.ctx, "app:navigate", map[string]string{"screen": screen})
	}
	a.emitAppStateChanged()
}

func (a *App) SetCloseToTrayPreference(enabled bool) AppState {
	a.mu.Lock()
	a.closeToTray = enabled
	a.state.UpdatedAt = time.Now().Unix()
	out := a.state
	a.mu.Unlock()
	return out
}

// SetLaunchOnStartupPreference registers (or clears) the current binary in
// the platform autostart store. On Windows this writes to
// HKCU\Software\Microsoft\Windows\CurrentVersion\Run; on other platforms
// the call returns an informative error. The toggle is persisted by the
// OS itself, so the app does not need to mirror state on disk.
func (a *App) SetLaunchOnStartupPreference(enabled bool) error {
	return setLaunchOnStartup(enabled)
}

// GetLaunchOnStartupPreference reflects what the OS autostart store has
// registered for the current executable. Returns false if the entry is
// missing, points at a different binary, or the store is unreadable.
func (a *App) GetLaunchOnStartupPreference() bool {
	v, _ := getLaunchOnStartup()
	return v
}

// StartedMinimized reports whether the current process was launched with
// the --minimized flag (e.g. from the autostart Run key entry). The
// frontend uses this to hide the window immediately on first render so
// the user does not see a brief flash before settings are applied.
func (a *App) StartedMinimized() bool {
	for _, arg := range os.Args[1:] {
		if strings.EqualFold(strings.TrimSpace(arg), "--minimized") {
			return true
		}
	}
	return false
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

const maxRuntimeDiagEvents = 220
const maxRuntimeDiagMsgLen = 520

func (a *App) appendRuntimeDiag(category, message string) {
	cat := strings.TrimSpace(category)
	if cat == "" {
		return
	}
	msg := strings.TrimSpace(message)
	if len(msg) > maxRuntimeDiagMsgLen {
		msg = msg[:maxRuntimeDiagMsgLen] + "…"
	}
	ev := RuntimeDiagEvent{
		TsUnixMs: time.Now().UnixMilli(),
		Category: cat,
		Message:  msg,
	}
	a.diagMu.Lock()
	a.diagEvents = append(a.diagEvents, ev)
	if len(a.diagEvents) > maxRuntimeDiagEvents {
		a.diagEvents = a.diagEvents[len(a.diagEvents)-maxRuntimeDiagEvents:]
	}
	a.diagMu.Unlock()
}

// GetRuntimeDiagEvents returns recent runtime diagnostics (bounded ring).
func (a *App) GetRuntimeDiagEvents() []RuntimeDiagEvent {
	a.diagMu.Lock()
	defer a.diagMu.Unlock()
	if len(a.diagEvents) == 0 {
		return nil
	}
	out := make([]RuntimeDiagEvent, len(a.diagEvents))
	copy(out, a.diagEvents)
	return out
}

// GetPreferredLanguage returns installer/system preferred UI language for first-run.
func (a *App) GetPreferredLanguage() string {
	lang := strings.TrimSpace(detectPreferredLanguage())
	if lang == "ru" || lang == "zh" || lang == "en" {
		return lang
	}
	return ""
}

// Connect / Disconnect / connectAfterCoreStarts and helpers live in app_connect.go.

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
		// Leaving global mode: UI focus should not stay on "GLOBAL" in
		// rule/direct. Drop it and let the per-profile sticky pick take
		// over (no-op if the user never picked a group for this profile).
		if strings.EqualFold(strings.TrimSpace(a.state.Proxy.ActiveGroup), "GLOBAL") {
			a.state.Proxy.ActiveGroup = ""
		}
		a.restoreStickyGroupLocked()
	}
	a.state.UpdatedAt = time.Now().Unix()
	if err := a.persistProfilesLocked(); err != nil {
		return a.state, err
	}
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
	needsReload := connected && prev != mode
	a.state.Traffic = mode
	if err := a.persistProfilesLocked(); err != nil {
		a.mu.Unlock()
		return a.GetAppState(), err
	}
	if needsReload {
		if mode == "tun" {
			// Moving proxy → tun: clear OS proxy so DNS hijack routes through TUN.
			a.clearSystemProxyLocked()
		} else {
			// Moving tun → proxy: apply OS proxy for direct app traffic.
			_ = a.applySystemProxyIfNeededLocked()
		}
	}
	a.state.UpdatedAt = time.Now().Unix()
	activeID := strings.TrimSpace(a.state.Profile.ActiveProfileID)
	var active Profile
	activeFound := false
	for _, p := range a.profiles {
		if p.ID == activeID {
			active = p
			activeFound = true
			break
		}
	}
	listen := a.effectiveCoreEndpointLocked()
	a.mu.Unlock()

	if needsReload && activeFound && strings.TrimSpace(listen) != "" {
		// Reload-model traffic switch (aligned with clash-verge-rev
		// toggle_tun_mode → patch_verge → update_config → reload_config):
		// regenerate YAML with the new enableTun value and push PUT /configs
		// force-reload. Mihomo brings TUN up or down without restarting; we
		// never thrash wintun with PATCH flips.
		enableTun := mode == "tun"
		if err := a.applyRuntimeConfig(active, mode, enableTun); err != nil {
			a.mu.Lock()
			if a.state.Connection.Status == "connected" {
				a.markConnectionBrokenLocked("Traffic mode could not be applied to the running core: " + strings.TrimSpace(err.Error()))
			}
			a.mu.Unlock()
			a.emitAppStateChanged()
			return a.GetAppState(), err
		}
	}
	return a.GetAppState(), nil
}

func (a *App) EnsureTunReady() TunSetupResult {
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.state.Service.Installed {
		a.state.Traffic = "tun"
		a.state.UpdatedAt = time.Now().Unix()
		_ = a.persistProfilesLocked()
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

	finalName := strings.TrimSpace(name)
	ctx, cancel := context.WithTimeout(context.Background(), 35*time.Second)
	defer cancel()
	peek, peekErr := peekSubscription(ctx, norm)
	if finalName == "" {
		if peekErr != nil {
			return a.GetAppState(), peekErr
		}
		finalName = strings.TrimSpace(peek.SuggestedName)
		if finalName == "" {
			finalName = "Subscription"
		}
	} else if peekErr != nil {
		peek = SubscriptionPeek{}
	}
	subInfo := strings.TrimSpace(peek.SubscriptionInfo)
	supportURL := strings.TrimSpace(peek.SubscriptionSupportURL)
	announce := strings.TrimSpace(peek.SubscriptionAnnouncement)

	a.mu.Lock()
	defer a.mu.Unlock()

	p := Profile{
		ID:                        "profile-" + time.Now().Format("20060102150405"),
		Name:                      finalName,
		Type:                      "subscription",
		URL:                       norm,
		SubscriptionInfo:          subInfo,
		SubscriptionSupportURL:    supportURL,
		SubscriptionAnnouncement:  announce,
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

// ImportProfileFromText creates a "local" profile from pasted or file-loaded
// content: a Clash/mihomo YAML config, OR a list of share links (vless://,
// vmess://, ss://, trojan://, hysteria2://, tuic://) — a single link, a
// newline-separated list, or a base64 envelope. The content is validated, then
// seeded into the profile's body cache so the normal pipeline turns it into a
// runnable config on activation. Local profiles carry no URL and are never
// auto-refreshed.
func (a *App) ImportProfileFromText(name string, content string) (AppState, error) {
	content = strings.TrimSpace(content)
	if content == "" {
		return a.GetAppState(), errors.New("config content is required")
	}
	// Validate up front: it must parse as a Clash doc, base64-Clash, or a
	// supported share-link list. parseClashDocToMap covers all three.
	doc, skipped, perr := parseClashDocToMapReport([]byte(content))
	if perr != nil || len(doc) == 0 {
		if perr != nil {
			return a.GetAppState(), fmt.Errorf("not a valid config or supported share link: %w", perr)
		}
		return a.GetAppState(), errors.New("not a valid config or supported share link")
	}
	importedCount := 0
	if px, ok := doc["proxies"].([]any); ok {
		importedCount = len(px)
	}

	finalName := strings.TrimSpace(name)
	if finalName == "" {
		finalName = "Local config"
	}

	a.mu.Lock()
	defer a.mu.Unlock()

	id := "profile-" + time.Now().Format("20060102150405")
	root, derr := slothDataRoot()
	if derr != nil {
		return a.state, derr
	}
	// Seed the per-profile body cache (the pipeline's cache-first source). For a
	// local profile this is the durable home of the content — there is no URL to
	// re-fetch from — so ResetSubscriptionCache refuses to wipe local caches.
	writeSubscriptionBodyCache(filepath.Join(root, "runtime", id), []byte(content))

	p := Profile{
		ID:                id,
		Name:              finalName,
		Type:              "local",
		LastUpdated:       time.Now().Unix(),
		AutoUpdateEnabled: false,
	}
	a.profiles = append(a.profiles, p)
	a.state.Profile.Profiles = a.profiles
	if a.state.Profile.ActiveProfileID == "" {
		a.state.Profile.ActiveProfileID = p.ID
	}
	// Surface partial imports instead of silently dropping nodes: a V2Ray list
	// with unsupported schemes / malformed links keeps the good nodes and tells
	// the user how many were skipped. (Frontend can render this as a toast.)
	if len(skipped) > 0 {
		a.state.Connection.LastWarning = fmt.Sprintf(
			"Imported %d node(s); skipped %d unsupported or invalid share link(s).",
			importedCount, len(skipped),
		)
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
	profileChanged := a.state.Profile.ActiveProfileID != profileID
	a.state.Profile.ActiveProfileID = profileID
	if profileChanged {
		// ActiveGroup + cached /proxies belong to the previous profile —
		// drop them so the UI doesn't briefly highlight a group that
		// doesn't exist in the new subscription. LastGoodGroup is
		// per-profile; re-hydrate it from the freshly activated profile
		// so the UI shows the user's sticky pick (or nothing, on a fresh
		// profile) immediately, before /proxies finishes loading.
		a.state.Proxy.ActiveGroup = ""
		a.state.Proxy.Groups = []ProxyGroup{}
		a.state.Proxy.LastGoodGroup = a.activeProfileLastGoodGroupLocked()
	}
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
		// Deleting or rotating the active profile invalidates the
		// cached /proxies snapshot. Re-hydrate LastGoodGroup from
		// whatever profile is active now (or blank if we deleted the
		// last one).
		a.state.Proxy.ActiveGroup = ""
		a.state.Proxy.Groups = []ProxyGroup{}
		a.state.Proxy.LastGoodGroup = a.activeProfileLastGoodGroupLocked()
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

// activeProfileIndexLocked returns the index of the currently active
// profile inside a.profiles, or -1 if no profile is active / found.
// Caller must hold a.mu.
func (a *App) activeProfileIndexLocked() int {
	id := strings.TrimSpace(a.state.Profile.ActiveProfileID)
	if id == "" {
		return -1
	}
	for i := range a.profiles {
		if a.profiles[i].ID == id {
			return i
		}
	}
	return -1
}

// activeProfileLastGoodGroupLocked returns the per-profile sticky pick.
// Caller must hold a.mu.
func (a *App) activeProfileLastGoodGroupLocked() string {
	idx := a.activeProfileIndexLocked()
	if idx < 0 {
		return ""
	}
	return strings.TrimSpace(a.profiles[idx].LastGoodGroup)
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
	// Persist the user's explicit pick on the active profile so it
	// survives reconnects AND app restarts. That's the "today MainGroup,
	// tomorrow still MainGroup" memory the user asked for. Auto-picked
	// fallbacks below (anchor / first-safe) never touch this field, so
	// the stored value always reflects genuine user intent.
	if idx := a.activeProfileIndexLocked(); idx >= 0 {
		if strings.TrimSpace(a.profiles[idx].LastGoodGroup) != name {
			a.profiles[idx].LastGoodGroup = name
			a.state.Profile.Profiles = a.profiles
			if err := a.persistProfilesLocked(); err != nil {
				debugLog(
					"select-proxy-group",
					"H5",
					"app.go:SelectProxyGroup",
					"failed to persist sticky pick",
					map[string]any{"error": err.Error(), "group": name},
				)
			}
		}
	}
	a.state.UpdatedAt = time.Now().Unix()
	return a.state, nil
}

// restoreStickyGroupLocked is the full auto-pick story after we removed the
// old multi-tier picker (anchor / first-safe heuristics were destabilising
// reconnects and causing the "залипания" the user reported). The logic is
// deliberately minimal:
//
//   - If the active profile has a persisted LastGoodGroup (the user's last
//     manual SelectProxyGroup click for this profile) AND that group is
//     still present in the current /proxies snapshot, copy it into
//     ActiveGroup so the Proxies screen keeps highlighting it across
//     reconnects, app restarts, traffic-mode flips and mode changes.
//   - Otherwise, leave ActiveGroup untouched. First-ever connects land on
//     an empty ActiveGroup (UI shows "—") until the user clicks a group
//     on the Proxies screen — that click persists onto the profile and
//     this routine picks it up on every subsequent connect.
//
// Built-in policy tokens (GLOBAL / DIRECT / REJECT / PASS) are filtered
// so we never land Rule mode on a non-pickable outbound even if a stale
// sticky value somehow names one.
//
// This is NOT an automatic group picker. There is no fallback to any
// "first safe group" or to the terminal MATCH target. That was the
// piece that kept surprising the user; removing it is the whole point.
// Caller must hold a.mu.
func (a *App) restoreStickyGroupLocked() {
	sticky := a.activeProfileLastGoodGroupLocked()
	if sticky == "" {
		return
	}
	switch strings.ToUpper(sticky) {
	case "GLOBAL", "DIRECT", "REJECT", "REJECT-DROP", "PASS":
		return
	}
	stickyLower := strings.ToLower(sticky)
	for _, g := range a.state.Proxy.Groups {
		if strings.EqualFold(strings.ToLower(strings.TrimSpace(g.Name)), stickyLower) {
			a.state.Proxy.ActiveGroup = g.Name
			a.state.UpdatedAt = time.Now().Unix()
			debugLog(
				"sticky-restore",
				"H5",
				"app.go:restoreStickyGroupLocked",
				"restored sticky pick",
				map[string]any{
					"group":       g.Name,
					"groupsCount": len(a.state.Proxy.Groups),
				},
			)
			return
		}
	}
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

// OnWindowBecameVisible re-syncs service status and Windows HKCU proxy when the user
// returns to the app (clash-verge-rev refetches system-proxy state on focus/reconnect).
func (a *App) OnWindowBecameVisible() {
	a.refreshServiceStatus()
	a.mu.RLock()
	connected := a.state.Connection.Status == "connected"
	a.mu.RUnlock()
	if connected {
		go func() { _, _ = a.RefreshHomeInsight() }()
	}
	if runtime.GOOS == "windows" {
		a.maybeWindowsSysProxyReconcile()
	}
	a.emitAppStateChanged()
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
	if attr := hideWindowSysProcAttr(); attr != nil {
		cmd.SysProcAttr = attr
	}
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
