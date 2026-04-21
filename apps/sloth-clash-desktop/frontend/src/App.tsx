import { load as parseYaml } from 'js-yaml'
import {
  useCallback,
  useEffect,
  useMemo,
  useRef,
  useState,
  type ComponentType,
  type CSSProperties,
  type SVGProps,
} from 'react'
import { useTranslation } from 'react-i18next'

import './App.css'
// Same asset Wails uses from build/appicon.png (single source of truth for the window chrome).
import titleBarLogo from '../../build/appicon.png'
import {
  ActivateProfile,
  ApplyUpdate,
  AutoSelectProxyGroup,
  CheckForUpdates,
  Connect,
  DeleteProfile,
  Disconnect,
  EnsureTunReady,
  FetchRulesOverview,
  GetAppState,
  GetTrayAvailability,
  GetProfilePaths,
  GetPreferredLanguage,
  GetTunStatus,
  GetUpdateState,
  ImportProfileFromURL,
  InstallService,
  PeekSubscriptionFromURL,
  ReadProfileConfig,
  ReadServiceLatestLog,
  RefreshProfileSubscription,
  RefreshHomeInsight,
  RefreshProxies,
  SelectProxyGroup,
  SetProfileMergeTemplate,
  SetProfileProxyTemplate,
  SetProfileRulesTemplate,
  SetCloseToTrayPreference,
  SetMode,
  SetProfileAutoUpdate,
  SetProxyNode,
  SetTrafficMode,
  SetUpdateChannel,
  UpdateProfileInfo,
  WriteProfileConfig,
} from '../wailsjs/go/main/App'
import { main } from '../wailsjs/go/models'
import {
  BrowserOpenURL,
  EventsOn,
  Quit,
  WindowHide,
  WindowMinimise,
  WindowToggleMaximise,
} from '../wailsjs/runtime/runtime'

import i18n, { LS_LANG, readStoredLang } from './i18n'
import {
  DEFAULT_MERGE_TEMPLATE,
  mergeTemplateFromProfile,
  proxyTemplateFromProfile,
  rulesTemplateFromProfile,
  proxyBucketsFromMerge,
  proxyBucketsToAdvancedYaml,
  proxyBucketsFromAdvancedYaml,
  applyProxyBucketsToMerge,
  rulesBucketsFromMerge,
  rulesBucketsToAdvancedYaml,
  rulesBucketsFromAdvancedYaml,
  applyRulesBucketsToMerge,
  type ProxyGroupRow,
  type RuleRow,
} from './mergeTemplate'
import {
  IconAdvanced,
  IconChevronNav,
  IconHome,
  IconProfiles,
  IconProxies,
  IconRules,
  IconSettings,
} from './navIcons'
import { parseMihomoRulesJson } from './rulesTable'
import { SpotlightTour } from './SpotlightTour'
import { SPOTLIGHT_TOUR_STEP_COUNT } from './spotlightTourConfig'

type Screen =
  | 'home'
  | 'proxies'
  | 'profiles'
  | 'rules'
  | 'advanced'
  | 'settings'

const NAV_DEFS: {
  id: Screen
  labelKey: string
  Icon: ComponentType<SVGProps<SVGSVGElement>>
}[] = [
  { id: 'home', labelKey: 'nav.home', Icon: IconHome },
  { id: 'profiles', labelKey: 'nav.profiles', Icon: IconProfiles },
  { id: 'proxies', labelKey: 'nav.proxies', Icon: IconProxies },
  { id: 'rules', labelKey: 'nav.rules', Icon: IconRules },
  { id: 'advanced', labelKey: 'nav.advanced', Icon: IconAdvanced },
  { id: 'settings', labelKey: 'nav.settings', Icon: IconSettings },
]

type ImportModalReason = 'beacon' | 'connect' | 'manual'

const LS_THEME = 'sloth-theme'
const LS_SPOTLIGHT = 'sloth-spotlight-tour-v2'
const LS_NAV_COLLAPSED = 'sloth-nav-collapsed-v1'
const LS_SETTINGS = 'sloth-settings-v1'
const APP_REPO_URL = 'https://github.com/Nemu-x/SlothClash'

function isUnsafeGroupName(name: string) {
  const u = name.trim().toUpperCase()
  return u === 'DIRECT' || u === 'REJECT'
}

function extractNodeFlagIso(nodeName: string): string {
  const s = String(nodeName ?? '')
    .replace(/[\u200B-\u200D\uFEFF]/g, '')
    .trim()
  if (!s) return ''
  // Support real flag emoji in name (regional indicator pair), e.g. "🇪🇸 Node".
  const chars = [...s]
  for (let i = 0; i < chars.length - 1; i++) {
    const a = chars[i].codePointAt(0) ?? 0
    const b = chars[i + 1].codePointAt(0) ?? 0
    const isRegional = (cp: number) => cp >= 0x1f1e6 && cp <= 0x1f1ff
    if (isRegional(a) && isRegional(b)) {
      const c1 = String.fromCharCode(65 + (a - 0x1f1e6))
      const c2 = String.fromCharCode(65 + (b - 0x1f1e6))
      return `${c1}${c2}`
    }
  }
  // Common pattern: "ES Node", but also support any standalone 2-letter token.
  const m0 = /^([A-Za-z]{2})\b/.exec(s)
  if (m0) {
    const up = m0[1].toUpperCase()
    return up === 'UK' ? 'GB' : up
  }
  const skip = new Set(['WS', 'GR', 'TCP', 'UDP', 'UP', 'IP'])
  const all = s.match(/\b([A-Za-z]{2})\b/g) ?? []
  for (const t of all) {
    const up = t.toUpperCase()
    if (!skip.has(up)) return up === 'UK' ? 'GB' : up
  }
  return ''
}

function nodeDisplayName(nodeName: string): string {
  const s = String(nodeName ?? '').trim()
  if (!s) return '—'
  // Normalize noisy provider prefixes: repeated flag emojis / ISO tokens.
  let out = s
  out = out.replace(/^(?:[\u{1F1E6}-\u{1F1FF}]{2}\s*)+/gu, '')
  out = out.replace(/^(?:[A-Za-z]{2}\s+){1,4}/, '')
  // Some providers add duplicate ISO pair like "ES es Name".
  out = out.replace(/^([A-Za-z]{2})\s+\1\s+/i, '')
  out = out.trim()
  return out || s
}

function isoToFlagEmoji(iso2: string): string {
  const up = String(iso2 ?? '').toUpperCase()
  if (!/^[A-Z]{2}$/.test(up)) return ''
  return String.fromCodePoint(
    ...[...up].map((c) => 0x1f1e6 + c.charCodeAt(0) - 65),
  )
}

function FlagMark({
  iso2,
  width,
  height,
}: {
  iso2: string
  width: number
  height: number
}) {
  const iso = String(iso2 ?? '').toUpperCase()
  const emoji = isoToFlagEmoji(iso)
  const [imgVisible, setImgVisible] = useState(Boolean(iso))
  if (!iso) return null
  return (
    <>
      {imgVisible ? (
        <img
          src={`https://flagcdn.com/w20/${iso.toLowerCase()}.png`}
          alt=""
          width={width}
          height={height}
          loading="lazy"
          decoding="async"
          referrerPolicy="no-referrer"
          onError={() => setImgVisible(false)}
        />
      ) : null}
      {!imgVisible && emoji ? (
        <span className="proxyFlagEmoji">{emoji}</span>
      ) : null}
      {!imgVisible && !emoji ? (
        <span className="proxyFlagIso">{iso}</span>
      ) : null}
    </>
  )
}

function nodeFeatureTags(nodeName: string): string[] {
  const s = String(nodeName ?? '').toLowerCase()
  const out: string[] = []
  if (s.includes('vless')) out.push('VLESS')
  if (s.includes('vmess')) out.push('VMESS')
  if (s.includes('trojan')) out.push('TROJAN')
  if (s.includes('tuic')) out.push('TUIC')
  if (s.includes('hysteria')) out.push('HYSTERIA')
  if (s.includes('reality')) out.push('REALITY')
  if (s.includes('udp')) out.push('UDP')
  if (s.includes('xudp')) out.push('XUDP')
  if (s.includes('grpc')) out.push('gRPC')
  if (s.includes('xhttp')) out.push('xHTTP')
  if (s.includes('ws')) out.push('WS')
  return [...new Set(out)].slice(0, 4)
}

function selectedNodeEmoji(value: unknown): string {
  return isoToFlagEmoji(extractNodeFlagIso(String(value ?? '')))
}

function nodeOptionLabel(nodeName: string): string {
  const raw = String(nodeName ?? '').trim()
  if (!raw) return '—'
  const iso = extractNodeFlagIso(raw)
  const emoji = isoToFlagEmoji(iso)
  const name = nodeDisplayName(raw)
  return emoji ? `${emoji} ${name}` : raw
}

/** mihomo /traffic reports speeds in kbps */
function formatSpeedKbps(kbps: number | undefined): string {
  if (kbps == null || kbps < 0) return '—'
  if (kbps === 0) return '0 KB/s'
  if (kbps >= 1024) {
    const mb = kbps / 1024
    return `${mb >= 10 ? Math.round(mb) : mb.toFixed(1)} MB/s`
  }
  return `${kbps} KB/s`
}

function formatBytesSmart(n: number): string {
  if (!Number.isFinite(n) || n < 0) return '—'
  const units = ['B', 'KB', 'MB', 'GB', 'TB']
  let v = n
  let i = 0
  while (v >= 1024 && i < units.length - 1) {
    v /= 1024
    i++
  }
  const d = v >= 10 || i === 0 ? 0 : 1
  return `${v.toFixed(d)} ${units[i]}`
}

function profileSubscriptionHost(url: string): string {
  const s = String(url ?? '').trim()
  if (!s) return ''
  try {
    const u = new URL(s.includes('://') ? s : `https://${s}`)
    return u.hostname || ''
  } catch {
    return ''
  }
}

function yamlValidationError(
  text: string,
  requireMapping = true,
): string | null {
  const src = String(text ?? '').trim()
  if (!src) return null
  try {
    const parsed = parseYaml(src)
    if (
      requireMapping &&
      (parsed == null || Array.isArray(parsed) || typeof parsed !== 'object')
    ) {
      return 'YAML must be a mapping object at top-level.'
    }
    return null
  } catch (e: any) {
    return String(e?.message || e || 'Invalid YAML')
  }
}

/** `used / total` from Subscription-Userinfo (total missing ⇒ `0 B`). */
function profileTrafficPair(profile: any): string {
  const raw = String(profile?.subscriptionInfo ?? '').trim()
  if (!raw) return ''
  const parseNum = (v: unknown): number => {
    const n = Number(v)
    return Number.isFinite(n) ? n : 0
  }
  const pair = (usedBytes: number, totalBytes: number): string => {
    const u = Math.max(0, usedBytes)
    const t = totalBytes > 0 ? totalBytes : 0
    return `${formatBytesSmart(u)} / ${t > 0 ? formatBytesSmart(t) : formatBytesSmart(0)}`
  }
  const fromObj = (obj: any): string => {
    if (!obj || typeof obj !== 'object' || Array.isArray(obj)) return ''
    const u =
      obj.usage && typeof obj.usage === 'object' && !Array.isArray(obj.usage)
        ? obj.usage
        : obj
    const up = parseNum(
      u.upload ?? u.u ?? u.used_upload ?? obj.upload ?? obj.used_upload,
    )
    const down = parseNum(
      u.download ?? u.d ?? u.used_download ?? obj.download ?? obj.used_download,
    )
    const total = parseNum(u.total ?? u.t ?? u.size ?? obj.total ?? obj.t)
    const usedOnce = parseNum(u.used ?? obj.used)
    const usedBytes = up + down > 0 ? up + down : usedOnce
    return pair(usedBytes, total)
  }
  try {
    const parsed = JSON.parse(raw)
    const s = fromObj(parsed)
    if (s) return s
  } catch {
    // fall through
  }
  const flat: Record<string, string> = {}
  for (const part of raw.split(/[;&,\n]/)) {
    const seg = part.trim()
    if (!seg.includes('=')) continue
    const i = seg.indexOf('=')
    flat[seg.slice(0, i).trim().toLowerCase()] = seg.slice(i + 1).trim()
  }
  const up = Number(flat.upload ?? flat.u ?? 0)
  const down = Number(flat.download ?? flat.d ?? 0)
  const total = Number(flat.total ?? flat.t ?? flat.size ?? 0)
  const usedFlat = Number(flat.used ?? 0)
  const usedSum =
    (Number.isFinite(up) ? up : 0) + (Number.isFinite(down) ? down : 0)
  const usedBytes =
    usedSum > 0 ? usedSum : Number.isFinite(usedFlat) ? usedFlat : 0
  if (
    usedSum > 0 ||
    (Number.isFinite(usedFlat) && usedFlat > 0) ||
    (Number.isFinite(total) && total > 0)
  ) {
    return pair(usedBytes, Number.isFinite(total) ? total : 0)
  }
  return ''
}

function profileTrafficLine(profile: any): string {
  const p = profileTrafficPair(profile)
  return p ? `Traffic: ${p}` : ''
}

function formatProfileAgo(lastUpdatedUnix: number): string {
  if (!Number.isFinite(lastUpdatedUnix) || lastUpdatedUnix <= 0) return ''
  const ms = lastUpdatedUnix < 1e12 ? lastUpdatedUnix * 1000 : lastUpdatedUnix
  const diff = Date.now() - ms
  if (diff < 0) return ''
  const s = Math.floor(diff / 1000)
  if (s < 60) return `${s}s ago`
  const m = Math.floor(s / 60)
  if (m < 60) return `${m}m ago`
  const h = Math.floor(m / 60)
  if (h < 48) return `${h}h ago`
  const d = Math.floor(h / 24)
  return `${d}d ago`
}

type CompactSettings = {
  startMinimized: boolean
  launchOnStartup: boolean
  closeToTray: boolean
  dnsSmartFallback: boolean
  dnsIpv6: boolean
  dnsAllowLan: boolean
  logLevel: 'error' | 'warn' | 'info' | 'debug'
  defaultAutoUpdateMinutes: number
  reconnectOnManualProfileUpdate: boolean
}

const DEFAULT_SETTINGS: CompactSettings = {
  startMinimized: false,
  launchOnStartup: false,
  closeToTray: true,
  dnsSmartFallback: true,
  dnsIpv6: false,
  dnsAllowLan: false,
  logLevel: 'info',
  defaultAutoUpdateMinutes: 360,
  reconnectOnManualProfileUpdate: true,
}

