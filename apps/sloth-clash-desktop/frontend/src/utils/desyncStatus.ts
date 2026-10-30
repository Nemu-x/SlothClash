import type { TFunction } from 'i18next'

import type {
  DesyncAutoService,
  DesyncAutoStatus,
  DesyncCustomEntry,
  DesyncRuleKind,
  DesyncRuleRoute,
  DesyncService,
  DesyncStatus,
} from '../api/desync'

/** Status + automatic calibration state, polled together (useDesyncStatus). */
export type DesyncSnapshot = {
  status: DesyncStatus
  automatic: DesyncAutoStatus
}

/** Visual tone of a status chip / Home mark. */
export type DesyncTone = 'ok' | 'pending' | 'problem' | 'off'

export type DesyncBuiltinKey = 'youtube' | 'discord'

/** One line of service status: the `desync.phase.*` key plus its params. */
export type DesyncPhaseView = {
  phase: string
  tone: DesyncTone
  attempt: number
  total: number
  /**
   * The strategy calibration reports for this line (a catalog ID, "direct"
   * or "profile"); meaningful only in the phases desyncStrategyLabel shows.
   */
  candidate?: string
}

export type DesyncBuiltinView = {
  key: DesyncBuiltinKey
  name: string
  /** The service toggle as the user sees it. */
  checked: boolean
  /** Whether turning the service ON is possible on this build / platform. */
  supported: boolean
  /** Settings patch the toggle applies. */
  patch: Partial<DesyncStatus['settings']>
  status: DesyncPhaseView
  /** Discord only: voice (UDP) sub-status; null when it is not shown. */
  voice: DesyncPhaseView | null
}

export type DesyncContext = {
  connected: boolean
  mode: string
  traffic: string
}

/**
 * Phase → tone. Every phase the backend emits (desync_auto.go,
 * desync_reserve.go, desync_voice_monitor.go) plus the UI-side gates
 * (disabled / connect / rule / needs_tun / queued / waiting). Unknown
 * phases fall back to 'pending': still shown verbatim-keyed, never as ok.
 *
 * 'problem' (red) is only for a service that definitely does not work and
 * that the app cannot fix; a state it is still recovering from is 'pending'.
 */
const PHASE_TONE: Record<string, DesyncTone> = {
  // Working.
  ready: 'ok',
  transport: 'ok',
  receiving: 'ok',
  media: 'ok',
  // Custom domain reachable without Desync (routed directly).
  not_needed: 'ok',
  // An app rule through Desync: the catalog default strategy, applied
  // (apps cannot be probed).
  fixed: 'ok',
  // A rule sending its traffic to the VPN or directly: applied as is.
  route_vpn: 'ok',
  route_direct: 'ok',
  // Desync did not work here: the service goes through the VPN (the
  // subscription reserve) while the search goes on. It works, but not by
  // Desync, so it is not shown as a success.
  profile: 'pending',
  // In progress / waiting for traffic.
  queued: 'pending',
  testing: 'pending',
  checking: 'pending',
  waiting: 'pending',
  observing: 'pending',
  partial: 'pending',
  // Degraded but recovering: the reserve's latest check failed while
  // background recovery continues; voice traffic stalled and is waiting for
  // recovery or a strategy trial.
  profile_failed: 'pending',
  interrupted: 'pending',
  // Broken: strategy search and reserve both failed.
  failed: 'problem',
  // Custom domain: no strategy works (likely blocked by IP); the profile
  // carries it.
  unavailable: 'problem',
  // Desync-only profile (no reserve): no strategy works, the route goes
  // direct while the search repeats. Nothing else carries the service, so it
  // does not work: red.
  no_strategy: 'problem',
  // Desync-only profile: "failed" there has no reserve to mention.
  failed_direct: 'problem',
  // Not active (switched off, or a precondition the UI explains).
  disabled: 'off',
  connect: 'off',
  rule: 'off',
  needs_tun: 'off',
}

export function desyncPhaseTone(phase: string): DesyncTone {
  return PHASE_TONE[phase] ?? 'pending'
}

/**
 * The phase key a line shows. A Desync-only profile has no subscription
 * reserve, so the generic failure is worded without one.
 */
function shownPhase(phase: string, desyncOnly: boolean): string {
  return desyncOnly && phase === 'failed' ? 'failed_direct' : phase
}

