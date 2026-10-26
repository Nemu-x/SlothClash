import { lazy, Suspense, useCallback, useEffect, useRef, useState } from 'react'
import { useTranslation } from 'react-i18next'

import './App.css'
// Same asset Wails uses from build/appicon.png (single source of truth for the window chrome).
import {
  Connect,
  Disconnect,
  EnsureTunReady,
  GetTunStatus,
  SetMode,
  SetTrafficMode,
} from './api/core'
import { GetCorpVpnStatus } from './api/corp'
import {
  GetRuntimeDiagEvents,
  OpenPathInExplorer,
  ReadServiceLatestLog,
  ReExtractBundledResources,
  ResetSubscriptionCache,
  RestartCore,
} from './api/diagnostics'
import { main } from './api/models'
import {
  GetPreferredLanguage,
  OnWindowBecameVisible,
  SetUISettings,
  SetUiLanguage,
} from './api/prefs'
import {
  ActivateProfile,
  DeleteProfile,
  GetProfilePaths,
  RefreshProfileSubscription,
  SetProfileAutoUpdate,
} from './api/profile'
import { RefreshProxies, SelectProxyGroup, SetProxyNode } from './api/proxy'
import { BrowserOpenURL } from './api/runtime'
import {
  GetServiceInfo,
  InstallService,
  IsBootSettled,
  RefreshSlothServiceStatus,
} from './api/service'
import { GetAppState, RefreshHomeInsight } from './api/state'
import { GetSubscriptionDeviceIdentity } from './api/subscription'
import { ApplyUpdate } from './api/update'
import { ErrorBoundary } from './components/ErrorBoundary'
import { OperatorModal } from './components/OperatorModal'
import { ProfileModalsHost } from './components/ProfileModalsHost'
import { SettingsResetModal } from './components/SettingsResetModal'
import { SidebarNav } from './components/SidebarNav'
import { ToastHub } from './components/ToastHub'
import { TunSettingsModal } from './components/TunSettingsModal'
import {
  LS_ACCENT,
  LS_NAV_COLLAPSED,
  LS_SETTINGS,
  LS_SPOTLIGHT,
  LS_THEME,
} from './constants'
import { useAdvancedInfo } from './hooks/queries/useAdvancedInfo'
import { useBranding } from './hooks/queries/useBranding'
import { useDesyncStatus } from './hooks/queries/useDesyncStatus'
import { useRuntimeDiag } from './hooks/queries/useRuntimeDiag'
import { useServiceLog } from './hooks/queries/useServiceLog'
import { useUpdateState } from './hooks/queries/useUpdateState'
import { useBackendEvents } from './hooks/useBackendEvents'
import { useConnectionsView } from './hooks/useConnectionsView'
import { useConnectivityChecks } from './hooks/useConnectivityChecks'
import { useDesktopPrefs } from './hooks/useDesktopPrefs'
import { useHomeViewModel } from './hooks/useHomeViewModel'
import { useProfileModals } from './hooks/useProfileModals'
import { useProxyDelay } from './hooks/useProxyDelay'
import { useRulesView } from './hooks/useRulesView'
import { useToasts } from './hooks/useToasts'
import i18n, { LS_LANG, readStoredLang } from './i18n'
import { HomePage } from './pages/Home'
import { ProfilesPage } from './pages/Profiles'
import { ProxiesPage } from './pages/Proxies'
// Heavy / rarely opened screens load lazily so the initial bundle stays
// focused on Home + Proxies + Profiles (the path 90% of users follow).
const AdvancedPage = lazy(() =>
  import('./pages/Advanced').then((m) => ({ default: m.AdvancedPage })),
)
const ConnectionsPage = lazy(() =>
  import('./pages/Connections').then((m) => ({ default: m.ConnectionsPage })),
)
const DevicesPage = lazy(() =>
  import('./pages/Devices').then((m) => ({ default: m.DevicesPage })),
)
const CorpVpnPage = lazy(() =>
  import('./pages/CorpVpn').then((m) => ({ default: m.CorpVpnPage })),
)
const LogsPage = lazy(() =>
  import('./pages/Logs').then((m) => ({ default: m.LogsPage })),
)
const RulesPage = lazy(() =>
  import('./pages/Rules').then((m) => ({ default: m.RulesPage })),
)
const SettingsPage = lazy(() =>
  import('./pages/Settings').then((m) => ({ default: m.SettingsPage })),
)
import { SpotlightTour } from './SpotlightTour'
import { SPOTLIGHT_TOUR_STEP_COUNT } from './spotlightTourConfig'
import type { ImportModalReason, Screen } from './types/app'
import { deriveAccentVars, normalizeHex, resolveAccent } from './utils/accent'
import { DEFAULT_SETTINGS } from './utils/settings'

