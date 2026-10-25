package main

import (
	"context"
	"embed"
	"os"
	"os/exec"
	"runtime"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"SlothClashDesktop/companion"

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
	// reconnectFlushConns: after the next reconnect's hot-reload succeeds, close
	// all live connections so a routing change (rule toggle / rules edit) applies
	// immediately instead of lingering on already-established keep-alive sockets.
	// Set by rule changes only; a plain config reload (subscription refresh, TUN
	// tweak) leaves connections alone.
	reconnectFlushConns atomic.Bool
	// bootDone is closed once the startup background boot (orphan-reconcile +
	// warm cold-start of the active profile) has finished. The first Connect
	// waits on it so it never races a half-started boot core or the boot's own
	// late config sync — the source of the "green but not actually connected on
	// first launch, reconnect fixes it" bug. nil until startup wires it.
	bootDone      chan struct{}
	closeToTray   bool
	quitRequested bool

	emitStateMu           sync.Mutex
	emitStateTimer        *time.Timer
	insightRefreshRunning atomic.Bool

	desyncMu   sync.Mutex
	desyncAuto desyncAutoRuntime
	diagMu     sync.Mutex
	diagEvents []RuntimeDiagEvent

	// LAN companion controller (control ClashFest on a TV, etc.). Lazily created.
	companionMu          sync.Mutex
	companionMgr         *companion.Manager
	companionWatchCancel context.CancelFunc
	// companionDeviceWatch cancels the event stream of each device page that is
	// open (CompanionWatchDevice), keyed by deviceId.
	companionDeviceWatch map[string]context.CancelFunc
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
	// Close-to-tray must be right before the frontend has had a chance to push
	// it (the first window close can happen while the webview is still loading).
	a.mu.Lock()
	a.closeToTray = currentDesktopPrefs().UI.IsCloseToTray()
	a.mu.Unlock()
	a.loadProfilesFromDisk()
	// The generation pipeline reports its own subscription body fetches here
	// (profile update status, profile_update_status.go).
	bodyFetchRecorder.Store(a)
	// Reclaim runtime dirs of profiles deleted in earlier sessions (audit R3).
	a.pruneOrphanRuntimeDirs()
	a.refreshServiceStatus()
	if trayRuntimeEnabled() {
		startAppTray(a)
	}
	go a.runGuardedLoop(ctx, "profile-autoupdate", a.startProfileAutoUpdateLoop)
	go a.runGuardedLoop(ctx, "update-check", a.updateCheckLoop)
	go a.runGuardedLoop(ctx, "supervisor", a.runRuntimeSupervisorLoop)
	go a.runGuardedLoop(ctx, "desync-auto", a.runDesyncAutoLoop)
	a.emitAppStateChanged()
	// Warm the core in the background for the active profile (cold start with
	// tun.enable: false) so the first Connect only has to push a single
	// PUT /configs?force=true reload with the desired TUN state instead of
	// paying the 2-5s cold-start cost of spawning Mihomo. Failures here are
	// non-fatal — Connect will retry the full ensureCoreForProfile path.
	// Clear an orphan service-core from a crashed prior run BEFORE booting a new
	// one, otherwise the survivor still holds the wintun adapter (audit L1).
	// bootDone lets the first Connect wait for this background boot to settle
	// (see field doc). Created here, on the single-threaded startup path, before
	// the webview can trigger a Connect.
	a.bootDone = make(chan struct{})
	// Claim the boot's connect generation NOW, synchronously, before the webview
	// can fire a Connect. If we claimed it later (inside the goroutine, AFTER the
	// IPC-heavy orphan reconcile), a Connect fired in that window would get a LOWER
	// generation and be aborted the instant the boot bumped the gen — the
	// "connect.aborted skip" cold-boot hang (auto-connect made it reproducible, but
	// a fast manual Connect hit it too). Claiming first means any Connect (auto or
	// manual) supersedes the boot cleanly, never the reverse.
	bootGen := a.connectGen.Add(1)
	go func() {
		defer close(a.bootDone)
		a.reconcileOrphanServiceCoreOnStartup()
		a.bootActiveProfileCoreInBackground(bootGen)
	}()
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
func (a *App) bootActiveProfileCoreInBackground(bootGen uint64) {
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
	// bootGen was claimed synchronously in OnStartup (before the webview could fire
	// a Connect) and passed in, so a concurrent Connect — which bumps connectGen to
	// a HIGHER value — cleanly supersedes us: we then skip our late config sync
	// instead of applying it on top of Connect's work, and we never abort the
	// Connect by bumping the gen out from under it.
	// Boot cores with TUN disabled — the user has not clicked Connect yet.
	// Connect() will bring TUN up via applyRuntimeConfig+PUT /configs force-reload
	// (matches clash-verge-rev's init path: start_core with current verge state,
	// then toggle_tun_mode goes through update_config → reload_config).
	coldStarted, err := a.ensureCoreForProfileEx(profile, bootGen, false)
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
		// A stale core pin (core bumped by the upgrade, service still pinned to
		// the old hash) shows up here first, before the user ever clicks
		// Connect. Flag it now so the reinstall banner is already up when the
		// window opens instead of after a failed connect.
		if isCorePinMismatchError(err) {
			a.mu.Lock()
			a.state.Service.CorePinMismatch = true
			a.state.UpdatedAt = time.Now().Unix()
			a.mu.Unlock()
			a.emitAppStateChanged()
		}
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
	// A Connect fired while we were booting and now owns the core — don't run our
	// late sync on top of its work (it would reapply enableTun=false and race the
	// connect's own reload, the "green but not connected" bug).
	if a.connectGen.Load() != bootGen {
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

// IsBootSettled reports whether the background startup boot has finished
// (reconcileOrphanServiceCoreOnStartup + bootActiveProfileCoreInBackground, which
// claims a connect generation). Auto-connect-on-startup MUST wait for this before
// firing: the boot claims its connectGen AFTER an IPC-heavy orphan reconcile, so a
// connect fired earlier gets a LOWER generation and is aborted the moment the boot
// bumps the gen ("connect.aborted skip" → status stuck at "connecting"). Gating the
// auto-connect on this keeps the boot→connect ordering the design assumes. Returns
// true when bootDone is closed (or was never set, e.g. no active profile).
func (a *App) IsBootSettled() bool {
	bd := a.bootDone
	if bd == nil {
		return true
	}
	select {
	case <-bd:
		return true
	default:
		return false
	}
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

// Connect / Disconnect / connectAfterCoreStarts and helpers live in app_connect.go.

// OnWindowBecameVisible re-syncs service status and Windows HKCU proxy when the user
// returns to the app (clash-verge-rev refetches system-proxy state on focus/reconnect).
func (a *App) OnWindowBecameVisible() {
	a.refreshServiceStatus()
	a.mu.RLock()
	connected := a.state.Connection.Status == "connected"
	a.mu.RUnlock()
	if connected {
		a.safeGo("insight", func() { _, _ = a.RefreshHomeInsight() })
	}
	if runtime.GOOS == "windows" {
		a.maybeWindowsSysProxyReconcile()
	}
	a.emitAppStateChanged()
}

func truncateString(s string, maxRunes int) string {
	r := []rune(s)
	if len(r) <= maxRunes {
		return s
	}
	return string(r[:maxRunes]) + "\n…(truncated)"
}