function phaseView(phase: string, state?: DesyncAutoService): DesyncPhaseView {
  return {
    phase,
    tone: desyncPhaseTone(phase),
    attempt: state?.attempt || 1,
    total: state?.total || 1,
    ...(state?.candidate ? { candidate: state.candidate } : {}),
  }
}

/**
 * Per-service rows exactly as the Desync card has always computed them
 * (toggle semantics, gates and phase precedence), shared with the Home chip.
 */
export function desyncBuiltinViews(
  snapshot: DesyncSnapshot,
  ctx: DesyncContext,
): DesyncBuiltinView[] {
  const { status, automatic } = snapshot
  const s = status.settings
  const enabled = s.enabled === true
  const ruleMode = ctx.mode.toLowerCase() === 'rule'
  const supported = status.coreSupported === true
  const discordOn = s.discord || s.discordUDP
  const desyncOnly = status.desyncOnly === true

  const gate = (checked: boolean, state?: DesyncAutoService) =>
    phaseView(
      !enabled || !checked
        ? 'disabled'
        : !ctx.connected
          ? 'connect'
          : !ruleMode
            ? 'rule'
            : shownPhase(state?.phase || 'queued', desyncOnly),
      state,
    )

  return [
    {
      key: 'youtube',
      name: 'YouTube',
      checked: s.youtube,
      supported,
      patch: { youtube: !s.youtube },
      status: gate(s.youtube, automatic?.youtube),
      voice: null,
    },
    {
      key: 'discord',
      name: 'Discord',
      checked: discordOn,
      supported: supported && status.platform === 'windows',
      patch: {
        discord: !discordOn,
        discordUDP: !discordOn && status.udpCoreSupported,
      },
      status: gate(discordOn, automatic?.discord),
      voice:
        enabled && discordOn
          ? phaseView(
              !s.discordUDP
                ? 'disabled'
                : !ctx.connected
                  ? 'connect'
                  : !ruleMode
                    ? 'rule'
                    : ctx.traffic !== 'tun'
                      ? 'needs_tun'
                      : shownPhase(
                          automatic?.voice?.phase || 'waiting',
                          desyncOnly,
                        ),
              automatic?.voice,
            )
          : null,
    },
  ]
}

const TONE_RANK: Record<DesyncTone, number> = {
  off: 0,
  ok: 1,
  pending: 2,
  problem: 3,
}

/**
 * The single tone a service shows on Home: its HTTPS status, escalated by a
 * worse voice status. A voice line that is merely inactive (off, e.g. "enable
 * TUN for voice") never downgrades a working service.
 */
export function desyncBuiltinTone(view: DesyncBuiltinView): DesyncTone {
  const base = view.status.tone
  const voice = view.voice?.tone
  if (!voice || voice === 'off' || base === 'off') return base
  return TONE_RANK[voice] > TONE_RANK[base] ? voice : base
}

export type DesyncCustomView = {
  id: string
  /** What the rule matches, as shown: its domain or process name. */
  domain: string
  kind: DesyncRuleKind
  route: DesyncRuleRoute
  /** The entry switch as the user sees it. */
  checked: boolean
  status: DesyncPhaseView
}

const RULE_KINDS: readonly string[] = ['suffix', 'domain', 'process']
const RULE_ROUTES: readonly string[] = ['desync', 'vpn', 'direct']

/** A rule's kind; an entry stored before kinds is a suffix. */
function ruleKind(e: DesyncCustomEntry): DesyncRuleKind {
  return RULE_KINDS.includes(e.kind ?? '')
    ? (e.kind as DesyncRuleKind)
    : 'suffix'
}

/** A rule's route; an entry stored before routes, and a service, use Desync. */
function ruleRoute(e: DesyncCustomEntry): DesyncRuleRoute {
  return !e.service && RULE_ROUTES.includes(e.route ?? '')
    ? (e.route as DesyncRuleRoute)
    : 'desync'
}

/**
 * A custom entry's status line, gated like the built-in services. A rule to
 * the VPN or direct has no calibration: once active it shows its route.
 */
function customEntryStatus(
  snapshot: DesyncSnapshot,
  ctx: DesyncContext,
  entry: DesyncCustomEntry | undefined,
): DesyncPhaseView {
  const route = entry ? ruleRoute(entry) : 'desync'
  const state =
    entry && route === 'desync'
      ? snapshot.automatic?.custom?.[entry.id]
      : undefined
  return phaseView(
    snapshot.status.settings.enabled !== true || !entry?.enabled
      ? 'disabled'
      : !ctx.connected
        ? 'connect'
        : ctx.mode.toLowerCase() !== 'rule'
          ? 'rule'
          : route !== 'desync'
            ? `route_${route}`
            : state?.phase || 'queued',
    state,
  )
}