function App() {
  const { t } = useTranslation()
  const shellRef = useRef<HTMLDivElement>(null)
  const [screen, setScreen] = useState<Screen>('home')
  // Server-state queries. Each one drives a single screen's view, polls itself
  // while that screen is open, and exposes a `refresh()` for manual triggers
  // (rule-providers bulk update, the "Refresh" buttons in headers). The Rules
  // query lives in useRulesView below. Connections: snapshot + search, held
  // here (not in ConnectionsPage) so the search survives leaving the screen.
  const connectionsView = useConnectionsView(screen === 'connections')
  const { log: serviceLog, refresh: refreshRuntimeLog } = useServiceLog(
    screen === 'logs',
  )
  const { events: runtimeDiagEvents } = useRuntimeDiag(screen === 'advanced')
  const {
    paths: advancedPaths,
    geo: advancedGeo,
    refresh: refreshAdvancedInfo,
  } = useAdvancedInfo(screen === 'advanced')
  const [toolsBusy, setToolsBusy] = useState<string | null>(null)
  const { toasts, push: pushToast, dismiss: dismissToast } = useToasts()
  const {
    snap: updateSnap,
    runCheck: runUpdateCheck,
    invalidate: invalidateUpdateState,
  } = useUpdateState()
  const [state, setState] = useState<any>(null)
  const [service, setService] = useState<any>(null)
  const [error, setError] = useState('')
  const [linkToast, setLinkToast] = useState('')
  const [tunBanner, setTunBanner] = useState('')
  // Status line, not a permanent notice: it used to stay on screen forever with
  // no way to close it (and a stale "Service installed …" text made the app look
  // like it had a pending problem). Auto-retire it; Settings also renders a
  // dismiss button.
  useEffect(() => {
    if (!tunBanner.trim()) return
    const id = window.setTimeout(() => setTunBanner(''), 12_000)
    return () => window.clearTimeout(id)
  }, [tunBanner])
  const [profilePaths, setProfilePaths] = useState<main.ProfilePaths | null>(
    null,
  )
  const {
    busy: connectivityBusy,
    results: connectivityResults,
    check: runConnectivityCheck,
  } = useConnectivityChecks()
  const [deviceIdentity, setDeviceIdentity] =
    useState<main.SubscriptionDeviceIdentityPublic | null>(null)
  // Which profile dialog is open; their drafts live in ProfileModalsHost.
  const profileModals = useProfileModals()
  const isAnyEditorModalOpen = profileModals.isAnyEditorModalOpen
  const [serviceInfo, setServiceInfo] =
    useState<main.ServiceRuntimeInfo | null>(null)
  // Session-dismiss for the app-wide "update the helper service" banner. It
  // reappears next launch while the service is still outdated.
  const [serviceBannerDismissed, setServiceBannerDismissed] = useState(false)
  const [showBuiltinProxyGroups, setShowBuiltinProxyGroups] = useState(false)
  const [homeActiveNodeOpen, setHomeActiveNodeOpen] = useState(false)
  const homeActiveNodeRef = useRef<HTMLDivElement | null>(null)
  // Live corp-VPN connection state, for the nav dot + Home indicator.
  const [corpConnected, setCorpConnected] = useState<boolean>(false)
  const [updateProgress, setUpdateProgress] = useState<{
    downloaded: number
    total: number
    pct: number
  } | null>(null)
  const [showTunModal, setShowTunModal] = useState(false)
  const [settingsResetModal, setSettingsResetModal] = useState<
    'keep_profiles' | 'with_profiles' | null
  >(null)
  const [profileRefreshBusyId, setProfileRefreshBusyId] = useState<
    string | null
  >(null)
  const [theme, setTheme] = useState<'dark' | 'light' | 'system'>(() => {
    const v = localStorage.getItem(LS_THEME) as
      | 'dark'
      | 'light'
      | 'system'
      | null
    if (v === 'light' || v === 'dark' || v === 'system') return v
    return 'dark'
  })
  // Raw user-picked hex; per-theme contrast adjustment happens at apply time.
  const [customAccent, setCustomAccent] = useState<string | null>(() =>
    normalizeHex(localStorage.getItem(LS_ACCENT)),
  )
  const [lang, setLang] = useState<'en' | 'ru' | 'zh'>(() => readStoredLang())
  const [spotlightOpen, setSpotlightOpen] = useState(
    () => localStorage.getItem(LS_SPOTLIGHT) !== '1',
  )
  const [spotlightStep, setSpotlightStep] = useState(0)
  const [connectBusy, setConnectBusy] = useState(false)
  const [optimisticMode, setOptimisticMode] = useState<string | null>(null)
  const [optimisticTraffic, setOptimisticTraffic] = useState<string | null>(
    null,
  )
  const [navCollapsed, setNavCollapsed] = useState(
    () => localStorage.getItem(LS_NAV_COLLAPSED) === '1',
  )
  const {
    busy: proxyDelayBusy,
    delays: proxyDelayMap,
    errors: proxyDelayErr,
    pingAll: runProxyDelayTestAll,
  } = useProxyDelay()
  const branding = useBranding()
  const brandManifest = branding?.manifest ?? null
  const [operatorModalOpen, setOperatorModalOpen] = useState(false)
  const refreshInFlightRef = useRef<Promise<void> | null>(null)
  const refreshQueuedRef = useRef(false)
  const connectBusySinceRef = useRef<number | null>(null)
  // sawConnectingRef gates the connectBusy auto-clear effect so it can only
  // fire on a *transition* into a terminal state, not on the stale
  // "disconnected" snapshot the local React state still holds in the same
  // render where the user just pressed Connect. Without this, the connect
  // button briefly flips back to "Connect" between "Connecting" and
  // "Connected" because the auto-clear's 360 ms timer fires against the
  // initial-disconnected state before the backend's "connecting" emit lands.
  const sawConnectingRef = useRef(false)
  const clearConnectBusySmooth = useCallback(() => {
    const since = connectBusySinceRef.current
    const minMs = 360
    if (since === null) {
      setConnectBusy(false)
      return
    }
    const elapsed = performance.now() - since
    if (elapsed >= minMs) {
      setConnectBusy(false)
      return
    }
    window.setTimeout(() => setConnectBusy(false), Math.max(0, minMs - elapsed))
  }, [])

  const refresh = useCallback(async () => {
    if (refreshInFlightRef.current) {
      refreshQueuedRef.current = true
      return refreshInFlightRef.current
    }
    const run = async () => {
      do {
        refreshQueuedRef.current = false
        const current = await GetAppState()
        setState(current)
        // Do not block visual state updates on Windows service polling (`sc query`).
        void GetTunStatus()
          .then(setService)
          .catch(() => {})
      } while (refreshQueuedRef.current)
    }
    const p = run().finally(() => {
      refreshInFlightRef.current = null
    })
    refreshInFlightRef.current = p
    return p
  }, [])

  useEffect(() => {
    refresh()
  }, [refresh])

  // Upstream-style: re-check service + Windows HKCU proxy when returning to the window.
  useEffect(() => {
    let timer: number | null = null
    const schedule = () => {
      if (document.visibilityState !== 'visible') return
      if (timer !== null) window.clearTimeout(timer)
      timer = window.setTimeout(() => {
        timer = null
        void (async () => {
          try {
            await OnWindowBecameVisible()
          } catch {
            /* ignore */
          }
          await refresh()
        })()
      }, 260)
    }
    document.addEventListener('visibilitychange', schedule)
    window.addEventListener('focus', schedule)
    return () => {
      document.removeEventListener('visibilitychange', schedule)
      window.removeEventListener('focus', schedule)
      if (timer !== null) window.clearTimeout(timer)
    }
  }, [refresh])

  // Prefs: localStorage compact settings + the backend-owned prefs.json
  // sections, loaded once on mount.
  const prefs = useDesktopPrefs({
    setError,
    pushToast,
    onCorpVpnDisabled: () => setScreen((s) => (s === 'corp' ? 'home' : s)),
  })
  const { settings, setSettings, settingsBusy, setSettingsBusy } = prefs
  const corpVpnEnabled = prefs.corpVpnEnabled

  // Poll corp-VPN status (only when the feature is on) to drive the nav dot and
  // the Home indicator. Cheap: one call every few seconds, and nothing at all
  // for the 95% of users who never enable it.
  useEffect(() => {
    if (!corpVpnEnabled) {
      setCorpConnected(false)
      return
    }
    let alive = true
    const tick = () => {
      GetCorpVpnStatus()
        .then((s) => {
          if (alive) setCorpConnected(!!s?.connected)
        })
        .catch(() => {})
    }
    tick()
    const id = window.setInterval(tick, 5000)
    return () => {
      alive = false
      window.clearInterval(id)
    }
  }, [corpVpnEnabled])

  useBackendEvents({
    refresh,
    isAnyEditorModalOpen,
    invalidateUpdateState,
    setUpdateProgress,
    setScreen,
    setError,
    pushToast,
  })

  useEffect(() => {
    if (!spotlightOpen) return
    setScreen('home')
  }, [spotlightOpen])

  // Background event surfacing: when Mihomo dies and the auto-restart loop
  // walks its backoffs, the user sees "reconnecting" toasts and a clear
  // "recovered" message when it succeeds. Without this they'd have to spot
  // the status flicker on the Home screen mid-recovery.
  const prevConnStatusRef = useRef<string>('')
  useEffect(() => {
    const cur = String(state?.connection?.status ?? '')
    const prev = prevConnStatusRef.current
    if (cur === prev) return
    prevConnStatusRef.current = cur
    if (cur === 'reconnecting') {
      const detail = String(state?.connection?.lastError ?? '').trim()
      pushToast({
        kind: 'warn',
        message: detail || t('ui.toasts.coreRestarting'),
      })
    } else if (prev === 'reconnecting' && cur === 'connected') {
      pushToast({ kind: 'success', message: t('ui.toasts.connRecovered') })
    } else if (prev === 'reconnecting' && cur === 'error') {
      pushToast({
        kind: 'error',
        message:
          String(state?.connection?.lastError ?? '') ||
          t('ui.toasts.coreNoRecover'),
        actionLabel: t('ui.toasts.retry'),
        onAction: () => {
          void run(() => Connect())
        },
        durationMs: 0,
      })
    } else if (prev === 'connecting' && cur === 'error') {
      // Async connect job failed (most common cause: invalid profile YAML
      // after subscription refresh). Surface a toast so the user sees the
      // issue even if they switched screens after kicking off Connect.
      pushToast({
        kind: 'error',
        message:
          String(state?.connection?.lastError ?? '') ||
          t('ui.toasts.connectFailed'),
        actionLabel: t('ui.toasts.logs'),
        onAction: () => setScreen('advanced'),
        durationMs: 0,
      })
    }
  }, [state?.connection?.status, state?.connection?.lastError, pushToast])

  useEffect(() => {
    if (!connectBusy) return
    const st = state?.connection?.status
    if (st === 'connecting') {
      sawConnectingRef.current = true
      return
    }
    // Auto-clear only AFTER we have witnessed a 'connecting' tick — otherwise
    // the stale 'disconnected' snapshot present in this render cycle (the
    // user just pressed Connect; the backend's `connecting` emit has not yet
    // round-tripped) would immediately trip clearConnectBusySmooth and make
    // the button flicker through "Connect" between "Connecting" and
    // "Connected". 'connected' is allowed through without that gate because
    // an already-connected core (idempotent re-press) is a legitimate
    // terminal state to clear on.
    if (st === 'connected') {
      sawConnectingRef.current = false
      clearConnectBusySmooth()
      return
    }
    if (!sawConnectingRef.current) return
    if (st === 'error' || st === 'disconnected' || st === 'reconnecting') {
      sawConnectingRef.current = false
      clearConnectBusySmooth()
    }
  }, [state?.connection?.status, connectBusy, clearConnectBusySmooth])

  // Safety net: if Connect drags on, poll state explicitly every 2s instead
  // of waiting for the app:state event to fire. We have seen sessions where
  // the IPC service path stalls between "ipc start finished" and the actual
  // status transition; without this poll the user is stuck on "Connecting"
  // forever even though mihomo is fully up and routing traffic.
  useEffect(() => {
    if (!connectBusy) return
    const id = window.setInterval(() => {
      void refresh()
    }, 2_000)
    // Final guardrail: force-clear after 90s no matter what so the user can
    // try again rather than restart the app.
    const giveUp = window.setTimeout(() => {
      pushToast({
        kind: 'warn',
        message: t('ui.toasts.connectTooLong'),
        durationMs: 8_000,
      })
      setConnectBusy(false)
    }, 90_000)
    return () => {
      window.clearInterval(id)
      window.clearTimeout(giveUp)
    }
  }, [connectBusy, refresh, pushToast])

  useEffect(() => {
    if (state?.connection?.status !== 'connected') {
      setOptimisticMode(null)
      setOptimisticTraffic(null)
      return
    }
    // Prevent stale fatal messages from previous failed attempts lingering after recovery.
    setError('')
  }, [state?.connection?.status])

  // Warmup nudge: only fire while /proxies is still returning an empty
  // group list right after Connect. An empty activeGroup is a VALID
  // steady state now — we removed the auto-picker, so on first-ever
  // connects the Proxies screen legitimately shows "—" until the user
  // clicks a group. Polling on empty activeGroup there would spin
  // refresh() for 8.4s every connect and was the main driver of the
  // "подлагивает, по 5 реконнектов" behaviour reported in the logs.
  useEffect(() => {
    if (isAnyEditorModalOpen) return
    if (state?.connection?.status !== 'connected') return
    const hasGroups = (state?.proxy?.groups?.length ?? 0) > 0
    if (hasGroups) return
    let i = 0
    const id = setInterval(() => {
      i++
      void refresh()
      if (i >= 24) clearInterval(id)
    }, 350)
    return () => clearInterval(id)
  }, [
    isAnyEditorModalOpen,
    state?.connection?.status,
    state?.proxy?.groups?.length,
    refresh,
  ])

  useEffect(() => {
    localStorage.setItem(LS_NAV_COLLAPSED, navCollapsed ? '1' : '0')
  }, [navCollapsed])

  useEffect(() => {
    if (isAnyEditorModalOpen) return
    if (screen !== 'proxies') return
    if (state?.connection?.status !== 'connected') return
    let cancelled = false
    void (async () => {
      setError('')
      try {
        const next = await RefreshProxies()
        if (!cancelled && next) {
          setState(next as main.AppState)
        }
      } catch (e: any) {
        if (!cancelled) setError(String(e))
      }
    })()
    return () => {
      cancelled = true
    }
  }, [screen, state?.connection?.status, isAnyEditorModalOpen])

  // Same as Proxies: Home reads proxy state — refresh when opening Home while connected
  // so Active group / node match the core without visiting Proxies first.
  useEffect(() => {
    if (isAnyEditorModalOpen) return
    if (screen !== 'home') return
    if (state?.connection?.status !== 'connected') return
    let cancelled = false
    void (async () => {
      try {
        const next = await RefreshProxies()
        if (!cancelled && next) {
          setState(next as main.AppState)
        }
      } catch {
        /* non-fatal */
      }
    })()
    return () => {
      cancelled = true
    }
  }, [screen, state?.connection?.status, isAnyEditorModalOpen])

  useEffect(() => {
    if (isAnyEditorModalOpen) return
    if (screen !== 'home') return
    if (state?.connection?.status !== 'connected') return
    let cancelled = false
    void (async () => {
      try {
        const next = await RefreshHomeInsight()
        if (!cancelled && next) {
          setState(next as main.AppState)
        }
      } catch {
        /* non-fatal */
      }
    })()
    const id = setInterval(() => {
      void (async () => {
        try {
          const next = await RefreshHomeInsight()
          if (!cancelled && next) {
            setState(next as main.AppState)
          }
        } catch {
          /* */
        }
      })()
    }, 45000)
    return () => {
      cancelled = true
      clearInterval(id)
    }
  }, [
    screen,
    state?.connection?.status,
    state?.mode?.current,
    isAnyEditorModalOpen,
  ])

  useEffect(() => {
    if (screen !== 'settings') return
    invalidateUpdateState()
  }, [screen, invalidateUpdateState])

  useEffect(() => {
    localStorage.setItem(LS_THEME, theme)
    if (customAccent) localStorage.setItem(LS_ACCENT, customAccent)
    else localStorage.removeItem(LS_ACCENT)
    const el = shellRef.current
    if (!el) return
    const apply = () => {
      let effective: 'dark' | 'light'
      if (theme === 'system') {
        const dark = window.matchMedia('(prefers-color-scheme: dark)').matches
        effective = dark ? 'dark' : 'light'
      } else {
        effective = theme
      }
      el.setAttribute('data-theme', effective)
      // Inline custom properties outrank both stylesheet blocks, so every
      // var(--accent*) consumer restyles without per-component changes.
      // Brand accent (panel-provided) sits below the user's own pick.
      const hex = resolveAccent({
        userHex: customAccent,
        brandHex: brandManifest?.accentColor,
      })
      if (hex) {
        const vars = deriveAccentVars(hex, effective)
        el.style.setProperty('--accent', vars.accent)
        el.style.setProperty('--accent-dim', vars.dim)
        el.style.setProperty('--accent-muted', vars.muted)
      } else {
        el.style.removeProperty('--accent')
        el.style.removeProperty('--accent-dim')
        el.style.removeProperty('--accent-muted')
      }
    }
    apply()
    if (theme !== 'system') return
    const mq = window.matchMedia('(prefers-color-scheme: dark)')
    mq.addEventListener('change', apply)
    return () => mq.removeEventListener('change', apply)
  }, [theme, customAccent, brandManifest?.accentColor])

  useEffect(() => {
    let cancelled = false
    void (async () => {
      try {
        const hasStored = Boolean(localStorage.getItem(LS_LANG))
        if (hasStored) return
        const preferred = await GetPreferredLanguage()
        if (cancelled) return
        if (preferred === 'ru' || preferred === 'zh' || preferred === 'en') {
          setLang(preferred)
        }
      } catch {
        // ignore and keep system detector fallback
      }
    })()
    return () => {
      cancelled = true
    }
  }, [])

  useEffect(() => {
    localStorage.setItem(LS_LANG, lang)
    void i18n.changeLanguage(lang)
    // Push the language to the backend so the native tray menu (built from
    // Go before the webview attaches) can localize its labels. Fire-and-
    // forget — the tray rebuilds the popup on every right-click anyway.
    void SetUiLanguage(lang).catch(() => {})
  }, [lang])

  const run = async (action: () => Promise<any>) => {
    setError('')
    try {
      await action()
    } catch (e: any) {
      setError(String(e))
    }
    await refresh()
  }

  const refreshAllSubscriptions = async () => {
    if (settingsBusy) return
    const profiles = state?.profile?.profiles ?? []
    const subs = profiles.filter((p: any) => String(p?.url ?? '').trim())
    if (subs.length === 0) {
      setTunBanner(t('ui.toasts.noSubsToRefresh'))
      return
    }
    setSettingsBusy(true)
    setError('')
    try {
      for (const p of subs) {
        await RefreshProfileSubscription(String(p.id))
      }
      setTunBanner(t('ui.toasts.refreshedSubs', { count: subs.length }))
    } catch (e: any) {
      setError(String(e))
    } finally {
      setSettingsBusy(false)
      await refresh()
    }
  }

  const applyDefaultAutoUpdateToProfiles = async () => {
    if (settingsBusy) return
    const profiles = state?.profile?.profiles ?? []
    if (profiles.length === 0) {
      setTunBanner(t('ui.toasts.noProfilesYet'))
      return
    }
    const interval = Math.max(
      5,
      Number(settings.defaultAutoUpdateMinutes) || 360,
    )
    setSettingsBusy(true)
    setError('')
    try {
      for (const p of profiles) {
        await SetProfileAutoUpdate(
          String(p.id),
          Boolean(p?.autoUpdateEnabled ?? true),
          interval,
        )
      }
      setTunBanner(
        `Applied ${interval} min auto-update interval to all profiles.`,
      )
    } catch (e: any) {
      setError(String(e))
    } finally {
      setSettingsBusy(false)
      await refresh()
    }
  }

  const exportDiagnosticsBundle = async () => {
    if (settingsBusy) return
    setSettingsBusy(true)
    setError('')
    try {
      const log = await ReadServiceLatestLog(180000)
      const runtimeEvents = await GetRuntimeDiagEvents()
      const payload = {
        exportedAt: new Date().toISOString(),
        appVersion:
          (import.meta.env.VITE_APP_VERSION as string | undefined) ?? 'dev',
        coreVersion: String(state?.core?.version ?? ''),
        update: updateSnap,
        settings,
        appState: state,
        serviceLog: log,
        runtimeEvents,
      }
      const blob = new Blob([JSON.stringify(payload, null, 2)], {
        type: 'application/json',
      })
      const url = URL.createObjectURL(blob)
      const a = document.createElement('a')
      const stamp = new Date().toISOString().replaceAll(':', '-')
      a.href = url
      a.download = `sloth-diagnostics-${stamp}.json`
      a.click()
      URL.revokeObjectURL(url)
      setTunBanner(t('ui.toasts.diagExported'))
    } catch (e: any) {
      setError(String(e))
    } finally {
      setSettingsBusy(false)
    }
  }

  const clearTempUiState = () => {
    localStorage.removeItem(LS_SPOTLIGHT)
    setSpotlightStep(0)
    setSpotlightOpen(true)
    setScreen('home')
    setTunBanner(t('ui.toasts.uiCacheCleared'))
  }

  const resetAppSettings = async (withProfiles: boolean) => {
    if (settingsBusy) return
    setSettingsResetModal(null)
    setSettingsBusy(true)
    setError('')
    try {
      setTheme('system')
      setCustomAccent(null)
      setLang('en')
      setSettings(DEFAULT_SETTINGS)
      // Backend-owned toggles reset too, otherwise the next launch would
      // restore them from prefs.json over the defaults shown here.
      try {
        await SetUISettings(
          new main.UISettings({
            startMinimized: DEFAULT_SETTINGS.startMinimized,
            autoConnectOnStartup: DEFAULT_SETTINGS.autoConnectOnStartup,
            closeToTray: DEFAULT_SETTINGS.closeToTray,
          }),
        )
      } catch {
        /* non-fatal: the localStorage reset below still applies */
      }
      localStorage.removeItem(LS_THEME)
      localStorage.removeItem(LS_ACCENT)
      localStorage.removeItem(LS_LANG)
      localStorage.removeItem(LS_SETTINGS)
      localStorage.removeItem(LS_NAV_COLLAPSED)
      localStorage.removeItem(LS_SPOTLIGHT)
      if (withProfiles) {
        const profiles = state?.profile?.profiles ?? []
        for (const p of profiles) {
          await DeleteProfile(String(p.id))
        }
      }
      setTunBanner(
        withProfiles ? t('ui.toasts.resetRemoved') : t('ui.toasts.resetKept'),
      )
    } catch (e: any) {
      setError(String(e))
    } finally {
      setSettingsBusy(false)
      await refresh()
    }
  }

  // Desync summary for Home: same shared poll as the Settings card, active
  // only while Home is shown (the card keeps it alive on Settings).
  const { snapshot: desyncSnapshot } = useDesyncStatus({
    connected: state?.connection?.status === 'connected',
    mode: String(state?.mode?.current ?? ''),
    traffic: String(state?.traffic ?? ''),
    enabled: screen === 'home',
  })

  // Home screen derivations (status label, active node, tooltips …).
  const homeVm = useHomeViewModel({
    state,
    connectBusy,
    optimisticMode,
    optimisticTraffic,
    updateSnap,
    desync: desyncSnapshot,
  })
  const { hasAnyProfile, hasActiveProfile, activeProfile, displayMode } = homeVm

  const handleOpenUpdate = useCallback(async () => {
    const assetURL = String(updateSnap?.assetDownloadUrl ?? '').trim()
    const releaseURL = String(updateSnap?.releaseUrl ?? '').trim()
    const target = assetURL || releaseURL
    if (!target) return
    try {
      await BrowserOpenURL(target)
    } catch (e: any) {
      setError(String(e))
    }
  }, [updateSnap?.assetDownloadUrl, updateSnap?.releaseUrl])

  useEffect(() => {
    if (screen !== 'advanced') return
    const activeID = String(state?.profile?.activeProfileId ?? '').trim()
    if (!activeID) {
      setProfilePaths(null)
      return
    }
    let cancelled = false
    void (async () => {
      try {
        const p = await GetProfilePaths(activeID)
        if (!cancelled) setProfilePaths(p as main.ProfilePaths)
      } catch {
        if (!cancelled) setProfilePaths(null)
      }
    })()
    return () => {
      cancelled = true
    }
  }, [screen, state?.profile?.activeProfileId])

  useEffect(() => {
    if (screen !== 'advanced' && !operatorModalOpen) return
    let cancelled = false
    void GetSubscriptionDeviceIdentity()
      .then((d) => {
        if (!cancelled)
          setDeviceIdentity(d as main.SubscriptionDeviceIdentityPublic)
      })
      .catch(() => {
        if (!cancelled) setDeviceIdentity(null)
      })
    return () => {
      cancelled = true
    }
  }, [screen, operatorModalOpen])

  useEffect(() => {
    if (!homeActiveNodeOpen) return
    const onDown = (e: MouseEvent) => {
      const el = homeActiveNodeRef.current
      if (el && !el.contains(e.target as Node)) setHomeActiveNodeOpen(false)
    }
    const onKey = (e: KeyboardEvent) => {
      if (e.key === 'Escape') setHomeActiveNodeOpen(false)
    }
    document.addEventListener('mousedown', onDown)
    document.addEventListener('keydown', onKey)
    return () => {
      document.removeEventListener('mousedown', onDown)
      document.removeEventListener('keydown', onKey)
    }
  }, [homeActiveNodeOpen])

  useEffect(() => {
    setHomeActiveNodeOpen(false)
  }, [screen, state?.connection?.status])

  // Rules screen: /rules snapshot, provider updates, dashboard toggles and the
  // filters. Held here (not in RulesPage) so filters and in-flight provider
  // updates survive leaving the screen.
  const rulesView = useRulesView({
    enabled: screen === 'rules',
    activeProfileId: String(state?.profile?.activeProfileId ?? '').trim(),
    profiles: state?.profile?.profiles,
    onError: (m) => setError(m),
  })

  const dismissSpotlight = useCallback(() => {
    localStorage.setItem(LS_SPOTLIGHT, '1')
    setSpotlightOpen(false)
  }, [])

  const openImportModal = (reason: ImportModalReason) => {
    profileModals.openImport(reason)
    setError('')
  }

  const ensureTun = async () => {
    setError('')
    const result = await EnsureTunReady()
    setTunBanner(result.message)
    await refresh()
  }

  const refreshServiceInfo = useCallback(async () => {
    try {
      setServiceInfo(await GetServiceInfo())
    } catch {
      /* non-fatal: the update nudge simply won't show */
    }
  }, [])

  useEffect(() => {
    void refreshServiceInfo()
  }, [refreshServiceInfo])

  // The backend flags a stale core pin / unreachable service on the *app
  // state* when a connect fails (finishConnectJobFailed). serviceInfo is only
  // fetched on mount and after an install, so without this hook the flag never
  // reached the banner within the session and the user was left with a raw
  // "HTTP 503 … does not match any pinned hash" and no action (seen on macOS
  // after the 0.9.3 core bump). Re-probe as soon as either flag turns on.
  const livePinMismatch = state?.service?.corePinMismatch === true
  const liveUnreachable = state?.service?.unreachable === true
  useEffect(() => {
    if (livePinMismatch || liveUnreachable) {
      setServiceBannerDismissed(false)
      void refreshServiceInfo()
    }
  }, [livePinMismatch, liveUnreachable, refreshServiceInfo])

  const installService = async () => {
    setError('')
    const result = await InstallService()
    setTunBanner(result.message)
    await refresh()
    await refreshServiceInfo()
  }

  const switchTraffic = async (mode: 'proxy' | 'tun') => {
    setTunBanner('')
    setOptimisticTraffic(mode)
    setError('')
    try {
      await SetTrafficMode(mode)
    } catch (e: any) {
      setError(String(e))
    } finally {
      setOptimisticTraffic(null)
      await refresh()
    }
  }

  const connectAction = async () => {
    if (state?.connection?.status === 'connected') {
      await run(() => Promise.resolve(Disconnect()))
      return
    }
    if (!hasAnyProfile) {
      openImportModal('connect')
      return
    }
    if (!hasActiveProfile) {
      openImportModal('connect')
      setError(t('ui.errors.chooseProfileOrImport'))
      return
    }
    setError('')
    connectBusySinceRef.current = performance.now()
    sawConnectingRef.current = false
    setConnectBusy(true)
    try {
      await Connect()
    } catch (e: any) {
      const msg = String(e)
      setError(msg)
      // Surface as an actionable toast in parallel with the inline banner —
      // banners are easy to miss when the user is mid-scroll on a long page,
      // and the toast gives them Retry / View logs without clicking back.
      pushToast({
        kind: 'error',
        message: msg,
        actionLabel: t('ui.toasts.logs'),
        onAction: () => setScreen('advanced'),
        durationMs: 0,
      })
      clearConnectBusySmooth()
    }
    await refresh()
  }

  // Auto-connect on startup (opt-in), hardened against the cold-boot race.
  //
  // The app is registered in HKCU\Run and launches in PARALLEL with the
  // AutoStart privileged service, so a single naive fire can hit the core /
  // adapter bring-up before the service (or the network) is ready: it fails once
  // and, with a plain one-shot latch, never retries — the user ends up "launched
  // but not connected, adapter never came up". Instead we run a small
  // once-per-session loop that (1) waits (bounded) for the service to be running
  // before the first attempt and (2) retries a few times with backoff, latching
  // only on an actual success. Reuses connectAction (via a ref so retries call
  // the freshest closure) for all the usual guards + error toasts.
  const connectActionRef = useRef(connectAction)
  connectActionRef.current = connectAction
  const canAutoConnect =
    settings.autoConnectOnStartup === true && hasActiveProfile
  useEffect(() => {
    if (!canAutoConnect) return
    let cancelled = false
    const sleep = (ms: number) =>
      new Promise<void>((res) => setTimeout(res, ms))
    const isUpish = (s?: string) =>
      s === 'connected' || s === 'connecting' || s === 'reconnecting'

    void (async () => {
      const MAX_ATTEMPTS = 4
      const SERVICE_WAIT_MS = 20000
      const BACKOFF_MS = 3000
      for (let attempt = 1; attempt <= MAX_ATTEMPTS && !cancelled; attempt++) {
        // Wait (bounded) for the privileged service to be running before firing,
        // so we don't race the boot. RefreshSlothServiceStatus re-queries the
        // service directly — GetAppState only returns cached state, refreshed on
        // a 45s tick, so it would stay stale-false for most of the boot window.
        // A non-service install reports installed=false and proceeds at once.
        const deadline = Date.now() + SERVICE_WAIT_MS
        for (;;) {
          if (cancelled) return
          let svcReady: boolean
          try {
            const svc = await RefreshSlothServiceStatus()
            svcReady = svc?.installed !== true || svc?.running === true
          } catch {
            svcReady = true // can't tell → don't block; the attempt + retries cover it
          }
          if (cancelled) return
          const cur = (await GetAppState())?.connection?.status
          if (isUpish(cur)) return // user or a prior attempt already brought it up
          // Wait for the background startup boot to finish claiming its connect
          // generation. Firing before it does gets our connect aborted the moment
          // the boot bumps the gen ("connect.aborted skip" → status stuck at
          // "connecting", the reported cold-boot hang). See App.IsBootSettled.
          let bootSettled: boolean
          try {
            bootSettled = await IsBootSettled()
          } catch {
            bootSettled = true
          }
          if (cancelled) return
          if (svcReady && bootSettled) break
          if (Date.now() > deadline) break // give up waiting; try anyway
          await sleep(1000)
        }
        if (cancelled) return
        // Re-check right before firing: connectAction would DISCONNECT if we are
        // already connected, so never call it when the tunnel is up.
        if (isUpish((await GetAppState())?.connection?.status)) return
        await connectActionRef.current()
        // Wait for the connect to reach a TERMINAL state before deciding to retry.
        // Never re-fire while it is still "connecting" — a second connect supersedes
        // and aborts the first, which is exactly the stuck-on-connecting failure.
        const settleDeadline = Date.now() + 30000
        for (;;) {
          if (cancelled) return
          const st = (await GetAppState())?.connection?.status
          if (st === 'connected') return // success → done
          if (st === 'error' || st === 'disconnected') break // genuine failure → retry
          if (Date.now() > settleDeadline) return // still settling after 30s → don't spam more connects
          await sleep(700) // connecting/reconnecting → keep waiting, do NOT re-fire
        }
        await sleep(BACKOFF_MS) // failed → back off, then retry
      }
    })()

    return () => {
      cancelled = true
    }
  }, [canAutoConnect])

  // Rules / Connections / Logs polling moved to react-query hooks
  // (useRulesOverview, useConnectionsOverview, useServiceLog) — they fetch on
  // mount and refetch on the same screen-change semantics via their `enabled`.

  return (
    <div
      className={`shell ${navCollapsed ? 'navCollapsed' : ''}`}
      ref={shellRef}
    >
      <SidebarNav
        screen={screen}
        onChange={setScreen}
        collapsed={navCollapsed}
        onToggleCollapse={() => setNavCollapsed((v) => !v)}
        hiddenScreens={[
          ...(brandManifest?.hideAdvanced ? (['advanced'] as const) : []),
          ...(corpVpnEnabled ? [] : (['corp'] as const)),
        ]}
        activeScreens={corpConnected ? ['corp'] : []}
      />

      <section className="content">
        {(serviceInfo?.updateRequired ||
          serviceInfo?.corePinMismatch ||
          serviceInfo?.unreachable ||
          livePinMismatch ||
          liveUnreachable) &&
        !serviceBannerDismissed ? (
          <div className="serviceUpdateBar" role="alert">
            <span className="serviceUpdateBarIcon" aria-hidden>
              ⚠️
            </span>
            <span className="serviceUpdateBarText">
              {serviceInfo?.updateRequired
                ? t('settings.serviceUpdateTitle')
                : serviceInfo?.corePinMismatch || livePinMismatch
                  ? t('settings.serviceRepinTitle')
                  : t('settings.serviceUnreachableTitle')}{' '}
              — {t('settings.serviceUpdateAdminHint')}
            </span>
            <button
              type="button"
              className="btn primary serviceUpdateBarBtn"
              onClick={() => {
                setScreen('settings')
                void installService()
              }}
            >
              {t('settings.serviceUpdateAction')}
            </button>
            <button
              type="button"
              className="serviceUpdateBarClose"
              aria-label={t('common.dismiss')}
              title={t('common.dismiss')}
              onClick={() => setServiceBannerDismissed(true)}
            >
              ✕
            </button>
          </div>
        ) : null}
        {/* Per-screen error boundary: a render crash in one page shows a
            recoverable fallback here instead of blanking the whole app; the
            sidebar stays alive. Keyed by screen so navigating resets it. */}
        <ErrorBoundary key={screen}>
          {screen === 'home' ? (
            <HomePage
              state={state}
              vm={homeVm}
              hideGlobalMode={brandManifest?.hideGlobalMode}
              hideProxyMode={brandManifest?.hideProxyMode}
              brandName={brandManifest?.name}
              brandLogo={branding?.logoDataUri || branding?.logoLightDataUri}
              onOpenOperator={() => setOperatorModalOpen(true)}
              service={service}
              linkToast={linkToast}
              error={error}
              updateSnap={updateSnap}
              connectBusy={connectBusy}
              homeActiveNodeOpen={homeActiveNodeOpen}
              homeActiveNodeRef={homeActiveNodeRef}
              showBuiltinProxyGroups={showBuiltinProxyGroups}
              onOpenImport={(reason) => openImportModal(reason)}
              onOpenSupport={(url) => void BrowserOpenURL(url)}
              onOpenUpdate={() => void handleOpenUpdate()}
              onSetMode={(m) => {
                setOptimisticMode(m)
                setError('')
                void (async () => {
                  try {
                    await SetMode(m)
                  } catch (e: any) {
                    setError(String(e))
                  } finally {
                    setOptimisticMode(null)
                    await refresh()
                  }
                })()
              }}
              onSwitchTraffic={(m) => switchTraffic(m)}
              corpConnected={corpVpnEnabled && corpConnected}
              onOpenCorp={() => setScreen('corp')}
              onOpenDesync={() => setScreen('settings')}
              onConnectClick={connectAction}
              onInstallService={() => void installService()}
              onRefreshService={() =>
                void (async () => {
                  try {
                    const s = await RefreshSlothServiceStatus()
                    setService(s as main.ServiceState)
                    await refresh()
                  } catch (e: any) {
                    setError(String(e))
                  }
                })()
              }
              onSelectGroup={(name) => void run(() => SelectProxyGroup(name))}
              onSelectNode={(group, node) =>
                void run(() => SetProxyNode(group, node))
              }
              onToggleActiveNodeOpen={() => setHomeActiveNodeOpen((o) => !o)}
            />
          ) : null}
          {screen === 'proxies' ? (
            <ProxiesPage
              groups={(state?.proxy?.groups ?? []) as any[]}
              activeGroup={state?.proxy?.activeGroup ?? ''}
              connectionStatus={state?.connection?.status ?? ''}
              displayMode={displayMode}
              showBuiltin={showBuiltinProxyGroups}
              proxyDelayBusy={proxyDelayBusy}
              proxyDelayMap={proxyDelayMap}
              proxyDelayErr={proxyDelayErr}
              error={error}
              onRefreshProxies={() => run(() => RefreshProxies())}
              onToggleShowBuiltin={() =>
                setShowBuiltinProxyGroups((prev) => !prev)
              }
              onSetMode={(mode) => run(() => SetMode(mode))}
              onSelectGroup={(name) => void run(() => SelectProxyGroup(name))}
              onSelectNode={(group, node) =>
                void run(() => SetProxyNode(group, node))
              }
              onPingAll={(group, nodes) =>
                void runProxyDelayTestAll(group, nodes)
              }
            />
          ) : null}

          {screen === 'profiles' ? (
            <ProfilesPage
              profiles={state?.profile?.profiles ?? []}
              activeProfileId={state?.profile?.activeProfileId ?? ''}
              refreshBusyId={profileRefreshBusyId}
              error={error}
              onImport={() => openImportModal('manual')}
              onActivate={(id) => void run(() => ActivateProfile(id))}
              onRefresh={(id) => {
                setProfileRefreshBusyId(id)
                void (async () => {
                  try {
                    await run(() => RefreshProfileSubscription(id))
                  } finally {
                    setProfileRefreshBusyId((cur) => (cur === id ? null : cur))
                  }
                })()
              }}
              onContextMenu={(target) => profileModals.setMenu(target)}
            />
          ) : null}

          {screen === 'connections' ? (
            <Suspense fallback={<div className="panel" />}>
              <ConnectionsPage
                view={connectionsView}
                onCloseAll={() => run(connectionsView.closeAll)}
              />
            </Suspense>
          ) : null}

          {screen === 'logs' ? (
            <Suspense fallback={<div className="panel" />}>
              <LogsPage
                serviceLog={serviceLog}
                onRefresh={() => void refreshRuntimeLog()}
              />
            </Suspense>
          ) : null}

          {screen === 'devices' ? (
            <Suspense fallback={<div className="panel" />}>
              <DevicesPage />
            </Suspense>
          ) : null}

          {screen === 'corp' && corpVpnEnabled ? (
            <Suspense fallback={<div className="panel" />}>
              <CorpVpnPage />
            </Suspense>
          ) : null}

          {screen === 'rules' ? (
            <Suspense fallback={<div className="panel" />}>
              <RulesPage
                view={rulesView}
                connectionStatus={state?.connection?.status ?? ''}
                error={error}
              />
            </Suspense>
          ) : null}

          {screen === 'advanced' ? (
            <Suspense fallback={<div className="panel" />}>
              <AdvancedPage
                connectionStatus={state?.connection?.status ?? ''}
                coreVersion={String(state?.core?.version ?? '')}
                controllerAddr={String(state?.core?.controllerAddr ?? '')}
                mixedPort={state?.core?.mixedPort ?? ''}
                profilePaths={profilePaths}
                deviceIdentity={deviceIdentity}
                connectivityBusy={connectivityBusy}
                connectivityResults={connectivityResults}
                error={error}
                onConnectivityCheck={(target, url) =>
                  runConnectivityCheck(target, url)
                }
                onCopyHwid={() => {
                  const h = deviceIdentity?.hwid
                  if (!h) return
                  void navigator.clipboard.writeText(h).then(
                    () => {
                      setLinkToast(t('ui.advanced.identityCopied'))
                      window.setTimeout(() => setLinkToast(''), 2500)
                    },
                    () => setError('Clipboard unavailable'),
                  )
                }}
                onCopyAllIdentity={() => {
                  const d = deviceIdentity
                  if (!d) return
                  const text = [
                    `x-hwid: ${d.hwid}`,
                    `x-device-os: ${d.deviceOs}`,
                    `x-ver-os: ${d.osVersion}`,
                    `x-device-model: ${d.deviceModel}`,
                    `x-app-version: ${d.appVersion}`,
                  ].join('\n')
                  void navigator.clipboard.writeText(text).then(
                    () => {
                      setLinkToast(t('ui.advanced.identityCopiedAll'))
                      window.setTimeout(() => setLinkToast(''), 2500)
                    },
                    () => setError('Clipboard unavailable'),
                  )
                }}
                onRefreshProxies={() => run(() => RefreshProxies())}
                onRefreshHomeInsight={() => run(() => RefreshHomeInsight())}
                runtimeDiagEvents={runtimeDiagEvents}
                advancedPaths={advancedPaths}
                advancedGeo={advancedGeo}
                toolsBusy={toolsBusy}
                onOpenPath={(p) => {
                  if (!p) return
                  void OpenPathInExplorer(p).catch((e) =>
                    pushToast({ kind: 'error', message: String(e) }),
                  )
                }}
                onRestartCore={() => {
                  if (toolsBusy) return
                  setToolsBusy('restartCore')
                  void RestartCore()
                    .then(() => {
                      pushToast({
                        kind: 'success',
                        message: t('ui.advanced.restartCore'),
                      })
                      refreshAdvancedInfo()
                    })
                    .catch((e) =>
                      pushToast({ kind: 'error', message: String(e) }),
                    )
                    .finally(() => setToolsBusy(null))
                }}
                onResetSubscriptionCache={() => {
                  if (toolsBusy) return
                  setToolsBusy('resetSubCache')
                  void ResetSubscriptionCache()
                    .then(() => {
                      pushToast({
                        kind: 'success',
                        message: t('ui.advanced.resetSubCache'),
                      })
                    })
                    .catch((e) =>
                      pushToast({ kind: 'error', message: String(e) }),
                    )
                    .finally(() => setToolsBusy(null))
                }}
                onReextractBundled={() => {
                  if (toolsBusy) return
                  setToolsBusy('reextract')
                  void ReExtractBundledResources()
                    .then(() => {
                      pushToast({
                        kind: 'success',
                        message: t('ui.advanced.reextractBundled'),
                      })
                      refreshAdvancedInfo()
                    })
                    .catch((e) =>
                      pushToast({ kind: 'error', message: String(e) }),
                    )
                    .finally(() => setToolsBusy(null))
                }}
                onCopyRuntimeTrace={(text) => {
                  if (!text) return
                  void navigator.clipboard.writeText(text).then(
                    () => {
                      setLinkToast(t('ui.advanced.runtimeTraceCopied'))
                      window.setTimeout(() => setLinkToast(''), 2500)
                    },
                    () => setError('Clipboard unavailable'),
                  )
                }}
                hwidEnabled={prefs.hwidEnabled}
                hwidSaving={prefs.hwidSaving}
                onToggleHwid={prefs.toggleHwid}
              />
            </Suspense>
          ) : null}

          {screen === 'settings' ? (
            <Suspense fallback={<div className="panel" />}>
              <SettingsPage
                theme={theme}
                accent={customAccent}
                onSetAccent={setCustomAccent}
                lang={lang}
                prefs={prefs}
                runningMixedPort={Number(state?.core?.mixedPort) || 0}
                tunBanner={tunBanner}
                onDismissBanner={() => setTunBanner('')}
                state={state}
                updateSnap={updateSnap}
                error={error}
                onBrowserOpen={(url) => BrowserOpenURL(url)}
                onSetTheme={setTheme}
                onSetLang={setLang}
                onInstallService={installService}
                serviceInfo={serviceInfo}
                onEnsureTun={ensureTun}
                onShowTunModal={() => setShowTunModal(true)}
                onApplyDefaultAutoUpdate={() =>
                  void applyDefaultAutoUpdateToProfiles()
                }
                onRefreshAllSubs={() => void refreshAllSubscriptions()}
                onExportDiagnostics={() => void exportDiagnosticsBundle()}
                onClearCache={clearTempUiState}
                onOpenResetModal={(mode) => setSettingsResetModal(mode)}
                onCheckUpdates={() =>
                  void (async () => {
                    setError('')
                    try {
                      await runUpdateCheck()
                    } catch (e: any) {
                      setError(String(e))
                    }
                  })()
                }
                onApplyUpdate={() =>
                  void (async () => {
                    setError('')
                    try {
                      const ok = window.confirm(
                        'The update will download, then the installer starts and Sloth Clash restarts to apply it. Continue?',
                      )
                      if (!ok) return
                      setUpdateProgress({ downloaded: 0, total: 0, pct: 0 })
                      await ApplyUpdate()
                      // Backend exits the process after handing off to the installer.
                      return
                    } catch (e: any) {
                      setUpdateProgress(null)
                      setError(String(e))
                    }
                    await refresh()
                    invalidateUpdateState()
                  })()
                }
                updateProgress={updateProgress}
              />
            </Suspense>
          ) : null}
        </ErrorBoundary>
      </section>

      <OperatorModal
        branding={branding}
        activeProfile={activeProfile}
        deviceIdentity={deviceIdentity}
        appVersion={
          String(updateSnap?.currentVersion ?? '').trim() ||
          ((import.meta.env.VITE_APP_VERSION as string | undefined) ?? 'dev')
        }
        coreVersion={String(state?.core?.version ?? '')}
        serviceVersion={String(serviceInfo?.version ?? '')}
        lightUI={theme === 'light'}
        open={operatorModalOpen}
        onClose={() => setOperatorModalOpen(false)}
        onBrowserOpen={(url) => BrowserOpenURL(url)}
      />

      <ProfileModalsHost
        modals={profileModals}
        profiles={state?.profile?.profiles}
        proxyGroups={state?.proxy?.groups}
        hidePaste={brandManifest?.hideLocalConfigs}
        run={run}
        refresh={refresh}
        setError={setError}
        setBanner={setTunBanner}
        pushToast={pushToast}
        onImported={dismissSpotlight}
      />

      <SettingsResetModal
        mode={settingsResetModal}
        onClose={() => setSettingsResetModal(null)}
        onConfirm={(withProfiles) => void resetAppSettings(withProfiles)}
      />

      <TunSettingsModal
        open={showTunModal}
        prefs={prefs}
        onClose={() => setShowTunModal(false)}
      />

      {spotlightOpen ? (
        <SpotlightTour
          open={spotlightOpen}
          stepIndex={spotlightStep}
          onNext={() =>
            setSpotlightStep((s) =>
              Math.min(SPOTLIGHT_TOUR_STEP_COUNT - 1, s + 1),
            )
          }
          onPrev={() => setSpotlightStep((s) => Math.max(0, s - 1))}
          onSkip={dismissSpotlight}
        />
      ) : null}

      <ToastHub toasts={toasts} onDismiss={dismissToast} />
    </div>
  )
}

export default App
