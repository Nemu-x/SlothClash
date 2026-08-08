import {
  lazy,
  Suspense,
  useCallback,
  useEffect,
  useMemo,
  useRef,
  useState,
} from 'react'
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
  SetTrafficSettings,
  SetConnectionSettings,
  SetTunSettings,
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
  GetDesktopPrefs,
  GetLaunchOnStartupPreference,
  GetPreferredLanguage,
  GetTrayAvailability,
  OnWindowBecameVisible,
  SetAppAutoUpdateEnabled,
  SetCloseToTrayPreference,
  SetExperimentalSettings,
  SetHwidEnabled,
  SetLaunchOnStartupPreference,
  SetUiLanguage,
} from './api/prefs'
import {
  ActivateProfile,
  ConfirmInstallConfigFromLink,
  DeleteProfile,
  DeriveAgePublicKey,
  GenerateAgeKeyPair,
  GetProfilePaths,
  ImportProfileFromText,
  ImportProfileFromURL,
  ReadProfileConfig,
  RefreshProfileSubscription,
  SetProfileAgeSecretKey,
  SetProfileAutoUpdate,
  SetProfileMergeTemplate,
  UpdateProfileInfo,
  WriteProfileConfig,
} from './api/profile'
import { RefreshProxies, SelectProxyGroup, SetProxyNode } from './api/proxy'
import { UpdateRuleProvider } from './api/rules'
import { BrowserOpenURL, EventsOn, WindowHide } from './api/runtime'
import {
  GetServiceInfo,
  InstallService,
  IsBootSettled,
  RefreshSlothServiceStatus,
} from './api/service'
import { GetAppState, RefreshHomeInsight } from './api/state'
import { GetSubscriptionDeviceIdentity } from './api/subscription'
import { ApplyUpdate } from './api/update'
import { DeleteProfileModal } from './components/DeleteProfileModal'
import { ErrorBoundary } from './components/ErrorBoundary'
import {
  ImportProfileModal,
  type ImportMode,
} from './components/ImportProfileModal'
import type { InstallConfigRequest } from './components/InstallConfigConfirmModal'
import { InstallConfigConfirmModal } from './components/InstallConfigConfirmModal'
import { OperatorModal } from './components/OperatorModal'
import { ProfileContextMenu } from './components/ProfileContextMenu'
import { ProfileEditInfoModal } from './components/ProfileEditInfoModal'
import { ProfileFileModal } from './components/ProfileFileModal'
import { ProfileMergeModal } from './components/ProfileMergeModal'
import { ProfileProxyModal } from './components/ProfileProxyModal'
import { ProfileRulesModal } from './components/ProfileRulesModal'
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
import { useConnectionsOverview } from './hooks/queries/useConnectionsOverview'
import { useRulesOverview } from './hooks/queries/useRulesOverview'
import { useRuntimeDiag } from './hooks/queries/useRuntimeDiag'
import { useServiceLog } from './hooks/queries/useServiceLog'
import { useUpdateState } from './hooks/queries/useUpdateState'
import { useConnectivityChecks } from './hooks/useConnectivityChecks'
import { useProxyDelay } from './hooks/useProxyDelay'
import { useRuleToggles } from './hooks/useRuleToggles'
import { useToasts } from './hooks/useToasts'
import i18n, { LS_LANG, readStoredLang } from './i18n'
import {
  DEFAULT_MERGE_TEMPLATE,
  mergeTemplateFromProfile,
} from './mergeTemplate'
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
import { parseMihomoRulesJson, parseRuleProvidersJson } from './rulesTable'
import { SpotlightTour } from './SpotlightTour'
import { SPOTLIGHT_TOUR_STEP_COUNT } from './spotlightTourConfig'
import type { CompactSettings, ImportModalReason, Screen } from './types/app'
import { deriveAccentVars, normalizeHex, resolveAccent } from './utils/accent'
import { decodeUnicodeEscapes, extractNodeFlagIso } from './utils/proxyNames'
import {
  applyUiScale,
  DEFAULT_SETTINGS,
  loadCompactSettings,
} from './utils/settings'
import { friendlyErrorMessage, yamlValidationError } from './utils/yaml'