function loadCompactSettings(): CompactSettings {
  const raw = localStorage.getItem(LS_SETTINGS)
  if (!raw) return DEFAULT_SETTINGS
  try {
    const parsed = JSON.parse(raw) as Partial<CompactSettings>
    return {
      ...DEFAULT_SETTINGS,
      ...parsed,
      defaultAutoUpdateMinutes:
        Number(parsed.defaultAutoUpdateMinutes) > 0
          ? Number(parsed.defaultAutoUpdateMinutes)
          : DEFAULT_SETTINGS.defaultAutoUpdateMinutes,
      reconnectOnManualProfileUpdate:
        parsed.reconnectOnManualProfileUpdate ?? true,
    }
  } catch {
    return DEFAULT_SETTINGS
  }
}

function App() {
  const { t } = useTranslation()
  const shellRef = useRef<HTMLDivElement>(null)
  const [screen, setScreen] = useState<Screen>('home')
  const [state, setState] = useState<any>(null)
  const [service, setService] = useState<any>(null)
  const [error, setError] = useState('')
  const [linkToast, setLinkToast] = useState('')
  const [tunBanner, setTunBanner] = useState('')
  const [profilePaths, setProfilePaths] = useState<main.ProfilePaths | null>(
    null,
  )
  const [connectivityBusy, setConnectivityBusy] = useState<string | null>(null)
  const [connectivityResults, setConnectivityResults] = useState<
    Record<string, string>
  >({})
  const [importName, setImportName] = useState('')
  const [importUrl, setImportUrl] = useState('')
  const [importModalOpen, setImportModalOpen] = useState(false)
  const [importModalReason, setImportModalReason] =
    useState<ImportModalReason>('manual')
  const [importBusy, setImportBusy] = useState(false)
  const [rulesOverview, setRulesOverview] = useState<main.RulesOverview | null>(
    null,
  )
  const [rulesBusy, setRulesBusy] = useState(false)
  const [serviceLog, setServiceLog] = useState<main.ServiceLogPeek | null>(null)
  const [updateSnap, setUpdateSnap] = useState<any>(null)
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
  const [proxyUiMode, setProxyUiMode] = useState<'visual' | 'advanced'>(
    'visual',
  )
  const [proxyMergeDraft, setProxyMergeDraft] = useState('')
  const [proxyAdvancedDraft, setProxyAdvancedDraft] = useState(
    'prepend: []\nappend: []\ndelete: []\n',
  )
  const [proxyAdvancedYamlErr, setProxyAdvancedYamlErr] = useState<
    string | null
  >(null)
  const [proxyRows, setProxyRows] = useState<ProxyGroupRow[]>([])
  const [proxyAppendRows, setProxyAppendRows] = useState<ProxyGroupRow[]>([])
  const [proxyTarget, setProxyTarget] = useState<'prepend' | 'append'>(
    'prepend',
  )
  const [pgFormName, setPgFormName] = useState('')
  const [pgFormType, setPgFormType] = useState('url-test')
  const [pgFormUse, setPgFormUse] = useState('')
  const [pgFormUrl, setPgFormUrl] = useState(
    'http://www.gstatic.com/generate_204',
  )
  const [pgFormInterval, setPgFormInterval] = useState('300')
  const [pgFormTimeout, setPgFormTimeout] = useState('3000')
  const [pgFormMaxFailed, setPgFormMaxFailed] = useState('5')
  const [pgFormLazy, setPgFormLazy] = useState(true)
  const [profileRulesModal, setProfileRulesModal] = useState<{
    id: string
    name: string
  } | null>(null)
  const [rulesUiMode, setRulesUiMode] = useState<'visual' | 'advanced'>(
    'visual',
  )
  const [rulesMergeDraft, setRulesMergeDraft] = useState('')
  const [rulesAdvancedDraft, setRulesAdvancedDraft] = useState(
    'prepend: []\nappend: []\ndelete: []\n',
  )
  const [rulesAdvancedYamlErr, setRulesAdvancedYamlErr] = useState<
    string | null
  >(null)
  const [ruleRows, setRuleRows] = useState<RuleRow[]>([])
  const [ruleAppendRows, setRuleAppendRows] = useState<RuleRow[]>([])
  const [ruleTarget, setRuleTarget] = useState<'prepend' | 'append'>('prepend')
  const [ruleFormType, setRuleFormType] = useState('DOMAIN-SUFFIX')
  const [ruleFormContent, setRuleFormContent] = useState('')
  const [ruleFormPolicy, setRuleFormPolicy] = useState('DIRECT')
  const [profileEditInfo, setProfileEditInfo] = useState<{
    id: string
    name: string
    url: string
  } | null>(null)
  const [profileEditName, setProfileEditName] = useState('')
  const [profileEditUrl, setProfileEditUrl] = useState('')
  const [profileEditAutoEnabled, setProfileEditAutoEnabled] = useState(true)
  const [profileEditAutoInterval, setProfileEditAutoInterval] = useState('360')
  const [deleteProfileModal, setDeleteProfileModal] = useState<{
    id: string
    name: string
  } | null>(null)
  const [settings, setSettings] = useState<CompactSettings>(() =>
    loadCompactSettings(),
  )
  const [settingsBusy, setSettingsBusy] = useState(false)
  const [trayAvailable, setTrayAvailable] = useState(false)
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
  const [expandedProxyGroups, setExpandedProxyGroups] = useState<
    Record<string, boolean>
  >({})
  const [ruleSearch, setRuleSearch] = useState('')
  const [ruleTypeFilter, setRuleTypeFilter] = useState('all')
  const [rulePolicyFilter, setRulePolicyFilter] = useState('all')
  const refreshInFlightRef = useRef<Promise<void> | null>(null)
  const refreshQueuedRef = useRef(false)
  const stateEventTimerRef = useRef<number | null>(null)
  const connectBusySinceRef = useRef<number | null>(null)
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

  useEffect(() => {
    setMergeTemplateYamlErr(yamlValidationError(mergeTemplateDraft, true))
  }, [mergeTemplateDraft])

  useEffect(() => {
    setProfileFileYamlErr(yamlValidationError(profileFileText, true))
  }, [profileFileText])

  useEffect(() => {
    setProxyAdvancedYamlErr(yamlValidationError(proxyAdvancedDraft, true))
  }, [proxyAdvancedDraft])

  useEffect(() => {
    setRulesAdvancedYamlErr(yamlValidationError(rulesAdvancedDraft, true))
  }, [rulesAdvancedDraft])

  useEffect(() => {
    void GetUpdateState().then(setUpdateSnap)
  }, [])

  useEffect(() => {
    const off = EventsOn('app:state', () => {
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
  }, [refresh])

  useEffect(() => {
    const off = EventsOn('app:update', () => {
      void GetUpdateState().then(setUpdateSnap)
    })
    return () => off()
  }, [])

  useEffect(() => {
    if (!spotlightOpen) return
    setScreen('home')
  }, [spotlightOpen])

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
          ? `Profile “${p.profileName}” added from link.`
          : 'Subscription added from link.'
        setLinkToast(n)
        window.setTimeout(() => setLinkToast(''), 6500)
      } else {
        setError(String(p?.message ?? 'Could not add subscription from link.'))
      }
    })
    return () => off()
  }, [refresh])

  useEffect(() => {
    if (!connectBusy) return
    const st = state?.connection?.status
    // Do not clear on transient "disconnected" during async connect bootstrap.
    // It causes visual bounce: idle -> connecting -> idle -> connected.
    if (st === 'connected' || st === 'error') {
      clearConnectBusySmooth()
    }
  }, [state?.connection?.status, connectBusy, clearConnectBusySmooth])

  useEffect(() => {
    if (state?.connection?.status !== 'connected') {
      setOptimisticMode(null)
      setOptimisticTraffic(null)
      return
    }
    // Prevent stale fatal messages from previous failed attempts lingering after recovery.
    setError('')
  }, [state?.connection?.status])

  // If /proxies was still warming up, or groups loaded before activeGroup — nudge briefly.
  useEffect(() => {
    if (state?.connection?.status !== 'connected') return
    const hasGroups = (state?.proxy?.groups?.length ?? 0) > 0
    const ag = String(state?.proxy?.activeGroup ?? '').trim()
    if (hasGroups && ag) return
    let i = 0
    const id = setInterval(() => {
      i++
      void refresh()
      if (i >= 24) clearInterval(id)
    }, 350)
    return () => clearInterval(id)
  }, [
    state?.connection?.status,
    state?.proxy?.groups?.length,
    state?.proxy?.activeGroup,
    refresh,
  ])

  useEffect(() => {
    localStorage.setItem(LS_NAV_COLLAPSED, navCollapsed ? '1' : '0')
  }, [navCollapsed])

  useEffect(() => {
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
  }, [screen, state?.connection?.status])

  // Same as Proxies: Home reads proxy state — refresh when opening Home while connected
  // so Active group / node match the core without visiting Proxies first.
  useEffect(() => {
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
  }, [screen, state?.connection?.status])

  useEffect(() => {
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
  }, [screen, state?.connection?.status, state?.mode?.current])

  useEffect(() => {
    if (screen !== 'settings') return
    void GetUpdateState().then(setUpdateSnap)
  }, [screen])

  useEffect(() => {
    localStorage.setItem(LS_THEME, theme)
    const el = shellRef.current
    if (!el) return
    const apply = () => {
      if (theme === 'system') {
        const dark = window.matchMedia('(prefers-color-scheme: dark)').matches
        el.setAttribute('data-theme', dark ? 'dark' : 'light')
      } else {
        el.setAttribute('data-theme', theme)
      }
    }
    apply()
    if (theme !== 'system') return
    const mq = window.matchMedia('(prefers-color-scheme: dark)')
    mq.addEventListener('change', apply)
    return () => mq.removeEventListener('change', apply)
  }, [theme])

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
  }, [lang])

  useEffect(() => {
    localStorage.setItem(LS_SETTINGS, JSON.stringify(settings))
  }, [settings])

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
    if (!profileProxyModal) return
    const raw = proxyTemplateFromProfile(
      state?.profile?.profiles,
      profileProxyModal.id,
    )
    setProxyMergeDraft(raw)
    const buckets = proxyBucketsFromMerge(raw)
    setProxyRows(buckets.prepend)
    setProxyAppendRows(buckets.append)
    setProxyAdvancedDraft(proxyBucketsToAdvancedYaml(buckets))
    setProxyTarget('prepend')
    setPgFormUrl('http://www.gstatic.com/generate_204')
    setPgFormInterval('300')
    setPgFormTimeout('3000')
    setPgFormMaxFailed('5')
    setPgFormLazy(true)
    setProxyUiMode('visual')
  }, [profileProxyModal])

  useEffect(() => {
    if (!profileRulesModal) return
    const raw = rulesTemplateFromProfile(
      state?.profile?.profiles,
      profileRulesModal.id,
    )
    setRulesMergeDraft(raw)
    const buckets = rulesBucketsFromMerge(raw)
    setRuleRows(buckets.prepend)
    setRuleAppendRows(buckets.append)
    setRulesAdvancedDraft(rulesBucketsToAdvancedYaml(buckets))
    setRuleTarget('prepend')
    setRulesUiMode('visual')
  }, [profileRulesModal])

  useEffect(() => {
    if (!profileEditInfo) return
    setProfileEditName(profileEditInfo.name)
    setProfileEditUrl(profileEditInfo.url)
    const p = state?.profile?.profiles?.find(
      (x: any) => x.id === profileEditInfo.id,
    )
    const interval = Number(p?.autoUpdateIntervalMinutes ?? 360)
    setProfileEditAutoEnabled(p?.autoUpdateEnabled === false ? false : true)
    setProfileEditAutoInterval(
      String(Number.isFinite(interval) && interval > 0 ? interval : 360),
    )
  }, [profileEditInfo, state?.profile?.profiles])

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

  const refreshAllSubscriptions = async () => {
    if (settingsBusy) return
    const profiles = state?.profile?.profiles ?? []
    const subs = profiles.filter((p: any) => String(p?.url ?? '').trim())
    if (subs.length === 0) {
      setTunBanner('No subscription profiles to refresh.')
      return
    }
    setSettingsBusy(true)
    setError('')
    try {
      for (const p of subs) {
        await RefreshProfileSubscription(String(p.id))
      }
      setTunBanner(`Refreshed ${subs.length} subscription profile(s).`)
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
      setTunBanner('No profiles to update yet.')
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
      const payload = {
        exportedAt: new Date().toISOString(),
        appVersion:
          (import.meta.env.VITE_APP_VERSION as string | undefined) ?? 'dev',
        coreVersion: String(state?.core?.version ?? ''),
        update: updateSnap,
        settings,
        appState: state,
        serviceLog: log,
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
      setTunBanner('Diagnostics bundle exported.')
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
    setTunBanner('Temporary UI/cache state cleared.')
  }

  const resetAppSettings = async (withProfiles: boolean) => {
    if (settingsBusy) return
    setSettingsResetModal(null)
    setSettingsBusy(true)
    setError('')
    try {
      setTheme('system')
      setLang('en')
      setSettings(DEFAULT_SETTINGS)
      localStorage.removeItem(LS_THEME)
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
        withProfiles
          ? 'Settings reset complete (profiles removed).'
          : 'Settings reset complete (profiles preserved).',
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
    if (connectBusy || s === 'connecting') return 'Connecting'
    if (s === 'connected') return 'Protected'
    if (s === 'disconnecting') return 'Disconnecting'
    if (s === 'error') return 'Problem'
    return 'Not connected'
  }, [state, connectBusy])

  const connectVisual = useMemo(() => {
    if (connectBusy) return 'connecting'
    const s = state?.connection?.status
    if (s === 'error') return 'error'
    if (s === 'connected') return 'connected'
    return 'idle'
  }, [connectBusy, state?.connection?.status])

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

  const rulePolicyOptions = useMemo(() => {
    const base = ['DIRECT', 'REJECT', 'REJECT-DROP', 'PASS']
    const groups = ((state?.proxy?.groups ?? []) as any[])
      .map((g: any) => String(g?.name ?? '').trim())
      .filter(Boolean)
    const seen = new Set<string>()
    const out: string[] = []
    for (const v of [...base, ...groups]) {
      if (seen.has(v)) continue
      seen.add(v)
      out.push(v)
    }
    return out
  }, [state?.proxy?.groups])

  useEffect(() => {
    const groups = (state?.proxy?.groups ?? []) as any[]
    if (groups.length === 0) return
    const active = String(state?.proxy?.activeGroup ?? '').trim()
    setExpandedProxyGroups((prev) => {
      const next: Record<string, boolean> = { ...prev }
      for (const g of groups) {
        const name = String(g?.name ?? '').trim()
        if (!name) continue
        if (!(name in next)) {
          next[name] = name === active
        }
      }
      return next
    })
  }, [state?.proxy?.groups, state?.proxy?.activeGroup])

  const homeAlertTooltip = useMemo(() => {
    const parts: string[] = []
    if (tunBanner?.trim()) parts.push(tunBanner.trim())
    if (
      state?.connection?.status === 'connected' &&
      state?.connection?.lastWarning
    ) {
      parts.push(String(state.connection.lastWarning).trim())
    }
    return parts.join('\n\n')
  }, [tunBanner, state?.connection?.status, state?.connection?.lastWarning])

  const loadServiceLog = useCallback(async () => {
    try {
      const peek = await ReadServiceLatestLog(200_000)
      setServiceLog(peek as main.ServiceLogPeek)
    } catch (e: any) {
      setServiceLog({
        path: '',
        text: '',
        truncated: false,
        lastError: String(e),
      } as main.ServiceLogPeek)
    }
  }, [])

  const runConnectivityCheck = useCallback(async (id: string, url: string) => {
    setConnectivityBusy(id)
    const started = performance.now()
    try {
      const ctrl = new AbortController()
      const timeout = window.setTimeout(() => ctrl.abort(), 7000)
      await fetch(url, {
        method: 'GET',
        mode: 'no-cors',
        cache: 'no-store',
        signal: ctrl.signal,
      })
      window.clearTimeout(timeout)
      const ms = Math.round(performance.now() - started)
      setConnectivityResults((prev) => ({
        ...prev,
        [id]: `Reachable (~${ms} ms)`,
      }))
    } catch (e: any) {
      const ms = Math.round(performance.now() - started)
      const msg = String(e?.message ?? e ?? 'Failed')
      setConnectivityResults((prev) => ({
        ...prev,
        [id]: `Failed (~${ms} ms): ${msg}`,
      }))
    } finally {
      setConnectivityBusy((prev) => (prev === id ? null : prev))
    }
  }, [])

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

  const rulesRows = useMemo(
    () => parseMihomoRulesJson(rulesOverview?.rulesBody),
    [rulesOverview?.rulesBody],
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
  const filteredRulesRows = useMemo(() => {
    const q = ruleSearch.trim().toLowerCase()
    return rulesRows.filter((r) => {
      if (ruleTypeFilter !== 'all' && r.type !== ruleTypeFilter) return false
      if (rulePolicyFilter !== 'all' && r.proxy !== rulePolicyFilter)
        return false
      if (!q) return true
      const hay = `${r.type} ${r.payload} ${r.proxy}`.toLowerCase()
      return hay.includes(q)
    })
  }, [rulesRows, ruleSearch, ruleTypeFilter, rulePolicyFilter])
  const rulesTypeTop = useMemo(() => {
    const counts = new Map<string, number>()
    for (const r of filteredRulesRows) {
      const key = String(r.type || '—')
      counts.set(key, (counts.get(key) ?? 0) + 1)
    }
    return [...counts.entries()].sort((a, b) => b[1] - a[1]).slice(0, 6)
  }, [filteredRulesRows])

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
      setImportUrl(text.trim())
    } catch {
      setError('Could not read clipboard — paste the URL manually.')
    }
  }

  const performImportAndClose = async () => {
    setError('')
    setImportBusy(true)
    try {
      let name = importName.trim()
      if (!name && importUrl.trim()) {
        const peek = await PeekSubscriptionFromURL(importUrl.trim())
        if (peek.suggestedName) name = peek.suggestedName
      }
      await ImportProfileFromURL(name, importUrl.trim())
      await refresh()
      dismissSpotlight()
      setImportUrl('')
      setImportName('')
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

  const installService = async () => {
    setError('')
    const result = await InstallService()
    setTunBanner(result.message)
    await refresh()
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
    setConnectBusy(true)
    try {
      await Connect()
    } catch (e: any) {
      setError(String(e))
      clearConnectBusySmooth()
    }
    await refresh()
  }

  const refreshRules = useCallback(async () => {
    setRulesBusy(true)
    setError('')
    try {
      const overview = await FetchRulesOverview()
      setRulesOverview(overview)
    } catch (e: any) {
      setError(String(e))
    } finally {
      setRulesBusy(false)
    }
  }, [])

  useEffect(() => {
    if (screen !== 'rules') return
    void refreshRules()
  }, [screen, refreshRules])

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
      <header className="titleBar">
        <div
          className="titleBarLeft wailsDrag"
          onDoubleClick={() => WindowToggleMaximise()}
        >
          <img
            className="titleBarLogo"
            src={titleBarLogo}
            alt=""
            width={32}
            height={32}
            draggable={false}
          />
          <span className="titleBarTitle">Sloth Clash</span>
        </div>
        <div className="titleBarWin noDrag">
          <button
            type="button"
            className="winBtnIcon"
            title="Minimize"
            aria-label="Minimize"
            onClick={() => WindowMinimise()}
          >
            <svg className="winIcon" viewBox="0 0 12 12" aria-hidden>
              <path d="M2.5 9h7" />
            </svg>
          </button>
          <button
            type="button"
            className="winBtnIcon"
            title="Maximize"
            aria-label="Maximize or restore"
            onClick={() => WindowToggleMaximise()}
          >
            <svg className="winIcon" viewBox="0 0 12 12" aria-hidden>
              <rect
                x="2.25"
                y="2.25"
                width="7.5"
                height="7.5"
                rx="1"
                fill="none"
              />
            </svg>
          </button>
          <button
            type="button"
            className="winBtnIcon winClose"
            title="Close"
            aria-label="Close"
            onClick={() => {
              if (settings.closeToTray) {
                if (trayAvailable) {
                  void WindowHide()
                  return
                }
                void WindowMinimise()
                return
              }
              void Quit()
            }}
          >
            <svg className="winIcon" viewBox="0 0 12 12" aria-hidden>
              <path d="M3 3l6 6M9 3L3 9" />
            </svg>
          </button>
        </div>
      </header>

      <aside className="nav">
        <nav className="navList">
          {NAV_DEFS.map(({ id, labelKey, Icon }) => (
            <button
              key={id}
              type="button"
              className={screen === id ? 'navItem active' : 'navItem'}
              title={t(labelKey)}
              onClick={() => setScreen(id)}
            >
              <span className="navIcon" aria-hidden>
                <Icon />
              </span>
              <span className="navLabel">{t(labelKey)}</span>
            </button>
          ))}
        </nav>
        <button
          type="button"
          className="navCollapseBtn"
          title={
            navCollapsed ? t('nav.expandSidebar') : t('nav.collapseSidebar')
          }
          onClick={() => setNavCollapsed((v) => !v)}
        >
          <span className={`navChevronWrap ${navCollapsed ? 'collapsed' : ''}`}>
            <IconChevronNav />
          </span>
        </button>
      </aside>

      <section className="content">
        {screen === 'home' ? (
          <div className="home">
            {linkToast ? (
              <p className="homeLinkToast" role="status">
                {linkToast}
              </p>
            ) : null}
            <header className="homeHeader">
              <div>
                <p className="eyebrow">{t('ui.home.activeProfile')}</p>
                <div className="homeTitleWithAlert">
                  <h2>{activeProfile?.name ?? t('ui.home.noProfileYet')}</h2>
                  {homeAlertTooltip ? (
                    <span
                      className="homeAlertBadge"
                      title={homeAlertTooltip}
                      role="status"
                      aria-label={homeAlertTooltip}
                    >
                      !
                    </span>
                  ) : null}
                </div>
                <p className="muted">
                  {activeProfile?.type === 'subscription'
                    ? t('ui.common.subscription')
                    : t('ui.common.local')}
                </p>
              </div>
              <div className="homeHeaderActions">
                {!hasAnyProfile ? (
                  <button
                    type="button"
                    className="pulseBeacon"
                    onClick={() => openImportModal('beacon')}
                  >
                    {t('ui.home.addSubscription')}
                  </button>
                ) : (
                  <button
                    type="button"
                    className="btn subtle"
                    onClick={() => openImportModal('manual')}
                  >
                    {t('ui.home.addSubscriptionShort')}
                  </button>
                )}
              </div>
            </header>

            <div className="connectArea">
              <div className="connectRow">
                <div className="connectSide connectSideLeft" data-tour="mode">
                  <span className="sideLabel sideLabelCentered">
                    {t('ui.home.mode')}
                  </span>
                  <div
                    className="segmentInset segmentInset3"
                    role="group"
                    aria-label="Routing mode"
                  >
                    <div
                      className="segmentGlider"
                      aria-hidden
                      style={
                        {
                          '--seg-i':
                            displayMode === 'rule'
                              ? 0
                              : displayMode === 'global'
                                ? 1
                                : 2,
                        } as CSSProperties
                      }
                    />
                    {(['rule', 'global', 'direct'] as const).map((m) => (
                      <button
                        key={m}
                        type="button"
                        title={
                          m === 'rule'
                            ? t('ui.home.ruleTitle')
                            : m === 'global'
                              ? t('ui.home.globalTitle')
                              : t('ui.home.directTitle')
                        }
                        className={
                          displayMode === m
                            ? 'segmentInsetBtn isOn'
                            : 'segmentInsetBtn'
                        }
                        onClick={() => {
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
                      >
                        {m === 'rule'
                          ? t('ui.common.rule')
                          : m === 'global'
                            ? t('ui.common.global')
                            : t('ui.common.direct')}
                      </button>
                    ))}
                  </div>
                </div>
                <div className="connectCenter">
                  <button
                    type="button"
                    className="connectBtn"
                    data-tour="connect"
                    data-visual={connectVisual}
                    disabled={connectBusy}
                    onClick={connectAction}
                  >
                    {state?.connection?.status === 'connected'
                      ? t('ui.home.disconnect')
                      : connectBusy
                        ? '…'
                        : t('ui.home.connect')}
                  </button>
                  <div className="statusLine statusLineSolo protectedLine">
                    {connectionLabel === 'Protected' ? (
                      <span className="protectedBadge">
                        <span className="protectedDot" aria-hidden />
                        <span>{t('ui.home.protected')}</span>
                      </span>
                    ) : (
                      <span className="pill">{connectionLabel}</span>
                    )}
                  </div>
                </div>
                <div
                  className="connectSide connectSideRight"
                  data-tour="traffic"
                >
                  <span className="sideLabel sideLabelCentered">
                    {t('ui.home.traffic')}
                  </span>
                  <div
                    className="segmentInset segmentInset2"
                    role="group"
                    aria-label="Traffic mode"
                  >
                    <div
                      className="segmentGlider"
                      aria-hidden
                      style={
                        {
                          '--seg-i': displayTraffic === 'proxy' ? 0 : 1,
                        } as CSSProperties
                      }
                    />
                    <button
                      type="button"
                      title="System proxy — HTTP/S via OS settings"
                      className={
                        displayTraffic === 'proxy'
                          ? 'segmentInsetBtn isOn'
                          : 'segmentInsetBtn'
                      }
                      onClick={() => switchTraffic('proxy')}
                    >
                      {t('ui.common.proxy')}
                    </button>
                    <button
                      type="button"
                      title="TUN — virtual adapter"
                      className={
                        displayTraffic === 'tun'
                          ? 'segmentInsetBtn isOn'
                          : 'segmentInsetBtn'
                      }
                      onClick={() => switchTraffic('tun')}
                    >
                      TUN
                    </button>
                  </div>
                </div>
              </div>
            </div>

            <div className="homeStatusGrid">
              <div className="statusCard statusCardCompact">
                <div className="statusRow" data-tour="service">
                  <span>{t('ui.home.service')}</span>
                  <div className="statusRowValue">
                    {!service?.installed ? (
                      <button
                        type="button"
                        className="btn btnCompact"
                        onClick={() => void installService()}
                      >
                        {t('settings.installService')}
                      </button>
                    ) : null}
                    <span
                      className={
                        service?.installed
                          ? 'statusDot statusDotOk'
                          : 'statusDot statusDotBad'
                      }
                      title={
                        service?.installed
                          ? t('ui.home.installed')
                          : t('ui.home.notInstalled')
                      }
                      aria-label={
                        service?.installed
                          ? t('ui.home.installed')
                          : t('ui.home.notInstalled')
                      }
                      role="img"
                    />
                  </div>
                </div>
                <div className="statusRow">
                  <span>{t('ui.home.activeGroup')}</span>
                  <strong>
                    {String(state?.proxy?.activeGroup ?? '').trim() || '—'}
                  </strong>
                </div>
                <div className="statusRow statusRowNode">
                  <span>{t('ui.home.pickGroup')}</span>
                  <div className="statusRowValue statusRowNodeValue">
                    {state?.connection?.status === 'connected' ? (
                      <div className="statusNodePick">
                        <select
                          className="selectModern selectInline selectCompact"
                          aria-label="Active proxy group"
                          value={String(state?.proxy?.activeGroup ?? '')}
                          onChange={(e) => {
                            const v = e.target.value
                            if (!v) return
                            void run(() => SelectProxyGroup(v))
                          }}
                        >
                          {(state?.proxy?.groups ?? []).map((g: any) => (
                            <option
                              key={g.name}
                              value={g.name}
                              disabled={isUnsafeGroupName(g.name)}
                            >
                              {g.name}
                            </option>
                          ))}
                        </select>
                      </div>
                    ) : (
                      <strong className="monoTight statusNodeTextClamp">
                        —
                      </strong>
                    )}
                  </div>
                </div>
                <div className="statusRow statusRowNode">
                  <span>{t('ui.home.activeNode')}</span>
                  <div className="statusRowValue statusRowNodeValue">
                    {nodePickerGroup &&
                    state?.connection?.status === 'connected' ? (
                      <div className="statusNodePick">
                        <select
                          className="selectModern selectInline selectCompact"
                          aria-label="Active outbound node"
                          value={
                            nodePickerGroup.selected &&
                            (nodePickerGroup.proxies ?? []).includes(
                              nodePickerGroup.selected,
                            )
                              ? nodePickerGroup.selected
                              : nodePickerGroup.selected || ''
                          }
                          onChange={(e) => {
                            const v = e.target.value
                            if (!v || !nodePickerGroup) return
                            void run(() =>
                              SetProxyNode(String(nodePickerGroup.name), v),
                            )
                          }}
                        >
                          {(nodePickerGroup.proxies ?? []).map((p: string) => (
                            <option key={p} value={p}>
                              {nodeOptionLabel(p)}
                            </option>
                          ))}
                        </select>
                      </div>
                    ) : (
                      <strong className="monoTight statusNodeTextClamp">
                        {activeNode || '—'}
                      </strong>
                    )}
                  </div>
                </div>
                <div className="statusRow">
                  <span>Core</span>
                  <span
                    className={
                      state?.core?.running
                        ? 'statusDot statusDotOk'
                        : 'statusDot statusDotBad'
                    }
                    title={state?.core?.running ? 'Running' : 'Not running'}
                    aria-label={
                      state?.core?.running ? 'Running' : 'Not running'
                    }
                    role="img"
                  />
                </div>
              </div>

              <div className="statusCard statusCardCompact">
                <div className="statusRow">
                  <span>Latency</span>
                  <strong
                    title={
                      (state?.insight?.nodeLatencyMs ?? 0) <= 0 &&
                      state?.insight?.latencyError
                        ? state.insight.latencyError
                        : undefined
                    }
                  >
                    {(state?.insight?.nodeLatencyMs ?? 0) > 0
                      ? `${state.insight.nodeLatencyMs} ms`
                      : '—'}
                  </strong>
                </div>
                <div className="statusRow statusRowMultiline">
                  <span>Exit</span>
                  <div className="statusRowValue statusExitStack">
                    {state?.insight?.exitLine ||
                    state?.insight?.exitFlagIso2 ? (
                      <strong className="statusNodeTextClamp exitGeoLine">
                        {state?.insight?.exitFlagIso2 ? (
                          <img
                            className="exitFlagImg"
                            src={`https://flagcdn.com/w20/${String(state.insight.exitFlagIso2).toLowerCase()}.png`}
                            alt=""
                            width={14}
                            height={11}
                            loading="lazy"
                            decoding="async"
                            referrerPolicy="no-referrer"
                            onError={(e) => {
                              e.currentTarget.style.display = 'none'
                            }}
                          />
                        ) : null}
                        {state?.insight?.exitLine ? (
                          <span className="exitGeoText">
                            {state.insight.exitLine}
                          </span>
                        ) : null}
                      </strong>
                    ) : null}
                    {state?.insight?.exitIp ? (
                      <span className="monoTight exitIpCompact">
                        {state.insight.exitIp}
                      </span>
                    ) : (
                      <span className="muted small insightExitErr">
                        {state?.insight?.lastError || '—'}
                      </span>
                    )}
                  </div>
                </div>
                <div className="statusRow">
                  <span>Direct IP</span>
                  <strong
                    className="monoTight"
                    title={
                      String(state?.mode?.current ?? '') === 'rule' &&
                      state?.insight?.directError
                        ? state.insight.directError
                        : undefined
                    }
                  >
                    {String(state?.mode?.current ?? '') === 'rule' &&
                    state?.insight?.directIp
                      ? state.insight.directIp
                      : '—'}
                  </strong>
                </div>
                <div className="statusRow">
                  <span>Speed</span>
                  <div
                    className="statusRowValue speedRow"
                    title={state?.insight?.trafficError || undefined}
                  >
                    <span className="speedChip" title="Upload">
                      ↑{' '}
                      {state?.insight?.trafficError
                        ? '—'
                        : formatSpeedKbps(state?.insight?.uploadKbps ?? 0)}
                    </span>
                    <span className="speedChip" title="Download">
                      ↓{' '}
                      {state?.insight?.trafficError
                        ? '—'
                        : formatSpeedKbps(state?.insight?.downloadKbps ?? 0)}
                    </span>
                  </div>
                </div>
              </div>
            </div>

            {(() => {
              const le =
                state?.connection?.status === 'error'
                  ? String(state?.connection?.lastError ?? '')
                  : ''
              const lines: string[] = []
              if (le && error === le) {
                lines.push(le)
              } else {
                if (error) lines.push(error)
                if (le && le !== error) lines.push(le)
              }
              return lines.length ? (
                <p className="error">{lines.join(' · ')}</p>
              ) : null
            })()}
          </div>
        ) : null}

        {screen === 'proxies' ? (
          <div className="panel proxiesPanel">
            <h2>{t('ui.proxies.title')}</h2>
            <p className="muted">{t('ui.proxies.lead')}</p>
            <div className="row">
              <button
                type="button"
                className="btn"
                disabled={state?.connection?.status !== 'connected'}
                onClick={() => run(() => RefreshProxies())}
              >
                {t('ui.proxies.refreshGroups')}
              </button>
              <button
                type="button"
                className="btn ghost"
                disabled={state?.connection?.status !== 'connected'}
                onClick={() => run(() => AutoSelectProxyGroup())}
              >
                {t('ui.proxies.autoPick')}
              </button>
            </div>
            <div className="proxyTopSwitches">
              <div
                className="segmentInset segmentInset3 proxyModeInset"
                role="group"
                aria-label="Proxy mode"
              >
                <div
                  className="segmentGlider"
                  aria-hidden
                  style={
                    {
                      '--seg-i':
                        displayMode === 'rule'
                          ? 0
                          : displayMode === 'global'
                            ? 1
                            : 2,
                    } as CSSProperties
                  }
                />
                <button
                  type="button"
                  className={
                    displayMode === 'rule'
                      ? 'segmentInsetBtn isOn'
                      : 'segmentInsetBtn'
                  }
                  onClick={() => run(() => SetMode('rule'))}
                >
                  {t('ui.common.rule')}
                </button>
                <button
                  type="button"
                  className={
                    displayMode === 'global'
                      ? 'segmentInsetBtn isOn'
                      : 'segmentInsetBtn'
                  }
                  onClick={() => run(() => SetMode('global'))}
                >
                  {t('ui.common.global')}
                </button>
                <button
                  type="button"
                  className={
                    displayMode === 'direct'
                      ? 'segmentInsetBtn isOn'
                      : 'segmentInsetBtn'
                  }
                  onClick={() => run(() => SetMode('direct'))}
                >
                  {t('ui.common.direct')}
                </button>
              </div>
            </div>
            <div className="segment modern policyBlock">
              <span className="segLabel">{t('ui.proxies.focusGroup')}</span>
              <select
                className="selectModern"
                value={state?.proxy?.activeGroup ?? ''}
                onChange={(e) => {
                  const v = e.target.value
                  if (!v) return
                  void run(() => SelectProxyGroup(v))
                }}
              >
                <option value="">—</option>
                {(state?.proxy?.groups ?? []).map((g: any) => (
                  <option
                    key={g.name}
                    value={g.name}
                    disabled={isUnsafeGroupName(g.name)}
                  >
                    {(() => {
                      const flag = selectedNodeEmoji(g.selected)
                      return `${flag ? `${flag} ` : ''}${g.name} (${g.type})`
                    })()}
                  </option>
                ))}
              </select>
            </div>
            <div className="proxyCardList">
              {(state?.proxy?.groups ?? []).length === 0 ? (
                <p className="muted">
                  {state?.connection?.status === 'connected'
                    ? t('ui.proxies.noGroups')
                    : t('ui.proxies.connectFirst')}
                </p>
              ) : (
                (state?.proxy?.groups ?? []).map((g: any) => (
                  <div key={g.name} className="proxyGroupCard">
                    <div className="proxyGroupHead">
                      <div className="proxyGroupHeadMain">
                        <div className="proxyGroupTitle">
                          {(() => {
                            const emoji = selectedNodeEmoji(g.selected)
                            return (
                              <>
                                {emoji ? (
                                  <span className="proxyGroupFlag">
                                    {emoji}
                                  </span>
                                ) : null}
                                <span>{g.name}</span>
                              </>
                            )
                          })()}
                        </div>
                        <div className="proxyGroupMeta">
                          <span className="proxyTypeChip">{g.type}</span>
                          <span className="proxyCountChip">
                            {(g.proxies ?? []).length}
                          </span>
                        </div>
                        <div
                          className="proxySelectedInline"
                          title={String(g.selected ?? '')}
                        >
                          {(() => {
                            const iso = extractNodeFlagIso(
                              String(g.selected ?? ''),
                            )
                            return (
                              <>
                                <FlagMark iso2={iso} width={14} height={10} />
                                <span className="proxySelectedName">
                                  {nodeDisplayName(String(g.selected ?? ''))}
                                </span>
                                <div className="proxyNodeTags">
                                  {nodeFeatureTags(
                                    String(g.selected ?? ''),
                                  ).map((t) => (
                                    <span key={t} className="proxyNodeTag">
                                      {t}
                                    </span>
                                  ))}
                                </div>
                              </>
                            )
                          })()}
                        </div>
                      </div>
                      <button
                        type="button"
                        className="btn ghost proxyExpandBtn"
                        onClick={() =>
                          setExpandedProxyGroups((prev) => ({
                            ...prev,
                            [String(g.name)]: !prev[String(g.name)],
                          }))
                        }
                      >
                        {expandedProxyGroups[String(g.name)]
                          ? 'Collapse'
                          : 'Expand'}
                      </button>
                    </div>
                    {expandedProxyGroups[String(g.name)] ? (
                      <div className="proxyNodesGrid">
                        {(g.proxies ?? []).map((p: string) => {
                          const active = String(g.selected ?? '') === p
                          const iso = extractNodeFlagIso(p)
                          return (
                            <button
                              key={p}
                              type="button"
                              className={`proxyNodeCard${active ? ' active' : ''}`}
                              title={p}
                              onClick={() => {
                                if (!p || active) return
                                void run(() => SetProxyNode(g.name, p))
                              }}
                            >
                              <div className="proxyNodeTop">
                                <FlagMark iso2={iso} width={16} height={12} />
                                <span className="proxyNodeName">
                                  {nodeDisplayName(p)}
                                </span>
                                <div className="proxyNodeTags">
                                  {nodeFeatureTags(p).map((t) => (
                                    <span key={t} className="proxyNodeTag">
                                      {t}
                                    </span>
                                  ))}
                                </div>
                              </div>
                            </button>
                          )
                        })}
                      </div>
                    ) : null}
                  </div>
                ))
              )}
            </div>
            {error ? <p className="error">{error}</p> : null}
          </div>
        ) : null}

        {screen === 'profiles' ? (
          <div className="panel">
            <div className="profilesHeader">
              <h2 className="profilesPageTitle">{t('ui.profiles.title')}</h2>
              <p className="muted profilesLead">{t('ui.profiles.lead')}</p>
            </div>
            <div className="profilesToolbar">
              <button
                type="button"
                className="btn primary"
                onClick={() => openImportModal('manual')}
              >
                {t('ui.profiles.importSubscription')}
              </button>
            </div>
            <div className="profileList">
              {state?.profile?.profiles?.map((p: any) => {
                const active = state?.profile?.activeProfileId === p.id
                const trafficPair = profileTrafficPair(p)
                const trafficTitle = profileTrafficLine(p)
                const host = profileSubscriptionHost(String(p.url ?? ''))
                const ago = formatProfileAgo(Number(p.lastUpdated ?? 0))
                return (
                  <div
                    key={p.id}
                    className={`profileCard${active ? ' profileCardActive' : ''}`}
                    role="button"
                    tabIndex={0}
                    onClick={() => {
                      if (active) return
                      void run(() => ActivateProfile(p.id))
                    }}
                    onKeyDown={(e) => {
                      if (active) return
                      if (e.key === 'Enter' || e.key === ' ') {
                        e.preventDefault()
                        void run(() => ActivateProfile(p.id))
                      }
                    }}
                    onContextMenu={(e) => {
                      e.preventDefault()
                      e.stopPropagation()
                      setProfileMenu({
                        id: p.id,
                        name: p.name,
                        x: e.clientX,
                        y: e.clientY,
                      })
                    }}
                  >
                    <div className="profileCardInner">
                      <div className="profileCardTopRow">
                        <div
                          className="profileTitle"
                          title={String(p.name ?? '')}
                        >
                          {p.name}
                        </div>
                        <button
                          type="button"
                          className={`profileRefreshIcon${profileRefreshBusyId === p.id ? ' isBusy' : ''}`}
                          aria-label={t('ui.profiles.refreshSubscription')}
                          title={t('ui.profiles.refreshSubscription')}
                          disabled={
                            !String(p.url ?? '').trim() ||
                            profileRefreshBusyId === p.id
                          }
                          onClick={(e) => {
                            e.preventDefault()
                            e.stopPropagation()
                            if (!String(p.url ?? '').trim()) return
                            const id = p.id
                            setProfileRefreshBusyId(id)
                            void (async () => {
                              try {
                                await run(() => RefreshProfileSubscription(id))
                              } finally {
                                setProfileRefreshBusyId((cur) =>
                                  cur === id ? null : cur,
                                )
                              }
                            })()
                          }}
                        >
                          <svg
                            className="profileRefreshSvg"
                            viewBox="0 0 24 24"
                            width="18"
                            height="18"
                            aria-hidden
                          >
                            <path
                              fill="none"
                              stroke="currentColor"
                              strokeWidth="2"
                              strokeLinecap="round"
                              strokeLinejoin="round"
                              d="M21 12a9 9 0 1 1-3-6.7"
                            />
                            <path
                              fill="none"
                              stroke="currentColor"
                              strokeWidth="2"
                              strokeLinecap="round"
                              strokeLinejoin="round"
                              d="M21 3v6h-6"
                            />
                          </svg>
                        </button>
                      </div>
                      <div className="profileHostRow">
                        <span
                          className="profileHost"
                          title={p.url ? String(p.url) : undefined}
                        >
                          {host ||
                            (p.url
                              ? t('ui.common.subscription')
                              : t('ui.profiles.localNoUrl'))}
                        </span>
                        {ago ? (
                          <span className="profileUpdated">{ago}</span>
                        ) : null}
                      </div>
                      {trafficPair ? (
                        <div
                          className="profileTrafficPair"
                          title={trafficTitle || trafficPair}
                        >
                          {trafficPair}
                        </div>
                      ) : null}
                      <div className="profileCardFoot">
                        <span className="profileTypeChip">{p.type}</span>
                        {active ? (
                          <span className="profileBadge">
                            {t('ui.profiles.active')}
                          </span>
                        ) : (
                          <span className="profileClickHint">
                            {t('ui.profiles.activate')}
                          </span>
                        )}
                      </div>
                    </div>
                  </div>
                )
              })}
            </div>
            {error ? <p className="error">{error}</p> : null}
          </div>
        ) : null}

        {screen === 'rules' ? (
          <div className="panel">
            <h2>{t('ui.rules.title')}</h2>
            <p className="muted">
              {t('ui.rules.leadPrefix')}{' '}
              <code className="code">SLOTH_CLASH_CONTROLLER</code>{' '}
              {t('ui.rules.leadSuffix')}
            </p>

            <div className="homeCard">
              <div className="homeCardHead">
                <div>
                  <p className="eyebrow">{t('ui.rules.liveEyebrow')}</p>
                  <h3 className="homeCardTitle">{t('ui.rules.liveTitle')}</h3>
                </div>
                <button
                  type="button"
                  className="btn"
                  disabled={rulesBusy}
                  onClick={() => refreshRules()}
                >
                  {rulesBusy ? t('ui.rules.refreshing') : t('ui.rules.refresh')}
                </button>
              </div>
              {rulesOverview?.lastError ? (
                <p className="error tight">{rulesOverview.lastError}</p>
              ) : null}
              {rulesOverview?.reachable ? (
                <p className="muted small tight">
                  {t('ui.rules.reachable')}: {rulesOverview.controller}
                </p>
              ) : null}
              {rulesRows.length > 0 ? (
                <>
                  <div className="rulesFilterRow">
                    <input
                      className="input rulesFilterSearch"
                      value={ruleSearch}
                      onChange={(e) => setRuleSearch(e.target.value)}
                      placeholder={t('ui.rules.searchPlaceholder')}
                    />
                    <select
                      className="selectModern rulesFilterSelect"
                      value={ruleTypeFilter}
                      onChange={(e) => setRuleTypeFilter(e.target.value)}
                    >
                      <option value="all">{t('ui.rules.allTypes')}</option>
                      {ruleTypeFilterOptions.map((t) => (
                        <option key={t} value={t}>
                          {t}
                        </option>
                      ))}
                    </select>
                    <select
                      className="selectModern rulesFilterSelect"
                      value={rulePolicyFilter}
                      onChange={(e) => setRulePolicyFilter(e.target.value)}
                    >
                      <option value="all">{t('ui.rules.allPolicies')}</option>
                      {rulePolicyFilterOptions.map((p) => (
                        <option key={p} value={p}>
                          {p}
                        </option>
                      ))}
                    </select>
                  </div>
                  <div className="rulesSummaryRow">
                    <span className="rulesSummaryChip">
                      {t('ui.rules.total')}: {filteredRulesRows.length}/
                      {rulesRows.length}
                    </span>
                    {rulesTypeTop.map(([t, c]) => (
                      <span key={t} className="rulesSummaryChip">
                        {t}: {c}
                      </span>
                    ))}
                  </div>
                  <div className="rulesTableWrap">
                    <table className="rulesTable">
                      <thead>
                        <tr>
                          <th>#</th>
                          <th>{t('ui.rules.type')}</th>
                          <th>{t('ui.rules.match')}</th>
                          <th>{t('ui.rules.policy')}</th>
                        </tr>
                      </thead>
                      <tbody>
                        {filteredRulesRows.map((r) => (
                          <tr key={`${r.idx}-${r.type}-${r.payload}`}>
                            <td>{r.idx}</td>
                            <td>{r.type}</td>
                            <td className="rulesPayload">{r.payload || '—'}</td>
                            <td
                              className={
                                r.proxy && r.proxy !== 'DIRECT'
                                  ? 'rulesPolicy rulesPolicyProxy'
                                  : 'rulesPolicy'
                              }
                            >
                              {r.proxy}
                            </td>
                          </tr>
                        ))}
                      </tbody>
                    </table>
                  </div>
                </>
              ) : (
                <p className="muted small tight">{t('ui.rules.noParsed')}</p>
              )}
              {rulesOverview?.ruleProvidersBody ? (
                <p className="muted small tight">
                  {t('ui.rules.providersHint')}
                </p>
              ) : null}
            </div>
            {error ? <p className="error">{error}</p> : null}
          </div>
        ) : null}

        {screen === 'advanced' ? (
          <div className="panel advancedPanel">
            <h2>{t('advanced.title')}</h2>
            <p className="muted">{t('ui.advanced.lead')}</p>

            <div className="advancedGrid">
              <div className="homeCard">
                <h3 className="homeCardTitle">
                  {t('ui.advanced.diagnostics')}
                </h3>
                <div className="statusRow">
                  <span>{t('advanced.connection')}</span>
                  <strong>{String(state?.connection?.status ?? '—')}</strong>
                </div>
                <div className="statusRow">
                  <span>{t('ui.advanced.coreVersion')}</span>
                  <strong>{String(state?.core?.version ?? '—')}</strong>
                </div>
                <div className="statusRow">
                  <span>{t('ui.advanced.controller')}</span>
                  <strong className="monoTight">
                    {String(state?.core?.controllerAddr ?? '—')}
                  </strong>
                </div>
                <div className="statusRow">
                  <span>{t('ui.advanced.mixedPort')}</span>
                  <strong>{state?.core?.mixedPort || '—'}</strong>
                </div>
                <div className="statusRow">
                  <span>{t('ui.advanced.runtimeDir')}</span>
                  <strong className="monoTight">
                    {String(profilePaths?.dataDir ?? '—')}
                  </strong>
                </div>
                <div className="statusRow">
                  <span>{t('ui.advanced.configFile')}</span>
                  <strong className="monoTight">
                    {String(profilePaths?.configPath ?? '—')}
                  </strong>
                </div>
              </div>

              <div className="homeCard">
                <h3 className="homeCardTitle">
                  {t('ui.advanced.connectivityTools')}
                </h3>
                <p className="muted small">
                  {t('ui.advanced.connectivityLead')}
                </p>
                <div className="row">
                  <button
                    type="button"
                    className="btn ghost"
                    onClick={() =>
                      runConnectivityCheck(
                        'google',
                        'https://www.google.com/generate_204',
                      )
                    }
                    disabled={connectivityBusy === 'google'}
                  >
                    Google
                  </button>
                  <button
                    type="button"
                    className="btn ghost"
                    onClick={() =>
                      runConnectivityCheck(
                        'youtube',
                        'https://www.youtube.com/generate_204',
                      )
                    }
                    disabled={connectivityBusy === 'youtube'}
                  >
                    YouTube
                  </button>
                  <button
                    type="button"
                    className="btn ghost"
                    onClick={() =>
                      runConnectivityCheck(
                        'telegram',
                        'https://web.telegram.org',
                      )
                    }
                    disabled={connectivityBusy === 'telegram'}
                  >
                    Telegram
                  </button>
                </div>
                <div className="diagList">
                  <div className="statusRow">
                    <span>Google</span>
                    <strong>{connectivityResults.google ?? '—'}</strong>
                  </div>
                  <div className="statusRow">
                    <span>YouTube</span>
                    <strong>{connectivityResults.youtube ?? '—'}</strong>
                  </div>
                  <div className="statusRow">
                    <span>Telegram</span>
                    <strong>{connectivityResults.telegram ?? '—'}</strong>
                  </div>
                </div>
              </div>
            </div>

            <div className="homeCard">
              <h3 className="homeCardTitle">Service log (tail)</h3>
              <details className="compatDetails">
                <summary className="compatSummary">
                  Show live service logs
                </summary>
                <p className="muted small">
                  Windows service core writes to{' '}
                  <code className="monoTight">logs/service_latest.log</code>{' '}
                  under the active profile runtime folder.
                </p>
                <div className="row">
                  <button
                    type="button"
                    className="btn"
                    onClick={() => void loadServiceLog()}
                  >
                    Refresh log
                  </button>
                </div>
                {serviceLog?.path ? (
                  <div className="statusRow">
                    <span>Log file</span>
                    <strong className="monoTight">{serviceLog.path}</strong>
                  </div>
                ) : null}
                {serviceLog?.truncated ? (
                  <p className="muted small">Showing last ~200 KB only.</p>
                ) : null}
                {serviceLog?.lastError ? (
                  <p className="error tight">{serviceLog.lastError}</p>
                ) : null}
                {serviceLog?.text ? (
                  <pre className="mono tightPre logPre">{serviceLog.text}</pre>
                ) : !serviceLog?.lastError ? (
                  <p className="muted small">No log text yet — tap Refresh.</p>
                ) : null}
              </details>
            </div>

            <div className="homeCard">
              <h3 className="homeCardTitle">Maintenance</h3>
              <p className="muted small">
                Safe operations to keep runtime healthy without full reset.
              </p>
              <div className="row">
                <button
                  type="button"
                  className="btn ghost"
                  onClick={() => run(() => RefreshProxies())}
                  disabled={state?.connection?.status !== 'connected'}
                >
                  Refresh proxies snapshot
                </button>
                <button
                  type="button"
                  className="btn ghost"
                  onClick={() => run(() => RefreshHomeInsight())}
                  disabled={state?.connection?.status !== 'connected'}
                >
                  Refresh home insight
                </button>
              </div>
            </div>
            {error ? <p className="error">{error}</p> : null}
          </div>
        ) : null}

        {screen === 'settings' ? (
          <div className="panel settingsPanel">
            <div className="settingsTopBar">
              <h2>{t('settings.title')}</h2>
              <button
                type="button"
                className="settingsGithubBtn settingsGithubTopBtn"
                title="Open GitHub repository"
                aria-label="Open GitHub repository"
                onClick={() => BrowserOpenURL(APP_REPO_URL)}
              >
                <svg
                  className="settingsGithubSvg"
                  viewBox="0 0 24 24"
                  width="22"
                  height="22"
                  aria-hidden
                >
                  <path
                    fill="currentColor"
                    d="M12 0c-6.626 0-12 5.373-12 12 0 5.302 3.438 9.8 8.207 11.387.599.111.793-.261.793-.577v-2.234c-3.338.726-4.033-1.416-4.033-1.416-.546-1.387-1.333-1.756-1.333-1.756-1.089-.745.083-.729.083-.729 1.205.084 1.839 1.237 1.839 1.237 1.07 1.834 2.807 1.304 3.492.997.107-.775.418-1.305.762-1.604-2.665-.305-5.467-1.334-5.467-5.931 0-1.311.469-2.381 1.236-3.221-.124-.303-.535-1.524.117-3.176 0 0 1.008-.322 3.301 1.23.957-.266 1.983-.399 3.003-.404 1.02.005 2.047.138 3.006.404 2.291-1.552 3.297-1.23 3.297-1.23.653 1.653.242 2.874.118 3.176.77.84 1.235 1.911 1.235 3.221 0 4.609-2.807 5.624-5.479 5.921.43.372.823 1.102.823 2.222v3.293c0 .319.192.694.801.576 4.765-1.589 8.199-6.086 8.199-11.386 0-6.627-5.373-12-12-12z"
                  />
                </svg>
              </button>
            </div>
            <p className="muted settingsPanelLead">{t('settings.lead')}</p>

            <div className="settingsGridCompact">
              <div className="homeCard settingsCardCompact">
                <h3 className="homeCardTitle">{t('settings.general')}</h3>
                <label className="field">
                  <span className="fieldLab">{t('settings.theme')}</span>
                  <div className="segPill">
                    {(['system', 'dark', 'light'] as const).map((th) => (
                      <button
                        key={th}
                        type="button"
                        className={theme === th ? 'pillOpt active' : 'pillOpt'}
                        onClick={() => setTheme(th)}
                      >
                        {th === 'system'
                          ? t('settings.themeSystem')
                          : th === 'dark'
                            ? t('settings.themeDark')
                            : t('settings.themeLight')}
                      </button>
                    ))}
                  </div>
                </label>
                <label className="field">
                  <span className="fieldLab">{t('settings.language')}</span>
                  <select
                    className="selectModern"
                    value={lang}
                    onChange={(e) =>
                      setLang(e.target.value as 'en' | 'ru' | 'zh')
                    }
                  >
                    <option value="en">English</option>
                    <option value="ru">Русский</option>
                    <option value="zh">简体中文</option>
                  </select>
                </label>
                <div className="settingsToggleRow">
                  <span>{t('settings.startMinimized')}</span>
                  <button
                    type="button"
                    className={`trafficKnob ${settings.startMinimized ? 'on' : ''}`}
                    onClick={() =>
                      setSetting('startMinimized', !settings.startMinimized)
                    }
                  >
                    {settings.startMinimized ? t('common.on') : t('common.off')}
                  </button>
                </div>
                <div className="settingsToggleRow">
                  <span>{t('settings.launchOnStartup')}</span>
                  <button
                    type="button"
                    className={`trafficKnob ${settings.launchOnStartup ? 'on' : ''}`}
                    onClick={() =>
                      setSetting('launchOnStartup', !settings.launchOnStartup)
                    }
                  >
                    {settings.launchOnStartup
                      ? t('common.on')
                      : t('common.off')}
                  </button>
                </div>
                <div className="settingsToggleRow">
                  <span>{t('settings.closeToTray')}</span>
                  <button
                    type="button"
                    className={`trafficKnob ${settings.closeToTray ? 'on' : ''}`}
                    disabled={!trayAvailable}
                    onClick={() =>
                      trayAvailable &&
                      setSetting('closeToTray', !settings.closeToTray)
                    }
                  >
                    {settings.closeToTray ? t('common.on') : t('common.off')}
                  </button>
                </div>
              </div>

              <div className="homeCard settingsCardCompact">
                <h3 className="homeCardTitle">{t('settings.connection')}</h3>
                <div className="settingsToggleRow">
                  <span>{t('settings.smartDns')}</span>
                  <button
                    type="button"
                    className={`trafficKnob ${settings.dnsSmartFallback ? 'on' : ''}`}
                    onClick={() =>
                      setSetting('dnsSmartFallback', !settings.dnsSmartFallback)
                    }
                  >
                    {settings.dnsSmartFallback
                      ? t('common.on')
                      : t('common.off')}
                  </button>
                </div>
                <div className="settingsToggleRow">
                  <span>{t('settings.ipv6Dns')}</span>
                  <button
                    type="button"
                    className={`trafficKnob ${settings.dnsIpv6 ? 'on' : ''}`}
                    onClick={() => setSetting('dnsIpv6', !settings.dnsIpv6)}
                  >
                    {settings.dnsIpv6 ? t('common.on') : t('common.off')}
                  </button>
                </div>
                <div className="settingsToggleRow">
                  <span>{t('settings.allowLanBinding')}</span>
                  <button
                    type="button"
                    className={`trafficKnob ${settings.dnsAllowLan ? 'on' : ''}`}
                    onClick={() =>
                      setSetting('dnsAllowLan', !settings.dnsAllowLan)
                    }
                  >
                    {settings.dnsAllowLan ? t('common.on') : t('common.off')}
                  </button>
                </div>
                <p className="muted settingsMicroHint">
                  {t('settings.dnsHint')}
                </p>
                <div className="row">
                  <button
                    type="button"
                    className="btn ghost"
                    onClick={installService}
                  >
                    {t('settings.installService')}
                  </button>
                  <button
                    type="button"
                    className="btn ghost"
                    onClick={ensureTun}
                  >
                    {t('settings.guidedTun')}
                  </button>
                </div>
              </div>

              <div className="homeCard settingsCardCompact">
                <h3 className="homeCardTitle">
                  {t('settings.profilesUpdates')}
                </h3>
                <label className="field">
                  <span className="fieldLab">
                    Default auto-update interval (min)
                  </span>
                  <input
                    className="input"
                    value={String(settings.defaultAutoUpdateMinutes)}
                    onChange={(e) =>
                      setSetting(
                        'defaultAutoUpdateMinutes',
                        Math.max(5, Number(e.target.value || 360)),
                      )
                    }
                    placeholder="360"
                  />
                </label>
                <div className="settingsToggleRow">
                  <span>Reconnect on manual active-profile update</span>
                  <button
                    type="button"
                    className={`trafficKnob ${settings.reconnectOnManualProfileUpdate ? 'on' : ''}`}
                    onClick={() =>
                      setSetting(
                        'reconnectOnManualProfileUpdate',
                        !settings.reconnectOnManualProfileUpdate,
                      )
                    }
                  >
                    {settings.reconnectOnManualProfileUpdate
                      ? t('common.yes')
                      : t('common.no')}
                  </button>
                </div>
                <div className="row">
                  <button
                    type="button"
                    className="btn ghost"
                    disabled={settingsBusy}
                    onClick={() => void applyDefaultAutoUpdateToProfiles()}
                  >
                    Apply defaults to profiles
                  </button>
                  <button
                    type="button"
                    className="btn"
                    disabled={settingsBusy}
                    onClick={() => void refreshAllSubscriptions()}
                  >
                    Refresh all subscriptions
                  </button>
                </div>
              </div>

              <div className="homeCard settingsCardCompact">
                <h3 className="homeCardTitle">{t('settings.dataDiag')}</h3>
                <label className="field">
                  <span className="fieldLab">Log level</span>
                  <select
                    className="selectModern"
                    value={settings.logLevel}
                    onChange={(e) =>
                      setSetting(
                        'logLevel',
                        e.target.value as CompactSettings['logLevel'],
                      )
                    }
                  >
                    <option value="error">error</option>
                    <option value="warn">warn</option>
                    <option value="info">info</option>
                    <option value="debug">debug</option>
                  </select>
                </label>
                <div className="row">
                  <button
                    type="button"
                    className="btn ghost"
                    disabled={settingsBusy}
                    onClick={() => void exportDiagnosticsBundle()}
                  >
                    Export diagnostics bundle
                  </button>
                  <button
                    type="button"
                    className="btn ghost"
                    onClick={clearTempUiState}
                  >
                    Clear cache/temp
                  </button>
                </div>
                <div className="row">
                  <button
                    type="button"
                    className="btn ghost"
                    onClick={() => setSettingsResetModal('keep_profiles')}
                  >
                    Reset app settings
                  </button>
                  <button
                    type="button"
                    className="btn ghost"
                    onClick={() => setSettingsResetModal('with_profiles')}
                  >
                    Reset + remove profiles
                  </button>
                </div>
              </div>
            </div>

            <div className="homeCard settingsCardCompact settingsInfoDevCard">
              <div className="settingsInfoDevHead">
                <h3 className="homeCardTitle settingsInfoDevTitle">
                  {t('settings.info')}
                </h3>
                <span className="muted settingsMicroHint">
                  Developer: Nemu-x
                </span>
              </div>
              <div className="settingsInfoDevBody">
                <div className="settingsKpiGrid">
                  <div className="settingsKpi">
                    <span>Core</span>
                    <strong title="Mihomo / embedded core">
                      {state?.core?.version?.trim() ? state.core.version : '—'}
                    </strong>
                  </div>
                  <div className="settingsKpi">
                    <span>{t('settings.appVersionLabel')}</span>
                    <strong>
                      {String(updateSnap?.currentVersion ?? '').trim()
                        ? String(updateSnap.currentVersion)
                        : ((import.meta.env.VITE_APP_VERSION as
                            | string
                            | undefined) ?? 'dev')}
                    </strong>
                  </div>
                  <div className="settingsKpi">
                    <span>Channel</span>
                    <strong>{updateSnap?.channel ?? 'stable'}</strong>
                  </div>
                  <div className="settingsKpi">
                    <span>Checked</span>
                    <strong>
                      {updateSnap?.lastCheckedAt
                        ? new Date(
                            (updateSnap.lastCheckedAt as number) * 1000,
                          ).toLocaleString()
                        : 'Never'}
                    </strong>
                  </div>
                </div>
                <div className="settingsInfoDevActions">
                  {updateSnap?.hasUpdate ? (
                    <p className="banner" role="status">
                      {t('settings.updateAvailable', {
                        version: String(updateSnap.latestVersion ?? ''),
                      })}
                    </p>
                  ) : updateSnap?.lastCheckedAt && !updateSnap?.lastError ? (
                    <p className="muted small" role="status">
                      {t('settings.upToDate')}
                    </p>
                  ) : null}
                  {updateSnap?.lastError ? (
                    <p className="error small">
                      {String(updateSnap.lastError)}
                    </p>
                  ) : null}
                  <p className="muted small">{t('settings.updateHint')}</p>
                  <div className="row settingsInfoDevBtnRow">
                    <button
                      type="button"
                      className="btn"
                      onClick={() =>
                        void (async () => {
                          setError('')
                          try {
                            const u = await CheckForUpdates()
                            setUpdateSnap(u)
                          } catch (e: any) {
                            setError(String(e))
                          }
                        })()
                      }
                    >
                      {t('settings.checkUpdates')}
                    </button>
                    <button
                      type="button"
                      className="btn ghost"
                      onClick={() =>
                        void (async () => {
                          setError('')
                          try {
                            const u = await SetUpdateChannel('stable')
                            setUpdateSnap(u)
                          } catch (e: any) {
                            setError(String(e))
                          }
                        })()
                      }
                    >
                      {t('settings.stableChannel')}
                    </button>
                    {updateSnap?.hasUpdate &&
                    String(updateSnap?.assetDownloadUrl ?? '').trim() ? (
                      <button
                        type="button"
                        className="btn primary"
                        onClick={() =>
                          void (async () => {
                            setError('')
                            try {
                              const ok = window.confirm(
                                'Installer will start now and Sloth Clash will close to avoid file lock conflicts. Continue?',
                              )
                              if (!ok) return
                              await ApplyUpdate()
                              // Give installer process a moment to initialize before we exit.
                              setTimeout(() => {
                                void Quit()
                              }, 350)
                            } catch (e: any) {
                              setError(String(e))
                            }
                            await refresh()
                            const u = await GetUpdateState()
                            setUpdateSnap(u)
                          })()
                        }
                      >
                        {t('settings.downloadInstaller')}
                      </button>
                    ) : null}
                    {String(updateSnap?.releaseUrl ?? '').trim() ? (
                      <button
                        type="button"
                        className="btn ghost"
                        onClick={() =>
                          BrowserOpenURL(String(updateSnap.releaseUrl))
                        }
                      >
                        {t('settings.openReleasePage')}
                      </button>
                    ) : null}
                  </div>
                </div>
              </div>
            </div>
            {tunBanner ? <p className="banner">{tunBanner}</p> : null}
            {error ? <p className="error">{error}</p> : null}
          </div>
        ) : null}
      </section>

      {importModalOpen ? (
        <div
          className="modalOverlay"
          role="presentation"
          onClick={closeImportModal}
        >
          <div
            className="modalCard"
            role="dialog"
            aria-modal="true"
            aria-labelledby="importTitle"
            onClick={(e) => e.stopPropagation()}
          >
            <h3 id="importTitle" className="modalTitle">
              {importModalTitle()}
            </h3>
            <p className="muted small">{importModalBlurb()}</p>

            <label className="field modalField">
              <span className="fieldLab">Subscription URL</span>
              <input
                className="input"
                value={importUrl}
                onChange={(e) => setImportUrl(e.target.value)}
                placeholder="https://…"
              />
            </label>
            <label className="field modalField">
              <span className="fieldLab">
                Display name <span className="optional">(optional)</span>
              </span>
              <input
                className="input"
                value={importName}
                onChange={(e) => setImportName(e.target.value)}
                placeholder="Empty = use Profile-Title from server"
              />
            </label>

            <div className="modalFooter">
              <button
                type="button"
                className="btn btnModalSecondary"
                onClick={() => pasteFromClipboard()}
              >
                Paste URL from clipboard
              </button>
              <div className="modalFooterRight">
                <button
                  type="button"
                  className="btn ghost"
                  disabled={importBusy}
                  onClick={closeImportModal}
                >
                  Cancel
                </button>
                <button
                  type="button"
                  className="btn primary"
                  disabled={importBusy || !importUrl.trim()}
                  onClick={() => performImportAndClose()}
                >
                  Import
                </button>
              </div>
            </div>
          </div>
        </div>
      ) : null}

      {profileMergeModal ? (
        <div
          className="modalOverlay"
          role="presentation"
          style={{ zIndex: 70 }}
          onClick={() => setProfileMergeModal(null)}
        >
          <div
            className="modalCard modalCardWide yamlModalCard vergeModal"
            role="dialog"
            aria-modal="true"
            aria-labelledby="mergeTplTitle"
            onClick={(e) => e.stopPropagation()}
          >
            <div className="vergeModalHead">
              <h3 id="mergeTplTitle" className="modalTitle vergeModalTitle">
                Profile merge template
              </h3>
            </div>
            <p className="muted small yamlModalBlurb">
              Top-level keys merge into the fetched profile;{' '}
              <code className="code">prepend</code> /{' '}
              <code className="code">append</code> /{' '}
              <code className="code">delete</code> for rules, proxy-groups, and
              provider maps. Applied whenever Sloth writes{' '}
              <code className="code">config.yaml</code>.
            </p>
            <label className="field modalField">
              <span className="fieldLab">
                {profileMergeModal.name}{' '}
                <span className="optional">· merge YAML</span>
              </span>
              <textarea
                className="input modalTextarea"
                spellCheck={false}
                value={mergeTemplateDraft}
                onChange={(e) => setMergeTemplateDraft(e.target.value)}
              />
              {mergeTemplateYamlErr ? (
                <span className="muted small" style={{ color: '#ff6b6b' }}>
                  YAML error: {mergeTemplateYamlErr}
                </span>
              ) : null}
            </label>
            <div className="modalFooter">
              <button
                type="button"
                className="btn btnModalSecondary"
                onClick={() => setMergeTemplateDraft(DEFAULT_MERGE_TEMPLATE)}
              >
                Reset scaffold
              </button>
              <div className="modalFooterRight">
                <button
                  type="button"
                  className="btn ghost"
                  onClick={() => setProfileMergeModal(null)}
                >
                  Cancel
                </button>
                <button
                  type="button"
                  className="btn primary"
                  disabled={Boolean(mergeTemplateYamlErr)}
                  onClick={async () => {
                    if (!profileMergeModal) return
                    if (mergeTemplateYamlErr) return
                    setError('')
                    try {
                      await SetProfileMergeTemplate(
                        profileMergeModal.id,
                        mergeTemplateDraft,
                      )
                      setProfileMergeModal(null)
                      setTunBanner(
                        'Merge template saved. Reconnect if you are already connected.',
                      )
                    } catch (e: any) {
                      setError(String(e))
                    }
                    await refresh()
                  }}
                >
                  Save
                </button>
              </div>
            </div>
          </div>
        </div>
      ) : null}

      {profileFileModal ? (
        <div
          className="modalOverlay"
          role="presentation"
          style={{ zIndex: 70 }}
          onClick={() => setProfileFileModal(null)}
        >
          <div
            className="modalCard modalCardWide yamlModalCard vergeModal"
            role="dialog"
            aria-modal="true"
            aria-labelledby="editFileTitle"
            onClick={(e) => e.stopPropagation()}
          >
            <div className="vergeModalHead">
              <h3 id="editFileTitle" className="modalTitle vergeModalTitle">
                Edit configuration file
              </h3>
            </div>
            <p className="muted small mono tight">{profileFilePath}</p>
            {profileFileLoadErr ? (
              <p className="muted small tight">
                Read: {profileFileLoadErr} — you can still paste YAML and save
                to create the file.
              </p>
            ) : null}
            <label className="field modalField">
              <span className="fieldLab">
                {profileFileModal.name}{' '}
                <span className="optional">· loaded config.yaml</span>
              </span>
              <textarea
                className="input modalTextarea"
                spellCheck={false}
                value={profileFileText}
                onChange={(e) => setProfileFileText(e.target.value)}
              />
              {profileFileYamlErr ? (
                <span className="muted small" style={{ color: '#ff6b6b' }}>
                  YAML error: {profileFileYamlErr}
                </span>
              ) : null}
            </label>
            <div className="modalFooter">
              <button
                type="button"
                className="btn btnModalSecondary"
                disabled={!profileFilePath}
                onClick={() => {
                  if (profileFilePath) {
                    void navigator.clipboard.writeText(profileFilePath)
                    setTunBanner('Config path copied.')
                  }
                }}
              >
                Copy path
              </button>
              <div className="modalFooterRight">
                <button
                  type="button"
                  className="btn ghost"
                  onClick={() => setProfileFileModal(null)}
                >
                  Cancel
                </button>
                <button
                  type="button"
                  className="btn primary"
                  disabled={Boolean(profileFileYamlErr)}
                  onClick={async () => {
                    if (!profileFileModal) return
                    if (profileFileYamlErr) return
                    setError('')
                    try {
                      await WriteProfileConfig(
                        profileFileModal.id,
                        profileFileText,
                      )
                      setProfileFileModal(null)
                      setTunBanner(
                        'Config saved. Sloth reapplies ports and secret on connect.',
                      )
                    } catch (e: any) {
                      setError(String(e))
                    }
                    await refresh()
                  }}
                >
                  Save
                </button>
              </div>
            </div>
          </div>
        </div>
      ) : null}

      {profileProxyModal ? (
        <div
          className="modalOverlay"
          role="presentation"
          style={{ zIndex: 70 }}
          onClick={() => setProfileProxyModal(null)}
        >
          <div
            className="modalCard modalCardWide yamlModalCard vergeModal"
            role="dialog"
            aria-modal="true"
            aria-labelledby="pgTitle"
            onClick={(e) => e.stopPropagation()}
          >
            <div className="vergeModalHead">
              <h3 id="pgTitle" className="modalTitle vergeModalTitle">
                Edit proxy groups
              </h3>
              <div className="vergeToggleRow">
                <button
                  type="button"
                  className={`btn vergeToggle ${proxyUiMode === 'visual' ? 'primary' : 'ghost'}`}
                  onClick={() => {
                    if (proxyUiMode === 'advanced' && proxyAdvancedYamlErr) {
                      setError(
                        'Fix proxy advanced YAML before switching to Visual mode.',
                      )
                      return
                    }
                    setProxyUiMode('visual')
                    const b = proxyBucketsFromAdvancedYaml(proxyAdvancedDraft)
                    setProxyRows(b.prepend)
                    setProxyAppendRows(b.append)
                  }}
                >
                  Visual
                </button>
                <button
                  type="button"
                  className={`btn vergeToggle ${proxyUiMode === 'advanced' ? 'primary' : 'ghost'}`}
                  onClick={() => setProxyUiMode('advanced')}
                >
                  Advanced (YAML)
                </button>
              </div>
            </div>
            <div className="vergeSplit">
              <div className="vergePane">
                {proxyUiMode === 'visual' ? (
                  <>
                    <label className="field modalField">
                      <span className="fieldLab">Group name</span>
                      <input
                        className="input"
                        value={pgFormName}
                        onChange={(e) => setPgFormName(e.target.value)}
                        placeholder="MainGroup"
                      />
                    </label>
                    <label className="field modalField">
                      <span className="fieldLab">Group type</span>
                      <select
                        className="selectModern"
                        value={pgFormType}
                        onChange={(e) => setPgFormType(e.target.value)}
                      >
                        <option value="select">select</option>
                        <option value="url-test">url-test</option>
                        <option value="fallback">fallback</option>
                        <option value="load-balance">load-balance</option>
                      </select>
                    </label>
                    <label className="field modalField">
                      <span className="fieldLab">
                        Use providers (comma-separated)
                      </span>
                      <input
                        className="input"
                        value={pgFormUse}
                        onChange={(e) => setPgFormUse(e.target.value)}
                        placeholder="sub1, my-provider"
                      />
                    </label>
                    <label className="field modalField">
                      <span className="fieldLab">Healthcheck URL</span>
                      <input
                        className="input"
                        value={pgFormUrl}
                        onChange={(e) => setPgFormUrl(e.target.value)}
                        placeholder="http://www.gstatic.com/generate_204"
                      />
                    </label>
                    <div className="fieldGrid">
                      <label className="field modalField">
                        <span className="fieldLab">Interval</span>
                        <input
                          className="input"
                          value={pgFormInterval}
                          onChange={(e) => setPgFormInterval(e.target.value)}
                        />
                      </label>
                      <label className="field modalField">
                        <span className="fieldLab">Timeout</span>
                        <input
                          className="input"
                          value={pgFormTimeout}
                          onChange={(e) => setPgFormTimeout(e.target.value)}
                        />
                      </label>
                    </div>
                    <div className="fieldGrid">
                      <label className="field modalField">
                        <span className="fieldLab">Max failed times</span>
                        <input
                          className="input"
                          value={pgFormMaxFailed}
                          onChange={(e) => setPgFormMaxFailed(e.target.value)}
                        />
                      </label>
                      <label className="field modalField">
                        <span className="fieldLab">Lazy</span>
                        <button
                          type="button"
                          className={`trafficKnob ${pgFormLazy ? 'on' : ''}`}
                          onClick={() => setPgFormLazy((v) => !v)}
                        >
                          {pgFormLazy ? 'On' : 'Off'}
                        </button>
                      </label>
                    </div>
                    <button
                      type="button"
                      className="btn primary vergeStackBtn"
                      onClick={() => {
                        const name = pgFormName.trim()
                        if (!name) return
                        const row: ProxyGroupRow = {
                          id: `pg-${Date.now()}`,
                          name,
                          type: pgFormType,
                          use: pgFormUse,
                          url:
                            pgFormUrl.trim() ||
                            'http://www.gstatic.com/generate_204',
                          interval: Number(pgFormInterval || '300'),
                          timeout: Number(pgFormTimeout || '3000'),
                          maxFailedTimes: Number(pgFormMaxFailed || '5'),
                          lazy: pgFormLazy,
                        }
                        const prepNext =
                          proxyTarget === 'prepend'
                            ? [...proxyRows, row]
                            : proxyRows
                        const appNext =
                          proxyTarget === 'append'
                            ? [...proxyAppendRows, row]
                            : proxyAppendRows
                        setProxyRows(prepNext)
                        setProxyAppendRows(appNext)
                        setProxyMergeDraft((prev) =>
                          applyProxyBucketsToMerge(prev, {
                            prepend: prepNext,
                            append: appNext,
                            delete: [],
                          }),
                        )
                        setProxyAdvancedDraft(
                          proxyBucketsToAdvancedYaml({
                            prepend: prepNext,
                            append: appNext,
                            delete: [],
                          }),
                        )
                        setPgFormName('')
                        setPgFormUse('')
                      }}
                    >
                      Add group
                    </button>
                    <div className="segPill">
                      <button
                        type="button"
                        className={`pillOpt ${proxyTarget === 'prepend' ? 'active' : ''}`}
                        onClick={() => setProxyTarget('prepend')}
                      >
                        Prepend
                      </button>
                      <button
                        type="button"
                        className={`pillOpt ${proxyTarget === 'append' ? 'active' : ''}`}
                        onClick={() => setProxyTarget('append')}
                      >
                        Append
                      </button>
                    </div>
                  </>
                ) : (
                  <label className="field modalField">
                    <span className="fieldLab">Advanced YAML</span>
                    <textarea
                      className="input modalTextarea vergePaneYaml"
                      spellCheck={false}
                      value={proxyAdvancedDraft}
                      onChange={(e) => setProxyAdvancedDraft(e.target.value)}
                    />
                    {proxyAdvancedYamlErr ? (
                      <span
                        className="muted small"
                        style={{ color: '#ff6b6b' }}
                      >
                        YAML error: {proxyAdvancedYamlErr}
                      </span>
                    ) : null}
                  </label>
                )}
              </div>
              <div className="vergePane vergePaneList">
                <p className="eyebrow">prepend.proxy-groups</p>
                <div className="vergeScrollList">
                  {proxyRows.length === 0 ? (
                    <p className="muted small">No groups yet.</p>
                  ) : (
                    proxyRows.map((r) => (
                      <div key={r.id} className="vergeCard">
                        <div>
                          <div className="vergeCardTitle">{r.name}</div>
                          <div className="muted small">{r.type}</div>
                          <div className="muted small vergeCardSub">
                            use: {r.use || '—'}
                          </div>
                        </div>
                        <button
                          type="button"
                          className="btn ghost vergeTrash"
                          aria-label="Remove"
                          onClick={() => {
                            const next = proxyRows.filter((x) => x.id !== r.id)
                            setProxyRows(next)
                            const merged = applyProxyBucketsToMerge(
                              proxyMergeDraft,
                              {
                                prepend: next,
                                append: proxyAppendRows,
                                delete: [],
                              },
                            )
                            setProxyMergeDraft(merged)
                            setProxyAdvancedDraft(
                              proxyBucketsToAdvancedYaml({
                                prepend: next,
                                append: proxyAppendRows,
                                delete: [],
                              }),
                            )
                          }}
                        >
                          ×
                        </button>
                      </div>
                    ))
                  )}
                </div>
                <p className="eyebrow" style={{ marginTop: 10 }}>
                  append.proxy-groups
                </p>
                <div className="vergeScrollList">
                  {proxyAppendRows.length === 0 ? (
                    <p className="muted small">No append groups.</p>
                  ) : (
                    proxyAppendRows.map((r) => (
                      <div key={r.id} className="vergeCard">
                        <div>
                          <div className="vergeCardTitle">{r.name}</div>
                          <div className="muted small">{r.type}</div>
                          <div className="muted small vergeCardSub">
                            use: {r.use || '—'}
                          </div>
                        </div>
                        <button
                          type="button"
                          className="btn ghost vergeTrash"
                          aria-label="Remove"
                          onClick={() => {
                            const next = proxyAppendRows.filter(
                              (x) => x.id !== r.id,
                            )
                            setProxyAppendRows(next)
                            const merged = applyProxyBucketsToMerge(
                              proxyMergeDraft,
                              { prepend: proxyRows, append: next, delete: [] },
                            )
                            setProxyMergeDraft(merged)
                            setProxyAdvancedDraft(
                              proxyBucketsToAdvancedYaml({
                                prepend: proxyRows,
                                append: next,
                                delete: [],
                              }),
                            )
                          }}
                        >
                          ×
                        </button>
                      </div>
                    ))
                  )}
                </div>
              </div>
            </div>
            <div className="modalFooter">
              <button
                type="button"
                className="btn ghost"
                onClick={() => setProfileProxyModal(null)}
              >
                Cancel
              </button>
              <div className="modalFooterRight">
                <button
                  type="button"
                  className="btn primary"
                  disabled={
                    proxyUiMode === 'advanced' && Boolean(proxyAdvancedYamlErr)
                  }
                  onClick={async () => {
                    if (!profileProxyModal) return
                    const body =
                      proxyUiMode === 'visual'
                        ? applyProxyBucketsToMerge(proxyMergeDraft, {
                            prepend: proxyRows,
                            append: proxyAppendRows,
                            delete: [],
                          })
                        : (() => {
                            if (proxyAdvancedYamlErr) return null
                            const buckets =
                              proxyBucketsFromAdvancedYaml(proxyAdvancedDraft)
                            return applyProxyBucketsToMerge(
                              proxyMergeDraft,
                              buckets,
                            )
                          })()
                    if (body == null) return
                    setError('')
                    try {
                      await SetProfileProxyTemplate(profileProxyModal.id, body)
                      setProfileProxyModal(null)
                      setTunBanner('Proxy groups saved into merge template.')
                    } catch (e: any) {
                      setError(String(e))
                    }
                    await refresh()
                  }}
                >
                  Save
                </button>
              </div>
            </div>
          </div>
        </div>
      ) : null}

      {profileRulesModal ? (
        <div
          className="modalOverlay"
          role="presentation"
          style={{ zIndex: 70 }}
          onClick={() => setProfileRulesModal(null)}
        >
          <div
            className="modalCard modalCardWide yamlModalCard vergeModal"
            role="dialog"
            aria-modal="true"
            aria-labelledby="rulesEdTitle"
            onClick={(e) => e.stopPropagation()}
          >
            <div className="vergeModalHead">
              <h3 id="rulesEdTitle" className="modalTitle vergeModalTitle">
                Edit rules
              </h3>
              <div className="vergeToggleRow">
                <button
                  type="button"
                  className={`btn vergeToggle ${rulesUiMode === 'visual' ? 'primary' : 'ghost'}`}
                  onClick={() => {
                    setRulesUiMode('visual')
                    const buckets = rulesBucketsFromMerge(rulesMergeDraft)
                    setRuleRows(buckets.prepend)
                    setRuleAppendRows(buckets.append)
                  }}
                >
                  Visual
                </button>
                <button
                  type="button"
                  className={`btn vergeToggle ${rulesUiMode === 'advanced' ? 'primary' : 'ghost'}`}
                  onClick={() => setRulesUiMode('advanced')}
                >
                  Advanced (YAML)
                </button>
              </div>
            </div>
            <div className="vergeSplit">
              <div className="vergePane">
                {rulesUiMode === 'visual' ? (
                  <>
                    <label className="field modalField">
                      <span className="fieldLab">Rule type</span>
                      <select
                        className="selectModern"
                        value={ruleFormType}
                        onChange={(e) => setRuleFormType(e.target.value)}
                      >
                        <option value="DOMAIN-SUFFIX">DOMAIN-SUFFIX</option>
                        <option value="DOMAIN">DOMAIN</option>
                        <option value="DOMAIN-KEYWORD">DOMAIN-KEYWORD</option>
                        <option value="MATCH">MATCH</option>
                        <option value="GEOSITE">GEOSITE</option>
                        <option value="GEOIP">GEOIP</option>
                        <option value="IP-CIDR">IP-CIDR</option>
                      </select>
                    </label>
                    <label className="field modalField">
                      <span className="fieldLab">Rule content</span>
                      <input
                        className="input"
                        value={ruleFormContent}
                        onChange={(e) => setRuleFormContent(e.target.value)}
                        placeholder="google.com"
                      />
                    </label>
                    <label className="field modalField">
                      <span className="fieldLab">Proxy policy</span>
                      <select
                        className="selectModern"
                        value={ruleFormPolicy}
                        onChange={(e) => setRuleFormPolicy(e.target.value)}
                      >
                        {rulePolicyOptions.map((opt) => (
                          <option key={opt} value={opt}>
                            {opt}
                          </option>
                        ))}
                      </select>
                    </label>
                    <div className="vergeRuleActions">
                      <div className="segPill">
                        <button
                          type="button"
                          className={`pillOpt ${ruleTarget === 'prepend' ? 'active' : ''}`}
                          onClick={() => setRuleTarget('prepend')}
                        >
                          Prepend
                        </button>
                        <button
                          type="button"
                          className={`pillOpt ${ruleTarget === 'append' ? 'active' : ''}`}
                          onClick={() => setRuleTarget('append')}
                        >
                          Append
                        </button>
                      </div>
                      <button
                        type="button"
                        className="btn primary vergeStackBtn"
                        onClick={() => {
                          const content = ruleFormContent.trim()
                          if (!content) return
                          const row: RuleRow = {
                            id: `rl-${Date.now()}`,
                            ruleType: ruleFormType,
                            content,
                            policy: ruleFormPolicy.trim() || 'DIRECT',
                          }
                          const prepNext =
                            ruleTarget === 'prepend'
                              ? [row, ...ruleRows]
                              : ruleRows
                          const appNext =
                            ruleTarget === 'append'
                              ? [...ruleAppendRows, row]
                              : ruleAppendRows
                          setRuleRows(prepNext)
                          setRuleAppendRows(appNext)
                          setRulesMergeDraft((prev) =>
                            applyRulesBucketsToMerge(prev, {
                              prepend: prepNext,
                              append: appNext,
                              delete: [],
                            }),
                          )
                          setRulesAdvancedDraft(
                            rulesBucketsToAdvancedYaml({
                              prepend: prepNext,
                              append: appNext,
                              delete: [],
                            }),
                          )
                          setRuleFormContent('')
                        }}
                      >
                        Add rule
                      </button>
                    </div>
                  </>
                ) : (
                  <label className="field modalField">
                    <span className="fieldLab">Advanced YAML</span>
                    <textarea
                      className="input modalTextarea vergePaneYaml"
                      spellCheck={false}
                      value={rulesAdvancedDraft}
                      onChange={(e) => setRulesAdvancedDraft(e.target.value)}
                    />
                    {rulesAdvancedYamlErr ? (
                      <span
                        className="muted small"
                        style={{ color: '#ff6b6b' }}
                      >
                        YAML error: {rulesAdvancedYamlErr}
                      </span>
                    ) : null}
                  </label>
                )}
              </div>
              <div className="vergePane vergePaneList">
                <p className="eyebrow">prepend.rules</p>
                <div className="vergeScrollList">
                  {ruleRows.length === 0 ? (
                    <p className="muted small">No custom prepend rules.</p>
                  ) : (
                    ruleRows.map((r) => (
                      <div key={r.id} className="vergeCard">
                        <div>
                          <div className="vergeCardTitle">{r.ruleType}</div>
                          <div className="muted small">{r.content}</div>
                          <div className="muted small">→ {r.policy}</div>
                        </div>
                        <button
                          type="button"
                          className="btn ghost vergeTrash"
                          aria-label="Remove"
                          onClick={() => {
                            const next = ruleRows.filter((x) => x.id !== r.id)
                            setRuleRows(next)
                            setRulesMergeDraft((prev) =>
                              applyRulesBucketsToMerge(prev, {
                                prepend: next,
                                append: ruleAppendRows,
                                delete: [],
                              }),
                            )
                            setRulesAdvancedDraft(
                              rulesBucketsToAdvancedYaml({
                                prepend: next,
                                append: ruleAppendRows,
                                delete: [],
                              }),
                            )
                          }}
                        >
                          ×
                        </button>
                      </div>
                    ))
                  )}
                </div>
                <p className="eyebrow" style={{ marginTop: 10 }}>
                  append.rules
                </p>
                <div className="vergeScrollList">
                  {ruleAppendRows.length === 0 ? (
                    <p className="muted small">No custom append rules.</p>
                  ) : (
                    ruleAppendRows.map((r) => (
                      <div key={r.id} className="vergeCard">
                        <div>
                          <div className="vergeCardTitle">{r.ruleType}</div>
                          <div className="muted small">{r.content}</div>
                          <div className="muted small">→ {r.policy}</div>
                        </div>
                        <button
                          type="button"
                          className="btn ghost vergeTrash"
                          aria-label="Remove"
                          onClick={() => {
                            const next = ruleAppendRows.filter(
                              (x) => x.id !== r.id,
                            )
                            setRuleAppendRows(next)
                            setRulesMergeDraft((prev) =>
                              applyRulesBucketsToMerge(prev, {
                                prepend: ruleRows,
                                append: next,
                                delete: [],
                              }),
                            )
                            setRulesAdvancedDraft(
                              rulesBucketsToAdvancedYaml({
                                prepend: ruleRows,
                                append: next,
                                delete: [],
                              }),
                            )
                          }}
                        >
                          ×
                        </button>
                      </div>
                    ))
                  )}
                </div>
              </div>
            </div>
            <div className="modalFooter">
              <button
                type="button"
                className="btn ghost"
                onClick={() => setProfileRulesModal(null)}
              >
                Cancel
              </button>
              <div className="modalFooterRight">
                <button
                  type="button"
                  className="btn primary"
                  disabled={
                    rulesUiMode === 'advanced' && Boolean(rulesAdvancedYamlErr)
                  }
                  onClick={() => {
                    if (!profileRulesModal) return
                    if (rulesUiMode === 'advanced' && rulesAdvancedYamlErr)
                      return
                    void (async () => {
                      const body =
                        rulesUiMode === 'visual'
                          ? applyRulesBucketsToMerge(rulesMergeDraft, {
                              prepend: ruleRows,
                              append: ruleAppendRows,
                              delete: [],
                            })
                          : (() => {
                              const buckets =
                                rulesBucketsFromAdvancedYaml(rulesAdvancedDraft)
                              return applyRulesBucketsToMerge(
                                rulesMergeDraft,
                                buckets,
                              )
                            })()
                      setError('')
                      try {
                        await SetProfileRulesTemplate(
                          profileRulesModal.id,
                          body,
                        )
                        await refresh()
                        setProfileRulesModal(null)
                        setTunBanner('Rules saved into merge template.')
                      } catch (e: any) {
                        setError(String(e))
                        await refresh()
                      }
                    })()
                  }}
                >
                  Save
                </button>
              </div>
            </div>
          </div>
        </div>
      ) : null}

      {profileEditInfo ? (
        <div
          className="modalOverlay"
          role="presentation"
          style={{ zIndex: 70 }}
          onClick={() => setProfileEditInfo(null)}
        >
          <div
            className="modalCard"
            role="dialog"
            aria-modal="true"
            aria-labelledby="editInfoTitle"
            onClick={(e) => e.stopPropagation()}
          >
            <h3 id="editInfoTitle" className="modalTitle">
              Edit profile info
            </h3>
            <p className="muted small">
              Display name and subscription link. Leave the URL field empty to
              keep the current link unchanged.
            </p>
            <label className="field modalField">
              <span className="fieldLab">Name</span>
              <input
                className="input"
                value={profileEditName}
                onChange={(e) => setProfileEditName(e.target.value)}
                autoFocus
              />
            </label>
            <label className="field modalField">
              <span className="fieldLab">Subscription URL</span>
              <input
                className="input"
                value={profileEditUrl}
                onChange={(e) => setProfileEditUrl(e.target.value)}
                placeholder="Leave empty to keep current"
              />
            </label>
            <div className="fieldGrid">
              <label className="field modalField">
                <span className="fieldLab">Auto-update</span>
                <button
                  type="button"
                  className={`trafficKnob ${profileEditAutoEnabled ? 'on' : ''}`}
                  onClick={() => setProfileEditAutoEnabled((v) => !v)}
                >
                  {profileEditAutoEnabled ? 'On' : 'Off'}
                </button>
              </label>
              <label className="field modalField">
                <span className="fieldLab">Interval (minutes)</span>
                <input
                  className="input"
                  value={profileEditAutoInterval}
                  onChange={(e) => setProfileEditAutoInterval(e.target.value)}
                  placeholder="360"
                />
              </label>
            </div>
            <div className="modalActions">
              <button
                type="button"
                className="btn btnModalSecondary"
                disabled={!profileEditUrl.trim()}
                onClick={() => {
                  if (profileEditUrl.trim()) {
                    void navigator.clipboard.writeText(profileEditUrl.trim())
                    setTunBanner('Subscription URL copied.')
                  }
                }}
              >
                Copy URL
              </button>
              <button
                type="button"
                className="btn btnModalSecondary"
                disabled={!profileEditName.trim()}
                onClick={() => {
                  void navigator.clipboard.writeText(profileEditName.trim())
                  setTunBanner('Name copied.')
                }}
              >
                Copy name
              </button>
            </div>
            <div className="modalFooter">
              <div className="modalFooterRight" style={{ width: '100%' }}>
                <button
                  type="button"
                  className="btn ghost"
                  onClick={() => setProfileEditInfo(null)}
                >
                  Cancel
                </button>
                <button
                  type="button"
                  className="btn primary"
                  disabled={!profileEditName.trim()}
                  onClick={() => {
                    if (!profileEditInfo) return
                    void run(async () => {
                      await UpdateProfileInfo(
                        profileEditInfo.id,
                        profileEditName.trim(),
                        profileEditUrl.trim(),
                      )
                      const interval = Number(profileEditAutoInterval || '360')
                      await SetProfileAutoUpdate(
                        profileEditInfo.id,
                        profileEditAutoEnabled,
                        Number.isFinite(interval) && interval > 0
                          ? interval
                          : 360,
                      )
                    })
                    setProfileEditInfo(null)
                  }}
                >
                  Save
                </button>
              </div>
            </div>
          </div>
        </div>
      ) : null}

      {deleteProfileModal ? (
        <div
          className="modalOverlay"
          role="presentation"
          style={{ zIndex: 72 }}
          onClick={() => setDeleteProfileModal(null)}
        >
          <div
            className="modalCard"
            role="dialog"
            aria-modal="true"
            aria-labelledby="deleteProfileTitle"
            onClick={(e) => e.stopPropagation()}
          >
            <h3 id="deleteProfileTitle" className="modalTitle">
              Delete profile
            </h3>
            <p className="muted small">
              Remove <strong>{deleteProfileModal.name}</strong> from this
              device? This does not cancel a remote subscription.
            </p>
            <div className="modalFooter">
              <div className="modalFooterRight" style={{ width: '100%' }}>
                <button
                  type="button"
                  className="btn ghost"
                  onClick={() => setDeleteProfileModal(null)}
                >
                  Cancel
                </button>
                <button
                  type="button"
                  className="btn primary"
                  onClick={() => {
                    const id = deleteProfileModal.id
                    setDeleteProfileModal(null)
                    void run(() => DeleteProfile(id))
                  }}
                >
                  Delete
                </button>
              </div>
            </div>
          </div>
        </div>
      ) : null}

      {settingsResetModal ? (
        <div
          className="modalOverlay"
          role="presentation"
          style={{ zIndex: 72 }}
          onClick={() => setSettingsResetModal(null)}
        >
          <div
            className="modalCard"
            role="dialog"
            aria-modal="true"
            aria-labelledby="resetSettingsTitle"
            onClick={(e) => e.stopPropagation()}
          >
            <h3 id="resetSettingsTitle" className="modalTitle">
              Reset app settings
            </h3>
            <p className="muted small">
              {settingsResetModal === 'with_profiles'
                ? 'Reset settings and delete all profiles from this device?'
                : 'Reset UI settings and local defaults, but keep profiles?'}
            </p>
            <div className="modalFooter">
              <div className="modalFooterRight" style={{ width: '100%' }}>
                <button
                  type="button"
                  className="btn ghost"
                  onClick={() => setSettingsResetModal(null)}
                >
                  Cancel
                </button>
                <button
                  type="button"
                  className="btn primary"
                  onClick={() =>
                    void resetAppSettings(
                      settingsResetModal === 'with_profiles',
                    )
                  }
                >
                  Reset
                </button>
              </div>
            </div>
          </div>
        </div>
      ) : null}

      {profileMenu ? (
        <div
          className="ctxMenu"
          style={{ left: profileMenu.x, top: profileMenu.y }}
          onClick={(e) => e.stopPropagation()}
        >
          <div className="ctxTitle">{profileMenu.name}</div>
          <button
            type="button"
            className="ctxItem"
            disabled={
              !state?.profile?.profiles?.find(
                (x: any) => x.id === profileMenu.id,
              )?.url
            }
            onClick={() => {
              const id = profileMenu.id
              setProfileMenu(null)
              void run(() => RefreshProfileSubscription(id))
            }}
          >
            Update profile now
          </button>
          <button
            type="button"
            className="ctxItem"
            onClick={() => {
              const id = profileMenu.id
              const name = profileMenu.name
              const p = state?.profile?.profiles?.find((x: any) => x.id === id)
              setProfileMenu(null)
              setProfileEditInfo({
                id,
                name,
                url: String(p?.url ?? ''),
              })
            }}
          >
            Edit info
          </button>
          <button
            type="button"
            className="ctxItem"
            disabled={
              !state?.profile?.profiles?.find(
                (x: any) => x.id === profileMenu.id,
              )?.url
            }
            onClick={() => {
              const p = state?.profile?.profiles?.find(
                (x: any) => x.id === profileMenu.id,
              )
              if (p?.url) {
                void navigator.clipboard.writeText(p.url)
                setTunBanner('Subscription URL copied.')
              }
              setProfileMenu(null)
            }}
          >
            Copy subscription URL
          </button>
          <button
            type="button"
            className="ctxItem"
            onClick={() => {
              const id = profileMenu.id
              const name = profileMenu.name
              setProfileMenu(null)
              setProfileRulesModal({ id, name })
            }}
          >
            Rules
          </button>
          <button
            type="button"
            className="ctxItem"
            onClick={() => {
              const id = profileMenu.id
              const p = state?.profile?.profiles?.find((x: any) => x.id === id)
              setProfileMenu(null)
              setDeleteProfileModal({
                id,
                name: String(p?.name ?? id),
              })
            }}
          >
            Delete profile
          </button>
          <div className="ctxSection">Advanced</div>
          <button
            type="button"
            className="ctxItem ctxItemSub"
            onClick={() => {
              const id = profileMenu.id
              const name = profileMenu.name
              setProfileMenu(null)
              setProfileMergeModal({ id, name })
            }}
          >
            Extend config
          </button>
          <button
            type="button"
            className="ctxItem ctxItemSub"
            onClick={() => {
              const id = profileMenu.id
              const name = profileMenu.name
              setProfileMenu(null)
              setProfileProxyModal({ id, name })
            }}
          >
            Proxy groups
          </button>
          <button
            type="button"
            className="ctxItem ctxItemSub"
            onClick={() => {
              const id = profileMenu.id
              const name = profileMenu.name
              setProfileMenu(null)
              setProfileFileModal({ id, name })
            }}
          >
            Edit file
          </button>
        </div>
      ) : null}

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
    </div>
  )
}

export default App
