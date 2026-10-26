import type { TFunction } from 'i18next'

import type {
  DesyncAutoService,
  DesyncAutoStatus,
  DesyncCustomEntry,
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
  // Broken.
  failed: 'problem',
  profile_failed: 'problem',
  interrupted: 'problem',
  // Custom domain: no strategy works (likely blocked by IP); the profile
  // carries it.
  unavailable: 'problem',
  // Desync-only profile (no reserve): no strategy works, the route goes
  // direct while the search repeats.
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

/**
 * One row per custom-domain entry, gated like the built-in services. The
 * calibration state comes from automatic.custom[id].
 */
export function desyncCustomViews(
  snapshot: DesyncSnapshot,
  ctx: DesyncContext,
): DesyncCustomView[] {
  const { status, automatic } = snapshot
  const enabled = status.settings.enabled === true
  const ruleMode = ctx.mode.toLowerCase() === 'rule'
  const entries: DesyncCustomEntry[] = status.settings.custom ?? []
  return entries.map((e) => {
    const state = automatic?.custom?.[e.id]
    return {
      id: e.id,
      domain: e.domain,
      checked: e.enabled,
      status: phaseView(
        !enabled || !e.enabled
          ? 'disabled'
          : !ctx.connected
            ? 'connect'
            : !ruleMode
              ? 'rule'
              : state?.phase || 'queued',
        state,
      ),
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
])

/**
 * The backend rejects a custom domain with "custom domain: <code>"; show the
 * translated reason, or the raw error for anything else.
 */
export function customDomainErrorText(t: TFunction, err: unknown): string {
  const raw = String(err)
  const code = /custom domain: ([a-z_]+)/.exec(raw)?.[1]
  return code && CUSTOM_ERROR_CODES.has(code)
    ? t(`desync.custom.error.${code}`, { limit: DESYNC_CUSTOM_LIMIT })
    : raw
}