function App() {
  const { t } = useTranslation()
  const shellRef = useRef<HTMLDivElement>(null)
  const [screen, setScreen] = useState<Screen>('home')
  // Server-state queries. Each one drives a single screen's view, polls itself
  // while that screen is open, and exposes a `refresh()` for manual triggers
  // (rule-providers bulk update, the "Refresh" buttons in headers).
  const {
    overview: rulesOverview,
    busy: rulesBusy,
    refresh: refreshRules,
  } = useRulesOverview(screen === 'rules')
  const {
    overview: connectionsOverview,
    busy: connectionsBusy,
    refresh: refreshConnections,
    closeAll: closeAllConnections,
  } = useConnectionsOverview(screen === 'connections')
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
  const [importName, setImportName] = useState('')
  const [importUrl, setImportUrl] = useState('')
  const [importMode, setImportMode] = useState<ImportMode>('url')
  const [importContent, setImportContent] = useState('')
  const [importModalOpen, setImportModalOpen] = useState(false)
  const [importModalReason, setImportModalReason] =
    useState<ImportModalReason>('manual')
  const [importBusy, setImportBusy] = useState(false)
  const [connectionsSearch, setConnectionsSearch] = useState('')
  const [ruleProviderBusyMap, setRuleProviderBusyMap] = useState<
    Record<string, boolean>
  >({})
  const [ruleProviderErrMap, setRuleProviderErrMap] = useState<
    Record<string, string>
  >({})
  const [ruleProvidersBulkBusy, setRuleProvidersBulkBusy] = useState(false)
  const [profileMenu, setProfileMenu] = useState<{
    id: string
    name: string
    x: number
    y: number
  } | null>(null)
  const [profileMergeModal, setProfileMergeModal] = useState<{
    id: string
    name: string
  } | null>(null)
  const [mergeTemplateDraft, setMergeTemplateDraft] = useState('')
  const [mergeTemplateYamlErr, setMergeTemplateYamlErr] = useState<
    string | null
  >(null)
  const [profileFileModal, setProfileFileModal] = useState<{
    id: string
    name: string
  } | null>(null)
  const [profileFileText, setProfileFileText] = useState('')
  const [profileFileYamlErr, setProfileFileYamlErr] = useState<string | null>(
    null,
  )
  const [profileFilePath, setProfileFilePath] = useState('')
  const [profileFileLoadErr, setProfileFileLoadErr] = useState('')
  const [profileProxyModal, setProfileProxyModal] = useState<{
    id: string
    name: string
  } | null>(null)
  // Proxy / Rules edit drafts now live inside ProfileProxyModal / ProfileRulesModal.
  const [profileRulesModal, setProfileRulesModal] = useState<{
    id: string
    name: string
  } | null>(null)
  const [profileEditInfo, setProfileEditInfo] = useState<{
    id: string
    name: string
    url: string
  } | null>(null)
  const [profileEditName, setProfileEditName] = useState('')
  const [profileEditUrl, setProfileEditUrl] = useState('')
  const [profileEditAutoEnabled, setProfileEditAutoEnabled] = useState(true)
  const [profileEditAutoInterval, setProfileEditAutoInterval] = useState('360')
  const [profileEditAgeKey, setProfileEditAgeKey] = useState('')
  const isAnyEditorModalOpen = Boolean(
    profileMergeModal ||
      profileFileModal ||
      profileProxyModal ||
      profileRulesModal,
  )
  const [deleteProfileModal, setDeleteProfileModal] = useState<{
    id: string
    name: string
  } | null>(null)
  const [installConfigRequest, setInstallConfigRequest] =
    useState<InstallConfigRequest | null>(null)
  const [installConfigBusy, setInstallConfigBusy] = useState(false)
  const [serviceInfo, setServiceInfo] =
    useState<main.ServiceRuntimeInfo | null>(null)
  // Session-dismiss for the app-wide "update the helper service" banner. It
  // reappears next launch while the service is still outdated.
  const [serviceBannerDismissed, setServiceBannerDismissed] = useState(false)
  const [settings, setSettings] = useState<CompactSettings>(() =>
    loadCompactSettings(),
  )
  const [settingsBusy, setSettingsBusy] = useState(false)
  const [trayAvailable, setTrayAvailable] = useState(false)
  const [showBuiltinProxyGroups, setShowBuiltinProxyGroups] = useState(false)
  const [homeActiveNodeOpen, setHomeActiveNodeOpen] = useState(false)
  const homeActiveNodeRef = useRef<HTMLDivElement | null>(null)
  const [tunPrefs, setTunPrefs] = useState<main.TunSettings>(
    () => new main.TunSettings({}),
  )
  const [trafficPrefs, setTrafficPrefs] = useState<main.TrafficSettings>(
    () => new main.TrafficSettings({}),
  )
  // Backend-owned (prefs.json), not localStorage: allow-lan changes what the
  // core binds to, so the Go side must be the source of truth.
  const [connectionPrefs, setConnectionPrefs] =
    useState<main.ConnectionSettings>(() => new main.ConnectionSettings({}))
  // HWID is enabled by default; the prefs.json field uses an optional bool so
  // a fresh install (or any pre-0.4.1 prefs file lacking `privacy`) lands on
  // `true` here. Setting this to false omits the x-hwid header on subscription
  // import / refresh — other identity headers remain.
  const [hwidEnabled, setHwidEnabled] = useState<boolean>(true)
  const [appUpdateEnabled, setAppUpdateEnabled] = useState<boolean>(true)
  // Experimental: Corporate VPN (OpenConnect) tab is opt-in, hidden by default.
  const [corpVpnEnabled, setCorpVpnEnabled] = useState<boolean>(false)
  // Live corp-VPN connection state, for the nav dot + Home indicator.
  const [corpConnected, setCorpConnected] = useState<boolean>(false)
  const [updateProgress, setUpdateProgress] = useState<{
    downloaded: number
    total: number
    pct: number
  } | null>(null)
  const [hwidSaving, setHwidSaving] = useState(false)
  const [tunDnsHijackDraft, setTunDnsHijackDraft] = useState<string>('')
  const [tunMtuDraft, setTunMtuDraft] = useState<string>('')
  const [tunDeviceDraft, setTunDeviceDraft] = useState<string>('')
  const [tunPrefsSaving, setTunPrefsSaving] = useState(false)
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
  const [ruleSearch, setRuleSearch] = useState('')
  const [ruleTypeFilter, setRuleTypeFilter] = useState('all')
  const [rulePolicyFilter, setRulePolicyFilter] = useState('all')
  const refreshInFlightRef = useRef<Promise<void> | null>(null)
  const refreshQueuedRef = useRef(false)
  const stateEventTimerRef = useRef<number | null>(null)
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

  useEffect(() => {
    setMergeTemplateYamlErr(yamlValidationError(mergeTemplateDraft, true))
  }, [mergeTemplateDraft])

  useEffect(() => {
    setProfileFileYamlErr(yamlValidationError(profileFileText, true))
  }, [profileFileText])

  // Sync the persisted autostart state with the OS-level registry on mount
  // and honour Start Minimized by hiding the window immediately when the
  // app was either launched with --minimized (autostart) or the user has
  // toggled the "Start minimized" preference.
  useEffect(() => {
    void (async () => {
      try {
        const actual = await GetLaunchOnStartupPreference()
        setSetting('launchOnStartup', Boolean(actual))
      } catch {
        /* ignore: registry not available (non-Windows / permission denied) */
      }
      // Honour ONLY the user's "Start minimized" preference. The autostart Run
      // key launches the app with --minimized, but that flag must NOT override
      // the setting — it used to force the window to the tray on every boot even
      // when "Start minimized" was off, so the app ran headless.
      if (settings.startMinimized) {
        WindowHide()
      }
      try {
        const prefs = await GetDesktopPrefs()
        const nextTun = new main.TunSettings(prefs?.tun ?? {})
        const nextTraffic = new main.TrafficSettings(prefs?.traffic ?? {})
        setTunPrefs(nextTun)
        setTrafficPrefs(nextTraffic)
        setConnectionPrefs(new main.ConnectionSettings(prefs?.connection ?? {}))
        setTunDnsHijackDraft((nextTun.dnsHijack ?? []).join(', '))
        setTunMtuDraft(nextTun.mtu ? String(nextTun.mtu) : '')
        setTunDeviceDraft(nextTun.device ?? '')
        // Privacy.hwidEnabled is *bool on the Go side: undefined/null on the
        // wire means "default → on". Only an explicit `false` should flip the
        // toggle off.
        const rawHwid = prefs?.privacy?.hwidEnabled
        setHwidEnabled(rawHwid === false ? false : true)
        // AppUpdate.autoCheckEnabled is *bool too: undefined/null → default on.
        const rawAppUpd = (prefs as any)?.appUpdate?.autoCheckEnabled
        setAppUpdateEnabled(rawAppUpd === false ? false : true)
        // Experimental.corpVpnEnabled is opt-in: only an explicit true shows it.
        const rawCorp = (prefs as any)?.experimental?.corpVpnEnabled
        const corpOn = rawCorp === true
        setCorpVpnEnabled(corpOn)
        // If it was disabled while the tab was open, fall back to Home so the
        // user isn't stranded on a now-hidden screen.
        if (!corpOn) setScreen((s) => (s === 'corp' ? 'home' : s))
      } catch {
        /* ignore: prefs API unavailable */
      }
    })()
  }, [])

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

  useEffect(() => {
    const off = EventsOn('app:state', () => {
      if (isAnyEditorModalOpen) return
      if (stateEventTimerRef.current !== null) return
      stateEventTimerRef.current = window.setTimeout(() => {
        stateEventTimerRef.current = null
        void refresh()
      }, 120)
    })
    return () => {
      off()
      if (stateEventTimerRef.current !== null) {
        window.clearTimeout(stateEventTimerRef.current)
        stateEventTimerRef.current = null
      }
    }
  }, [refresh, isAnyEditorModalOpen])

  useEffect(() => {
    const off = EventsOn('app:update', () => {
      invalidateUpdateState()
    })
    return () => off()
  }, [invalidateUpdateState])

  useEffect(() => {
    const off = EventsOn('app:update:progress', (payload: unknown) => {
      const p = payload as
        | { downloaded?: number; total?: number; pct?: number }
        | undefined
      if (!p) return
      setUpdateProgress({
        downloaded: Number(p.downloaded ?? 0),
        total: Number(p.total ?? 0),
        pct: Number(p.pct ?? -1),
      })
    })
    return () => off()
  }, [])

  useEffect(() => {
    if (!spotlightOpen) return
    setScreen('home')
  }, [spotlightOpen])

  useEffect(() => {
    const off = EventsOn('app:navigate', (payload: unknown) => {
      const p = payload as { screen?: string } | undefined
      const id = String(p?.screen ?? '').trim() as Screen
      const allowed: Screen[] = [
        'home',
        'proxies',
        'profiles',
        'rules',
        'advanced',
        'settings',
      ]
      if (!allowed.includes(id)) return
      setScreen(id)
      void refresh()
    })
    return () => off()
  }, [refresh])

  // A deep link never imports silently: the backend asks first (audit SEC1).
  useEffect(() => {
    const off = EventsOn('app:install-config-request', (payload: unknown) => {
      const p = payload as { name?: string; url?: string; host?: string }
      if (!p?.url) return
      setInstallConfigRequest({
        name: String(p.name ?? ''),
        url: String(p.url),
        host: String(p.host ?? p.url),
      })
    })
    return () => off()
  }, [])

  const confirmInstallConfig = useCallback(
    async (req: InstallConfigRequest) => {
      setInstallConfigBusy(true)
      try {
        await ConfirmInstallConfigFromLink(req.name, req.url)
      } catch (e) {
        pushToast({
          kind: 'error',
          message: friendlyErrorMessage(String(e)),
          durationMs: 0,
        })
      } finally {
        setInstallConfigBusy(false)
        setInstallConfigRequest(null)
      }
    },
    [pushToast],
  )

  useEffect(() => {
    const off = EventsOn('app:install-config', (payload: unknown) => {
      void refresh()
      setScreen('home')
      const p = payload as {
        success?: boolean
        message?: string
        profileName?: string
      }
      if (p?.success) {
        setError('')
        const n = p.profileName
          ? t('ui.toasts.profileAddedFromLink', { name: p.profileName })
          : t('ui.toasts.subAddedFromLink')
        pushToast({ kind: 'success', message: n })
      } else {
        pushToast({
          kind: 'error',
          message: String(p?.message ?? t('ui.toasts.subAddFromLinkFailed')),
          durationMs: 0,
        })
      }
    })
    return () => off()
  }, [refresh, pushToast])

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

  useEffect(() => {
    localStorage.setItem(LS_SETTINGS, JSON.stringify(settings))
  }, [settings])

  useEffect(() => {
    applyUiScale(settings.uiScale)
  }, [settings.uiScale])

  useEffect(() => {
    let cancelled = false
    let ticks = 0
    const refreshTray = async () => {
      try {
        const ok = await GetTrayAvailability()
        if (!cancelled) setTrayAvailable(Boolean(ok))
      } catch {
        if (!cancelled) setTrayAvailable(false)
      }
    }
    void (async () => {
      await refreshTray()
    })()
    const id = window.setInterval(() => {
      ticks += 1
      void refreshTray()
      if (ticks >= 20) window.clearInterval(id)
    }, 500)
    return () => {
      cancelled = true
      window.clearInterval(id)
    }
  }, [])

  useEffect(() => {
    void SetCloseToTrayPreference(
      Boolean(settings.closeToTray && trayAvailable),
    )
  }, [settings.closeToTray, trayAvailable])

  useEffect(() => {
    if (!profileMenu) return
    const close = (e: MouseEvent) => {
      const el = e.target as HTMLElement | null
      if (el?.closest?.('.ctxMenu')) return
      setProfileMenu(null)
    }
    document.addEventListener('mousedown', close)
    return () => document.removeEventListener('mousedown', close)
  }, [profileMenu])

  useEffect(() => {
    if (!profileMergeModal) return
    const raw = mergeTemplateFromProfile(
      state?.profile?.profiles,
      profileMergeModal.id,
    )
    setMergeTemplateDraft(raw.trim() ? raw : DEFAULT_MERGE_TEMPLATE)
    // Intentionally only when opening the modal — avoid overwriting edits on refresh.
  }, [profileMergeModal])

  useEffect(() => {
    if (!profileEditInfo) return
    setProfileEditName(profileEditInfo.name)
    setProfileEditUrl(profileEditInfo.url)
    setProfileEditAgeKey(
      String(
        state?.profile?.profiles?.find((x: any) => x.id === profileEditInfo.id)
          ?.ageSecretKey ?? '',
      ),
    )
    const p = state?.profile?.profiles?.find(
      (x: any) => x.id === profileEditInfo.id,
    )
    const interval = Number(p?.autoUpdateIntervalMinutes ?? 360)
    setProfileEditAutoEnabled(p?.autoUpdateEnabled === false ? false : true)
    setProfileEditAutoInterval(
      String(Number.isFinite(interval) && interval > 0 ? interval : 360),
    )
  }, [profileEditInfo])

  useEffect(() => {
    if (!profileFileModal) return
    let cancelled = false
    void (async () => {
      setProfileFileLoadErr('')
      setProfileFilePath('')
      setProfileFileText('')
      try {
        const peek = await ReadProfileConfig(profileFileModal.id)
        if (cancelled) return
        setProfileFilePath(peek.path ?? '')
        if (peek.lastError) {
          setProfileFileLoadErr(peek.lastError)
          setProfileFileText(
            `# config.yaml not found yet (${peek.lastError}).\n# Connect once to generate, then open again — or paste a full profile below.\n`,
          )
        } else {
          setProfileFileText(peek.body ?? '')
        }
      } catch (e: any) {
        if (cancelled) return
        setProfileFileLoadErr(String(e))
        setProfileFileText(
          '# Could not load current config.yaml. Paste YAML below to overwrite.\n',
        )
      }
    })()
    return () => {
      cancelled = true
    }
  }, [profileFileModal])

  const run = async (action: () => Promise<any>) => {
    setError('')
    try {
      await action()
    } catch (e: any) {
      setError(String(e))
    }
    await refresh()
  }

  const setSetting = <K extends keyof CompactSettings>(
    key: K,
    value: CompactSettings[K],
  ) => {
    setSettings((prev) => ({ ...prev, [key]: value }))
  }

  const commitTunPrefs = async (
    patch: Partial<main.TunSettings>,
    drafts?: {
      dnsHijack?: string
      mtu?: string
      device?: string
    },
  ) => {
    if (tunPrefsSaving) return
    setTunPrefsSaving(true)
    try {
      const dnsHijackRaw = drafts?.dnsHijack ?? tunDnsHijackDraft
      const dnsHijack = dnsHijackRaw
        .split(/[,\n;\r]+/)
        .map((s) => s.trim())
        .filter(Boolean)
      const mtuRaw = drafts?.mtu ?? tunMtuDraft
      const mtu = mtuRaw.trim() === '' ? 0 : Math.max(0, Number(mtuRaw) || 0)
      const device = (drafts?.device ?? tunDeviceDraft).trim()
      const next: main.TunSettings = new main.TunSettings({
        ...tunPrefs,
        ...patch,
        dnsHijack: dnsHijack.length > 0 ? dnsHijack : undefined,
        mtu: mtu > 0 ? mtu : undefined,
        device: device !== '' ? device : undefined,
      })
      const updated = await SetTunSettings(next)
      const updatedTun = new main.TunSettings(updated?.tun ?? {})
      setTunPrefs(updatedTun)
      setTunDnsHijackDraft((updatedTun.dnsHijack ?? []).join(', '))
      setTunMtuDraft(updatedTun.mtu ? String(updatedTun.mtu) : '')
      setTunDeviceDraft(updatedTun.device ?? '')
    } catch (e: any) {
      setError(String(e))
    } finally {
      setTunPrefsSaving(false)
    }
  }

  const commitConnectionPrefs = async (
    patch: Partial<main.ConnectionSettings>,
  ) => {
    if (tunPrefsSaving) return
    setTunPrefsSaving(true)
    try {
      const next: main.ConnectionSettings = new main.ConnectionSettings({
        ...connectionPrefs,
        ...patch,
      })
      const updated = await SetConnectionSettings(next)
      setConnectionPrefs(new main.ConnectionSettings(updated?.connection ?? {}))
    } catch (e: any) {
      setError(String(e))
    } finally {
      setTunPrefsSaving(false)
    }
  }

  const commitTrafficPrefs = async (patch: Partial<main.TrafficSettings>) => {
    if (tunPrefsSaving) return
    setTunPrefsSaving(true)
    try {
      const next: main.TrafficSettings = new main.TrafficSettings({
        ...trafficPrefs,
        ...patch,
      })
      const updated = await SetTrafficSettings(next)
      setTrafficPrefs(new main.TrafficSettings(updated?.traffic ?? {}))
    } catch (e: any) {
      setError(String(e))
    } finally {
      setTunPrefsSaving(false)
    }
  }

  // Tun stack / sniffer summary value still consumed by SettingsPage card.
  const tunStackValue: string = tunPrefs.stack ?? ''

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

  const hasAnyProfile = useMemo(
    () => (state?.profile?.profiles?.length ?? 0) > 0,
    [state],
  )

  const hasActiveProfile = useMemo(
    () => Boolean(state?.profile?.activeProfileId),
    [state],
  )

  const activeProfile = useMemo(() => {
    const id = state?.profile?.activeProfileId
    return state?.profile?.profiles?.find((p: any) => p.id === id)
  }, [state])

  const displayMode = optimisticMode ?? String(state?.mode?.current ?? 'rule')
  const displayTraffic = optimisticTraffic ?? String(state?.traffic ?? 'proxy')

  const connectionLabel = useMemo(() => {
    const s = state?.connection?.status
    const health = String(state?.connection?.health ?? '').trim()
    if (connectBusy || s === 'connecting') return t('ui.home.connecting')
    if (s === 'connected') {
      // Only "broken" is shown as a problem; warming/degraded are still connected for users.
      if (health === 'broken') return t('ui.home.brokenConnection')
      return t('ui.home.protected')
    }
    if (s === 'disconnecting') return t('ui.home.disconnecting')
    if (s === 'error') return t('ui.home.problem')
    return t('ui.home.notConnected')
  }, [state, connectBusy, t])

  const connectVisual = useMemo(() => {
    if (connectBusy) return 'connecting'
    const s = state?.connection?.status
    const health = String(state?.connection?.health ?? '').trim()
    if (s === 'error') return 'error'
    if (s === 'connected') {
      if (health === 'broken') return 'broken'
      return 'connected'
    }
    return 'idle'
  }, [connectBusy, state?.connection?.status, state?.connection?.health])

  const showProtectedBadge = useMemo(() => {
    const health = String(state?.connection?.health ?? '').trim()
    return state?.connection?.status === 'connected' && health !== 'broken'
  }, [state?.connection?.status, state?.connection?.health])

  const homeTrafficHealthSubtitle = useMemo(() => {
    if (state?.connection?.status !== 'connected') return ''
    const tr = displayTraffic === 'tun' ? 'TUN' : t('ui.common.proxy')
    const h = String(state?.connection?.health ?? '').trim()
    const hk =
      h === 'broken' ? t('ui.home.healthBroken') : t('ui.home.healthReady')
    return t('ui.home.trafficHealthLine', { traffic: tr, health: hk })
  }, [state?.connection?.status, state?.connection?.health, displayTraffic, t])

  const activeNode = useMemo(() => {
    const groups = (state?.proxy?.groups ?? []) as any[]
    const activeGroup = String(state?.proxy?.activeGroup ?? '').trim()
    if (!activeGroup || groups.length === 0) return ''
    const g = groups.find((x) => String(x?.name ?? '') === activeGroup)
    if (!g) return ''
    const selected = String(g?.selected ?? '').trim()
    if (!selected) return ''
    if (activeGroup === 'GLOBAL') {
      const sub = groups.find((x) => String(x?.name ?? '') === selected)
      const leaf = String(sub?.selected ?? '').trim()
      return leaf ? `${selected} -> ${leaf}` : selected
    }
    return selected
  }, [state?.proxy?.activeGroup, state?.proxy?.groups])

  const activeNodeVisual = useMemo(() => {
    const raw = decodeUnicodeEscapes(String(activeNode ?? '')).trim()
    if (!raw) return { iso: '', text: '' as string }
    const tail = raw.includes(' -> ')
      ? (raw.split(' -> ').pop() ?? '').trim()
      : raw
    const piece = tail || raw
    return {
      iso: extractNodeFlagIso(piece),
      text: piece,
    }
  }, [activeNode])

  /** Group whose `proxies` list we let the user change from Home (GLOBAL → nested group). */
  const nodePickerGroup = useMemo(() => {
    const groups = (state?.proxy?.groups ?? []) as any[]
    const activeGroup = String(state?.proxy?.activeGroup ?? '').trim()
    if (!activeGroup || groups.length === 0) return null
    const g = groups.find((x) => String(x?.name ?? '') === activeGroup)
    if (!g) return null
    if (activeGroup === 'GLOBAL') {
      const subName = String(g?.selected ?? '').trim()
      if (!subName) return null
      const sub = groups.find((x) => String(x?.name ?? '') === subName)
      if (
        !sub ||
        !Array.isArray(sub.proxies) ||
        (sub.proxies as string[]).length === 0
      ) {
        return null
      }
      return sub
    }
    if (!Array.isArray(g.proxies) || (g.proxies as string[]).length === 0) {
      return null
    }
    return g
  }, [state?.proxy?.activeGroup, state?.proxy?.groups])

  const homeAlertTooltip = useMemo(() => {
    const parts: string[] = []
    // tunBanner is a general status line ("URL copied", "service installed",
    // "cache cleared" …) — informational, not an alert. Feeding it here made
    // routine successes raise the warning badge on Home, which read as
    // "something is wrong". Only genuine connection problems belong below.
    if (state?.connection?.status === 'connected') {
      const health = String(state?.connection?.health ?? '').trim()
      if (health === 'degraded') {
        parts.push(t('ui.home.healthDegradedHint'))
      }
      if (health === 'broken') {
        parts.push(t('ui.home.healthBrokenHint'))
      }
      if (state?.connection?.lastWarning) {
        parts.push(String(state.connection.lastWarning).trim())
      }
    }
    return parts.join('\n\n')
  }, [
    t,
    state?.connection?.status,
    state?.connection?.health,
    state?.connection?.lastWarning,
  ])

  const homeUpdateTooltip = useMemo(() => {
    if (!updateSnap?.hasUpdate) return ''
    const version = String(updateSnap?.latestVersion ?? '').trim()
    return version
      ? t('ui.home.updateAvailableVersion', { version })
      : t('ui.home.updateAvailable')
  }, [t, updateSnap?.hasUpdate, updateSnap?.latestVersion])

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

  const rulesRows = useMemo(
    () => parseMihomoRulesJson(rulesOverview?.rulesBody),
    [rulesOverview?.rulesBody],
  )
  const ruleProvidersRows = useMemo(
    () => parseRuleProvidersJson(rulesOverview?.ruleProvidersBody),
    [rulesOverview?.ruleProvidersBody],
  )
  const ruleTypeFilterOptions = useMemo(() => {
    const set = new Set<string>()
    for (const r of rulesRows) {
      const t = String(r.type ?? '').trim()
      if (t) set.add(t)
    }
    return [...set].sort((a, b) => a.localeCompare(b))
  }, [rulesRows])
  const rulePolicyFilterOptions = useMemo(() => {
    const set = new Set<string>()
    for (const r of rulesRows) {
      const p = String(r.proxy ?? '').trim()
      if (p) set.add(p)
    }
    return [...set].sort((a, b) => a.localeCompare(b))
  }, [rulesRows])
  // Dashboard rule enable/disable. The hook loads the active profile's rule
  // baseline, tracks which lines are disabled (rules-template `delete` bucket),
  // and toggles them via SetProfileRulesTemplate (which auto-reconnects). See
  // useRuleToggles for the safety invariant (only unambiguous matches toggle).
  const ruleToggles = useRuleToggles({
    activeProfileId: String(state?.profile?.activeProfileId ?? '').trim(),
    profiles: state?.profile?.profiles,
    enabled: screen === 'rules',
    onError: (m) => setError(m),
    // The reconnect reloads the core; refetch /rules shortly after so the live
    // list reconciles with the optimistic view.
    onApplied: () => {
      window.setTimeout(() => void refreshRules(), 1500)
    },
  })
  // Combined display: live rows (minus any optimistically-disabled ones) tagged
  // enabled, followed by disabled rows so they can be re-enabled from the table.
  const combinedRulesRows = useMemo(() => {
    const live = rulesRows
      .filter((r) => {
        const line = ruleToggles.matchLine(r)
        return !(line && ruleToggles.deleteLines.has(line))
      })
      .map((r) => ({ ...r, disabled: false as const }))
    return [...live, ...ruleToggles.disabledRows]
  }, [
    rulesRows,
    ruleToggles.matchLine,
    ruleToggles.deleteLines,
    ruleToggles.disabledRows,
  ])
  const filteredRulesRows = useMemo(() => {
    const q = ruleSearch.trim().toLowerCase()
    return combinedRulesRows.filter((r) => {
      if (ruleTypeFilter !== 'all' && r.type !== ruleTypeFilter) return false
      if (rulePolicyFilter !== 'all' && r.proxy !== rulePolicyFilter)
        return false
      if (!q) return true
      const hay = `${r.type} ${r.payload} ${r.proxy}`.toLowerCase()
      return hay.includes(q)
    })
  }, [combinedRulesRows, ruleSearch, ruleTypeFilter, rulePolicyFilter])
  const rulesTypeTop = useMemo(() => {
    const counts = new Map<string, number>()
    for (const r of filteredRulesRows) {
      const key = String(r.type || '—')
      counts.set(key, (counts.get(key) ?? 0) + 1)
    }
    return [...counts.entries()].sort((a, b) => b[1] - a[1]).slice(0, 6)
  }, [filteredRulesRows])
  const filteredConnections = useMemo(() => {
    const rows = connectionsOverview?.connections ?? []
    const q = connectionsSearch.trim().toLowerCase()
    if (!q) return rows
    return rows.filter((c) => {
      const meta = c.metadata ?? {}
      const hay =
        `${c.id} ${meta.host ?? ''} ${meta.destinationIP ?? ''} ${meta.process ?? ''} ${c.rule ?? ''} ${c.rulePayload ?? ''}`.toLowerCase()
      return hay.includes(q)
    })
  }, [connectionsOverview?.connections, connectionsSearch])

  const dismissSpotlight = useCallback(() => {
    localStorage.setItem(LS_SPOTLIGHT, '1')
    setSpotlightOpen(false)
  }, [])

  const openImportModal = (reason: ImportModalReason) => {
    setImportModalReason(reason)
    setImportModalOpen(true)
    setError('')
  }

  const closeImportModal = () => {
    setImportModalOpen(false)
    setImportBusy(false)
  }

  const pasteFromClipboard = async () => {
    setError('')
    try {
      const text = await navigator.clipboard.readText()
      if (importMode === 'paste') setImportContent(text)
      else setImportUrl(text.trim())
    } catch {
      setError(t('ui.toasts.clipboardReadFail'))
    }
  }

  const performImportAndClose = async () => {
    setError('')
    setImportBusy(true)
    try {
      const name = importName.trim()
      if (importMode === 'paste') {
        await ImportProfileFromText(name, importContent.trim())
      } else {
        await ImportProfileFromURL(name, importUrl.trim())
      }
      await refresh()
      dismissSpotlight()
      setImportUrl('')
      setImportName('')
      setImportContent('')
      closeImportModal()
    } catch (e: any) {
      setError(String(e))
      await refresh()
    } finally {
      setImportBusy(false)
    }
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

  const refreshRuleProviderOne = useCallback(
    async (name: string) => {
      const id = String(name ?? '').trim()
      if (!id) return
      setRuleProviderErrMap((prev) => {
        if (!prev[id]) return prev
        const next = { ...prev }
        delete next[id]
        return next
      })
      setRuleProviderBusyMap((prev) => ({ ...prev, [id]: true }))
      try {
        await UpdateRuleProvider(id)
        refreshRules()
      } catch (e: any) {
        setRuleProviderErrMap((prev) => ({ ...prev, [id]: String(e) }))
      } finally {
        setRuleProviderBusyMap((prev) => ({ ...prev, [id]: false }))
      }
    },
    [refreshRules],
  )

  const refreshRuleProvidersAll = useCallback(async () => {
    if (ruleProvidersRows.length === 0) return
    setRuleProvidersBulkBusy(true)
    setRuleProviderErrMap({})
    const busySeed: Record<string, boolean> = {}
    for (const p of ruleProvidersRows) busySeed[p.name] = true
    setRuleProviderBusyMap(busySeed)
    try {
      for (const p of ruleProvidersRows) {
        try {
          await UpdateRuleProvider(p.name)
          setRuleProviderBusyMap((prev) => ({ ...prev, [p.name]: false }))
        } catch (e: any) {
          setRuleProviderErrMap((prev) => ({ ...prev, [p.name]: String(e) }))
          setRuleProviderBusyMap((prev) => ({ ...prev, [p.name]: false }))
        }
      }
      refreshRules()
    } finally {
      setRuleProvidersBulkBusy(false)
    }
  }, [refreshRules, ruleProvidersRows])
  // Rules / Connections / Logs polling moved to react-query hooks
  // (useRulesOverview, useConnectionsOverview, useServiceLog) — they fetch on
  // mount and refetch on the same screen-change semantics via their `enabled`.

  const importModalTitle = () => {
    if (importModalReason === 'connect') {
      return t('ui.import.connectNeedsProfile')
    }
    if (importModalReason === 'beacon') {
      return t('ui.import.addFirstSubscription')
    }
    return t('ui.import.importSubscription')
  }

  const importModalBlurb = () => {
    if (importModalReason === 'connect') {
      return t('ui.import.connectBlurb')
    }
    return t('ui.import.defaultBlurb')
  }

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
          serviceInfo?.unreachable) &&
        !serviceBannerDismissed ? (
          <div className="serviceUpdateBar" role="alert">
            <span className="serviceUpdateBarIcon" aria-hidden>
              ⚠️
            </span>
            <span className="serviceUpdateBarText">
              {serviceInfo?.updateRequired
                ? t('settings.serviceUpdateTitle')
                : serviceInfo?.corePinMismatch
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
              activeProfile={activeProfile}
              hideGlobalMode={brandManifest?.hideGlobalMode}
              hideProxyMode={brandManifest?.hideProxyMode}
              brandName={brandManifest?.name}
              brandLogo={branding?.logoDataUri || branding?.logoLightDataUri}
              onOpenOperator={() => setOperatorModalOpen(true)}
              service={service}
              linkToast={linkToast}
              error={error}
              updateSnap={updateSnap}
              homeUpdateTooltip={homeUpdateTooltip}
              homeAlertTooltip={homeAlertTooltip}
              hasAnyProfile={hasAnyProfile}
              displayMode={displayMode}
              displayTraffic={displayTraffic}
              connectBusy={connectBusy}
              connectVisual={connectVisual}
              connectionLabel={connectionLabel}
              showProtectedBadge={showProtectedBadge}
              homeTrafficHealthSubtitle={homeTrafficHealthSubtitle}
              nodePickerGroup={nodePickerGroup}
              homeActiveNodeOpen={homeActiveNodeOpen}
              homeActiveNodeRef={homeActiveNodeRef}
              activeNode={activeNode}
              activeNodeVisual={activeNodeVisual}
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
              onContextMenu={(target) => setProfileMenu(target)}
            />
          ) : null}

          {screen === 'connections' ? (
            <Suspense fallback={<div className="panel" />}>
              <ConnectionsPage
                overview={connectionsOverview}
                filtered={filteredConnections}
                busy={connectionsBusy}
                search={connectionsSearch}
                onSearchChange={setConnectionsSearch}
                onRefresh={() => void refreshConnections()}
                onCloseAll={() => run(closeAllConnections)}
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
                rulesOverview={rulesOverview}
                connectionStatus={state?.connection?.status ?? ''}
                rulesBusy={rulesBusy}
                providers={ruleProvidersRows}
                providerBusyMap={ruleProviderBusyMap}
                providerErrMap={ruleProviderErrMap}
                bulkBusy={ruleProvidersBulkBusy}
                rulesRows={rulesRows}
                filteredRulesRows={filteredRulesRows}
                rulesTypeTop={rulesTypeTop}
                ruleSearch={ruleSearch}
                ruleTypeFilter={ruleTypeFilter}
                rulePolicyFilter={rulePolicyFilter}
                ruleTypeOptions={ruleTypeFilterOptions}
                rulePolicyOptions={rulePolicyFilterOptions}
                error={error}
                onRefresh={() => refreshRules()}
                onRefreshAll={() => void refreshRuleProvidersAll()}
                onRefreshOne={(name) => void refreshRuleProviderOne(name)}
                onSearchChange={setRuleSearch}
                onTypeFilterChange={setRuleTypeFilter}
                onPolicyFilterChange={setRulePolicyFilter}
                ruleToggle={{
                  hasActiveProfile: ruleToggles.hasActiveProfile,
                  baselineLoading: ruleToggles.baselineLoading,
                  baselineError: ruleToggles.baselineError,
                  busyLines: ruleToggles.busyLines,
                  isToggleable: ruleToggles.isToggleable,
                  matchLine: ruleToggles.matchLine,
                  onDisable: ruleToggles.disableRow,
                  onEnable: ruleToggles.enableLine,
                }}
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
                hwidEnabled={hwidEnabled}
                hwidSaving={hwidSaving}
                onToggleHwid={(next) => {
                  if (hwidSaving) return
                  setHwidSaving(true)
                  // Optimistic flip so the toggle reflects user intent
                  // immediately; rolled back on backend error.
                  setHwidEnabled(next)
                  void SetHwidEnabled(next)
                    .then((prefs) => {
                      const raw = prefs?.privacy?.hwidEnabled
                      setHwidEnabled(raw === false ? false : true)
                    })
                    .catch((e) => {
                      setHwidEnabled(!next)
                      pushToast({ kind: 'error', message: String(e) })
                    })
                    .finally(() => setHwidSaving(false))
                }}
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
                settings={settings}
                settingsBusy={settingsBusy}
                trayAvailable={trayAvailable}
                tunStackValue={tunStackValue}
                tunPrefs={tunPrefs}
                trafficPrefs={trafficPrefs}
                allowLan={connectionPrefs.allowLan === true}
                onSetAllowLan={(next) =>
                  void commitConnectionPrefs({ allowLan: next })
                }
                dnsIpv6={connectionPrefs.dnsIpv6 !== false}
                onSetDnsIpv6={(next) =>
                  void commitConnectionPrefs({ dnsIpv6: next })
                }
                smartDns={connectionPrefs.smartDns === true}
                onSetSmartDns={(next) =>
                  void commitConnectionPrefs({ smartDns: next })
                }
                mixedPort={connectionPrefs.mixedPort ?? 0}
                runningMixedPort={Number(state?.core?.mixedPort) || 0}
                onSetMixedPort={(next) =>
                  void commitConnectionPrefs({ mixedPort: next })
                }
                tunBanner={tunBanner}
                onDismissBanner={() => setTunBanner('')}
                state={state}
                updateSnap={updateSnap}
                error={error}
                onBrowserOpen={(url) => BrowserOpenURL(url)}
                onSetTheme={setTheme}
                onSetLang={setLang}
                onSetSetting={setSetting}
                onSetLaunchOnStartup={(next) => {
                  setSetting('launchOnStartup', next)
                  void (async () => {
                    try {
                      await SetLaunchOnStartupPreference(next)
                    } catch (e: any) {
                      setError(String(e))
                      setSetting('launchOnStartup', !next)
                    }
                  })()
                }}
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
                appUpdateEnabled={appUpdateEnabled}
                onToggleAppUpdate={(next: boolean) => {
                  setAppUpdateEnabled(next)
                  void SetAppAutoUpdateEnabled(next)
                    .then((prefs: any) => {
                      const raw = prefs?.appUpdate?.autoCheckEnabled
                      setAppUpdateEnabled(raw === false ? false : true)
                    })
                    .catch(() => setAppUpdateEnabled(!next))
                }}
                corpVpnEnabled={corpVpnEnabled}
                onToggleCorpVpn={(next: boolean) => {
                  setCorpVpnEnabled(next)
                  void SetExperimentalSettings({ corpVpnEnabled: next } as any)
                    .then((prefs: any) => {
                      setCorpVpnEnabled(
                        prefs?.experimental?.corpVpnEnabled === true,
                      )
                    })
                    .catch(() => setCorpVpnEnabled(!next))
                }}
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

      <ImportProfileModal
        open={importModalOpen}
        title={importModalTitle()}
        blurb={importModalBlurb()}
        mode={importMode}
        url={importUrl}
        name={importName}
        content={importContent}
        busy={importBusy}
        hidePaste={brandManifest?.hideLocalConfigs}
        onModeChange={setImportMode}
        onUrlChange={setImportUrl}
        onNameChange={setImportName}
        onContentChange={setImportContent}
        onPasteFromClipboard={() => pasteFromClipboard()}
        onClose={closeImportModal}
        onSubmit={() => performImportAndClose()}
      />

      <ProfileMergeModal
        target={profileMergeModal}
        value={mergeTemplateDraft}
        yamlError={mergeTemplateYamlErr}
        onChange={setMergeTemplateDraft}
        onResetScaffold={() => setMergeTemplateDraft(DEFAULT_MERGE_TEMPLATE)}
        onClose={() => setProfileMergeModal(null)}
        onSave={async (id) => {
          if (mergeTemplateYamlErr) return
          setError('')
          try {
            await SetProfileMergeTemplate(id, mergeTemplateDraft)
            setProfileMergeModal(null)
            setTunBanner(
              'Merge template saved. Reconnect if you are already connected.',
            )
          } catch (e: any) {
            setError(String(e))
          }
          await refresh()
        }}
      />

      <ProfileFileModal
        target={profileFileModal}
        path={profileFilePath}
        loadError={profileFileLoadErr}
        value={profileFileText}
        yamlError={profileFileYamlErr}
        onChange={setProfileFileText}
        onCopyPath={(path) => {
          if (path) {
            void navigator.clipboard.writeText(path)
            setTunBanner(t('ui.toasts.configPathCopied'))
          }
        }}
        onClose={() => setProfileFileModal(null)}
        onSave={async (id) => {
          if (profileFileYamlErr) return
          setError('')
          try {
            await WriteProfileConfig(id, profileFileText)
            setProfileFileModal(null)
            setTunBanner(
              'Config saved. Sloth reapplies ports and secret on connect.',
            )
          } catch (e: any) {
            setError(String(e))
          }
          await refresh()
        }}
      />

      <ProfileProxyModal
        key={`pgm-${profileProxyModal?.id ?? 'closed'}`}
        target={profileProxyModal}
        profiles={state?.profile?.profiles}
        onClose={() => setProfileProxyModal(null)}
        onSaved={(banner) => {
          setProfileProxyModal(null)
          setTunBanner(banner)
          void refresh()
        }}
        onError={(msg) => setError(msg)}
      />
      <ProfileRulesModal
        key={`prm-${profileRulesModal?.id ?? 'closed'}`}
        target={profileRulesModal}
        profiles={state?.profile?.profiles}
        proxyGroups={state?.proxy?.groups}
        onClose={() => setProfileRulesModal(null)}
        onSaved={(banner) => {
          setProfileRulesModal(null)
          setTunBanner(banner)
          void refresh()
        }}
        onError={(msg) => setError(msg)}
      />
      <ProfileEditInfoModal
        target={profileEditInfo}
        name={profileEditName}
        url={profileEditUrl}
        autoEnabled={profileEditAutoEnabled}
        autoInterval={profileEditAutoInterval}
        ageKey={profileEditAgeKey}
        onAgeKeyChange={setProfileEditAgeKey}
        onGenerateAgeKeyPair={async (kind) => {
          const pair = await GenerateAgeKeyPair(kind)
          return {
            publicKey: String(pair?.publicKey ?? ''),
            secretKey: String(pair?.secretKey ?? ''),
          }
        }}
        onDeriveAgePublicKey={async (secret) =>
          String((await DeriveAgePublicKey(secret)) ?? '')
        }
        onNameChange={setProfileEditName}
        onUrlChange={setProfileEditUrl}
        onAutoEnabledToggle={() => setProfileEditAutoEnabled((v) => !v)}
        onAutoIntervalChange={setProfileEditAutoInterval}
        onCopyUrl={(url) => {
          if (url) {
            void navigator.clipboard.writeText(url)
            setTunBanner(t('ui.toasts.urlCopied'))
          }
        }}
        onCopyName={(name) => {
          if (name) {
            void navigator.clipboard.writeText(name)
            setTunBanner(t('ui.toasts.nameCopied'))
          }
        }}
        onClose={() => setProfileEditInfo(null)}
        onSave={(id) => {
          void run(async () => {
            await UpdateProfileInfo(
              id,
              profileEditName.trim(),
              profileEditUrl.trim(),
            )
            await SetProfileAgeSecretKey(id, profileEditAgeKey.trim())
            const interval = Number(profileEditAutoInterval || '360')
            await SetProfileAutoUpdate(
              id,
              profileEditAutoEnabled,
              Number.isFinite(interval) && interval > 0 ? interval : 360,
            )
          })
          setProfileEditInfo(null)
        }}
      />

      <InstallConfigConfirmModal
        request={installConfigRequest}
        busy={installConfigBusy}
        onCancel={() => setInstallConfigRequest(null)}
        onConfirm={(req) => void confirmInstallConfig(req)}
      />

      <DeleteProfileModal
        target={deleteProfileModal}
        onClose={() => setDeleteProfileModal(null)}
        onConfirm={(id) => {
          setDeleteProfileModal(null)
          void run(() => DeleteProfile(id))
        }}
      />

      <SettingsResetModal
        mode={settingsResetModal}
        onClose={() => setSettingsResetModal(null)}
        onConfirm={(withProfiles) => void resetAppSettings(withProfiles)}
      />

      <ProfileContextMenu
        target={profileMenu}
        profile={
          profileMenu
            ? (state?.profile?.profiles?.find(
                (x: any) => x.id === profileMenu.id,
              ) ?? null)
            : null
        }
        onUpdate={(id) => {
          setProfileMenu(null)
          void run(() => RefreshProfileSubscription(id))
        }}
        onEditInfo={(id, name, url) => {
          setProfileMenu(null)
          setProfileEditInfo({ id, name, url })
        }}
        onCopyUrl={(url) => {
          if (url) {
            void navigator.clipboard.writeText(url)
            setTunBanner(t('ui.toasts.urlCopied'))
          }
          setProfileMenu(null)
        }}
        onOpenRules={(id, name) => {
          setProfileMenu(null)
          setProfileRulesModal({ id, name })
        }}
        onDelete={(id, name) => {
          setProfileMenu(null)
          setDeleteProfileModal({ id, name })
        }}
        onOpenExtendConfig={(id, name) => {
          setProfileMenu(null)
          setProfileMergeModal({ id, name })
        }}
        onOpenProxyGroups={(id, name) => {
          setProfileMenu(null)
          setProfileProxyModal({ id, name })
        }}
        onOpenEditFile={(id, name) => {
          setProfileMenu(null)
          setProfileFileModal({ id, name })
        }}
      />

      <TunSettingsModal
        open={showTunModal}
        tunPrefs={tunPrefs}
        trafficPrefs={trafficPrefs}
        tunDnsHijackDraft={tunDnsHijackDraft}
        tunMtuDraft={tunMtuDraft}
        tunDeviceDraft={tunDeviceDraft}
        tunPrefsSaving={tunPrefsSaving}
        onTunDnsHijackDraftChange={setTunDnsHijackDraft}
        onTunMtuDraftChange={setTunMtuDraft}
        onTunDeviceDraftChange={setTunDeviceDraft}
        onCommitTunPrefs={(patch, drafts) => void commitTunPrefs(patch, drafts)}
        onCommitTrafficPrefs={(patch) => void commitTrafficPrefs(patch)}
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