/**
 * One row per user rule ("My rules"; service entries are rows of
 * desyncServiceViews), gated like the built-in services. The calibration
 * state of a rule through Desync comes from automatic.custom[id]. An entry
 * stored before rules had a kind and a route reads as a suffix through
 * Desync, as the backend reads it.
 */
export function desyncCustomViews(
  snapshot: DesyncSnapshot,
  ctx: DesyncContext,
): DesyncCustomView[] {
  const entries: DesyncCustomEntry[] = snapshot.status.settings.custom ?? []
  return entries
    .filter((e) => !e.service)
    .map((e) => ({
      id: e.id,
      domain: (e.kind === 'process' ? e.process : e.domain) ?? '',
      kind: ruleKind(e),
      route: ruleRoute(e),
      checked: e.enabled,
      status: customEntryStatus(snapshot, ctx, e),
    }))
}

export type DesyncServiceView = {
  /** Catalog service ID. */
  id: string
  name: string
  domains: string[]
  /** The stored entry's ID; null while the service was never switched on. */
  entryId: string | null
  /** The service switch as the user sees it. */
  checked: boolean
  status: DesyncPhaseView
  /**
   * Domains this switched-on service routes on behalf of a switched-off one
   * listed first in the catalog (Instagram carrying Facebook's fbcdn.net),
   * with that service's name; null when there are none.
   */
  shared: { domains: string[]; names: string[] } | null
}

/**
 * One row per catalog service (catalog order), then one per stored service
 * entry the catalog does not list (named by its ID, e.g. while the catalog
 * has not loaded). Shared domains follow the backend's ownership rule
 * (desync.ServiceOwnedDomains): a domain listed by two services belongs to the
 * first switched-on one in catalog order.
 */
export function desyncServiceViews(
  snapshot: DesyncSnapshot,
  ctx: DesyncContext,
  catalog: DesyncService[],
): DesyncServiceView[] {
  const entries = (snapshot.status.settings.custom ?? []).filter(
    (e) => e.service,
  )
  const entryOf = (id: string) => entries.find((e) => e.service === id)
  const services = [
    ...catalog,
    ...entries
      .filter((e) => !catalog.some((p) => p.id === e.service))
      .map((e) => ({
        id: String(e.service),
        name: String(e.service),
        domains: [],
      })),
  ]
  const on = (id: string) => entryOf(id)?.enabled === true
  return services.map((p, index) => {
    const entry = entryOf(p.id)
    const checked = entry?.enabled === true
    const shared = new Map<string, string[]>() // domain -> earlier listers
    if (checked) {
      for (const d of p.domains) {
        const earlier = services
          .slice(0, index)
          .filter((o) => o.domains.includes(d))
        // Another switched-on service earlier in the catalog routes it.
        if (earlier.some((o) => on(o.id)) || earlier.length === 0) continue
        shared.set(
          d,
          earlier.map((o) => o.name),
        )
      }
    }
    const names = [...new Set([...shared.values()].flat())]
    return {
      id: p.id,
      name: p.name,
      domains: p.domains,
      entryId: entry?.id ?? null,
      checked,
      status: customEntryStatus(snapshot, ctx, entry),
      shared: shared.size > 0 ? { domains: [...shared.keys()], names } : null,
    }
  })
}

/**
 * The single tone of all switched-on custom entries on Home. Red only when
 * Desync can unblock none of them; a mix of working and unavailable entries
 * is a partial state (pending), never a problem.
 */
export function desyncCustomTone(views: DesyncCustomView[]): DesyncTone {
  const tones = views.filter((v) => v.checked).map((v) => v.status.tone)
  if (tones.length === 0) return 'off'
  if (tones.every((t) => t === 'off')) return 'off'
  const active = tones.filter((t) => t !== 'off')
  if (active.every((t) => t === 'problem')) return 'problem'
  if (active.every((t) => t === 'ok')) return 'ok'
  return 'pending'
}

/** Mirrors desyncCustomLimit (desync_custom.go): entries through Desync. */
export const DESYNC_CUSTOM_LIMIT = 32
/** Mirrors desyncRuleLimit (desync_custom.go): every entry. */
export const DESYNC_RULE_LIMIT = 128

