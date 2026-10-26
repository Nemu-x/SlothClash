import type { TFunction } from 'i18next'

import type {
  DesyncAutoService,
  DesyncAutoStatus,
  DesyncCustomEntry,
  DesyncPreset,
  DesyncStatus,
} from '../api/desync'

/** Status + automatic calibration state, polled together (useDesyncStatus). */
export type DesyncSnapshot = {
  status: DesyncStatus
  automatic: DesyncAutoStatus
}

/** Visual tone of a status chip / Home mark. */
export type DesyncTone = 'ok' | 'pending' | 'problem' | 'off'

export type DesyncServiceKey = 'youtube' | 'discord'

/** One line of service status: the `desync.phase.*` key plus its params. */
export type DesyncPhaseView = {
  phase: string
  tone: DesyncTone
  attempt: number
  total: number
}

export type DesyncServiceView = {
  key: DesyncServiceKey
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
  profile: 'ok',
  transport: 'ok',
  receiving: 'ok',
  media: 'ok',
  // Custom domain reachable without Desync (routed directly).
  not_needed: 'ok',
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
  }
}

/**
 * Per-service rows exactly as the Desync card has always computed them
 * (toggle semantics, gates and phase precedence), shared with the Home chip.
 */
export function desyncServiceViews(
  snapshot: DesyncSnapshot,
  ctx: DesyncContext,
): DesyncServiceView[] {
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
export function desyncServiceTone(view: DesyncServiceView): DesyncTone {
  const base = view.status.tone
  const voice = view.voice?.tone
  if (!voice || voice === 'off' || base === 'off') return base
  return TONE_RANK[voice] > TONE_RANK[base] ? voice : base
}

export type DesyncCustomView = {
  id: string
  domain: string
  /** The entry switch as the user sees it. */
  checked: boolean
  status: DesyncPhaseView
}

/** A custom entry's status line, gated like the built-in services. */
function customEntryStatus(
  snapshot: DesyncSnapshot,
  ctx: DesyncContext,
  entry: DesyncCustomEntry | undefined,
): DesyncPhaseView {
  const state = entry ? snapshot.automatic?.custom?.[entry.id] : undefined
  return phaseView(
    snapshot.status.settings.enabled !== true || !entry?.enabled
      ? 'disabled'
      : !ctx.connected
        ? 'connect'
        : ctx.mode.toLowerCase() !== 'rule'
          ? 'rule'
          : state?.phase || 'queued',
    state,
  )
}

/**
 * One row per plain custom-domain entry (preset entries are rows of
 * desyncPresetViews), gated like the built-in services. The calibration
 * state comes from automatic.custom[id].
 */
export function desyncCustomViews(
  snapshot: DesyncSnapshot,
  ctx: DesyncContext,
): DesyncCustomView[] {
  const entries: DesyncCustomEntry[] = snapshot.status.settings.custom ?? []
  return entries
    .filter((e) => !e.preset)
    .map((e) => ({
      id: e.id,
      domain: e.domain,
      checked: e.enabled,
      status: customEntryStatus(snapshot, ctx, e),
    }))
}

export type DesyncPresetView = {
  /** Catalog preset ID. */
  id: string
  name: string
  domains: string[]
  /** The stored entry's ID; null while the preset was never switched on. */
  entryId: string | null
  /** The preset switch as the user sees it. */
  checked: boolean
  status: DesyncPhaseView
  /**
   * Domains this switched-on preset routes on behalf of a switched-off one
   * listed first in the catalog (Instagram carrying Facebook's fbcdn.net),
   * with that preset's name; null when there are none.
   */
  shared: { domains: string[]; names: string[] } | null
}

/**
 * One row per catalog preset (catalog order), then one per stored preset
 * entry the catalog does not list (named by its ID, e.g. while the catalog
 * has not loaded). Shared domains follow the backend's ownership rule
 * (desync.PresetOwnedDomains): a domain listed by two presets belongs to the
 * first switched-on one in catalog order.
 */
export function desyncPresetViews(
  snapshot: DesyncSnapshot,
  ctx: DesyncContext,
  catalog: DesyncPreset[],
): DesyncPresetView[] {
  const entries = (snapshot.status.settings.custom ?? []).filter(
    (e) => e.preset,
  )
  const entryOf = (id: string) => entries.find((e) => e.preset === id)
  const presets = [
    ...catalog,
    ...entries
      .filter((e) => !catalog.some((p) => p.id === e.preset))
      .map((e) => ({
        id: String(e.preset),
        name: String(e.preset),
        domains: [],
      })),
  ]
  const on = (id: string) => entryOf(id)?.enabled === true
  return presets.map((p, index) => {
    const entry = entryOf(p.id)
    const checked = entry?.enabled === true
    const shared = new Map<string, string[]>() // domain -> earlier listers
    if (checked) {
      for (const d of p.domains) {
        const earlier = presets
          .slice(0, index)
          .filter((o) => o.domains.includes(d))
        // Another switched-on preset earlier in the catalog routes it.
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

/** Mirrors desyncCustomLimit (desync_custom.go). */
export const DESYNC_CUSTOM_LIMIT = 32

const CUSTOM_ERROR_CODES = new Set([
  'empty',
  'invalid',
  'ip',
  'wildcard',
  'public_suffix',
  'builtin',
  'duplicate',
  'limit',
  'not_found',
  'preset_overlap',
  'unknown_preset',
])

/** Codes worded for a preset switch rather than a typed domain. */
const PRESET_ERROR_CODES = new Set([
  'duplicate',
  'limit',
  'not_found',
  'unknown_preset',
])

/**
 * The backend rejects a custom entry with "custom domain: <code>" (or
 * "custom domain: preset_overlap: <preset id>"); show the translated reason,
 * or the raw error for anything else. `presetName` names a preset ID;
 * `forPreset` words the reason for a preset switch.
 */
export function customDomainErrorText(
  t: TFunction,
  err: unknown,
  {
    presetName = (id: string) => id,
    forPreset = false,
  }: { presetName?: (id: string) => string; forPreset?: boolean } = {},
): string {
  const raw = String(err)
  const m = /custom domain: ([a-z_]+)(?:: ([a-z0-9_-]+))?/.exec(raw)
  const code = m?.[1]
  if (!code || !CUSTOM_ERROR_CODES.has(code)) return raw
  const scope = forPreset && PRESET_ERROR_CODES.has(code) ? 'presets' : 'custom'
  return t(`desync.${scope}.error.${code}`, {
    limit: DESYNC_CUSTOM_LIMIT,
    name: presetName(m?.[2] ?? ''),
  })
}

/** A preset's display name from the catalog, or its ID. */
export function desyncPresetName(catalog: DesyncPreset[], id: string): string {
  return catalog.find((p) => p.id === id)?.name ?? id
}

/**
 * Mirrors desyncHasSelection (desync_custom.go): the backend refuses to
 * enable Desync without a service or a switched-on custom entry (a domain or
 * a preset).
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
 * Short names of what Desync covers: the services, the switched-on presets
 * by name (catalog order), then the plain domains as "Sites N".
 */
export function desyncSelectionNames(
  t: TFunction,
  settings: DesyncStatus['settings'],
  catalog: DesyncPreset[] = [],
): string[] {
  const names: string[] = []
  if (settings.youtube) names.push('YouTube')
  if (settings.discord || settings.discordUDP) names.push('Discord')
  const on = (settings.custom ?? []).filter((e) => e.enabled)
  const presets = on.filter((e) => e.preset).map((e) => String(e.preset))
  const rank = (id: string) => {
    const i = catalog.findIndex((p) => p.id === id)
    return i < 0 ? catalog.length : i
  }
  presets.sort((a, b) => rank(a) - rank(b))
  for (const id of presets) names.push(desyncPresetName(catalog, id))
  const sites = on.filter((e) => !e.preset).length
  if (sites > 0) names.push(t('desync.custom.homeItem', { count: sites }))
  return names
}