const CUSTOM_ERROR_CODES = new Set([
  'empty',
  'invalid',
  'ip',
  'wildcard',
  'public_suffix',
  'builtin',
  'duplicate',
  'limit',
  'rule_limit',
  'not_found',
  'service_overlap',
  'unknown_service',
  'invalid_process',
  'invalid_kind',
  'invalid_route',
  'no_vpn',
])

/** Codes worded for a service switch rather than a typed domain. */
const SERVICE_ERROR_CODES = new Set([
  'duplicate',
  'limit',
  'not_found',
  'unknown_service',
])

/** Codes worded for a process name rather than a domain. */
const PROCESS_ERROR_CODES = new Set(['empty', 'wildcard', 'duplicate'])

/** Settings failures (desync_settings.go), worded under desync.error.*. */
const SETTINGS_ERROR_CODES = new Set([
  'unsupported_core',
  'unsupported_udp_core',
  'no_selection',
  'discord_platform',
  'no_profile',
])

/**
 * The backend rejects a Desync change with "desync: <code>" (or
 * "desync: service_overlap: <service id>"); show the translated reason, or
 * the raw error for anything else. `serviceName` names a service ID;
 * `forService` words the reason for a popular-service switch, `forProcess`
 * for an app rule's process name.
 */
export function desyncErrorText(
  t: TFunction,
  err: unknown,
  {
    serviceName = (id: string) => id,
    forService = false,
    forProcess = false,
  }: {
    serviceName?: (id: string) => string
    forService?: boolean
    forProcess?: boolean
  } = {},
): string {
  const raw = String(err)
  const m = /desync: ([a-z_]+)(?:: ([a-z0-9_-]+))?/.exec(raw)
  const code = m?.[1]
  if (code && SETTINGS_ERROR_CODES.has(code)) return t(`desync.error.${code}`)
  if (!code || !CUSTOM_ERROR_CODES.has(code)) return raw
  const scope =
    forService && SERVICE_ERROR_CODES.has(code) ? 'services' : 'custom'
  const key =
    forProcess && PROCESS_ERROR_CODES.has(code) ? `process_${code}` : code
  return t(`desync.${scope}.error.${key}`, {
    limit: code === 'rule_limit' ? DESYNC_RULE_LIMIT : DESYNC_CUSTOM_LIMIT,
    name: serviceName(m?.[2] ?? ''),
  })
}

/** A service's display name from the catalog, or its ID. */
export function desyncServiceName(
  catalog: DesyncService[],
  id: string,
): string {
  return catalog.find((p) => p.id === id)?.name ?? id
}

/**
 * Mirrors desyncHasSelection (desync_custom.go): the backend refuses to
 * enable Desync without a service or a switched-on custom entry (a domain or
 * a service).
 */
export function desyncHasSelection(
  settings: DesyncStatus['settings'],
): boolean {
  if (settings.youtube || settings.discord || settings.discordUDP) return true
  return (settings.custom ?? []).some((e) => e.enabled)
}

/**
 * The settings patch that switches Desync on, exactly as the master switch
 * applies it: a selected Discord gets voice (UDP) whenever the core has it.
 */
export function desyncEnablePatch(
  status: DesyncStatus,
): Partial<DesyncStatus['settings']> {
  const s = status.settings
  return {
    enabled: true,
    ...(s.discord || s.discordUDP
      ? { discord: true, discordUDP: status.udpCoreSupported }
      : {}),
  }
}

/**
 * Short names of what Desync covers: the services, the switched-on services
 * by name (catalog order), then the switched-on rules as "My rules N".
 */
export function desyncSelectionNames(
  t: TFunction,
  settings: DesyncStatus['settings'],
  catalog: DesyncService[] = [],
): string[] {
  const names: string[] = []
  if (settings.youtube) names.push('YouTube')
  if (settings.discord || settings.discordUDP) names.push('Discord')
  const on = (settings.custom ?? []).filter((e) => e.enabled)
  const services = on.filter((e) => e.service).map((e) => String(e.service))
  const rank = (id: string) => {
    const i = catalog.findIndex((p) => p.id === id)
    return i < 0 ? catalog.length : i
  }
  services.sort((a, b) => rank(a) - rank(b))
  for (const id of services) names.push(desyncServiceName(catalog, id))
  const sites = on.filter((e) => !e.service).length
  if (sites > 0) names.push(t('desync.custom.homeItem', { count: sites }))
  return names
}
