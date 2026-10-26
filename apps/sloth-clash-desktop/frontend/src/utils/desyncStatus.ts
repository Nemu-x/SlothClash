import type {
  DesyncAutoService,
  DesyncAutoStatus,
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
  // Not active (switched off, or a precondition the UI explains).
  disabled: 'off',
  connect: 'off',
  rule: 'off',
  needs_tun: 'off',
}

export function desyncPhaseTone(phase: string): DesyncTone {
  return PHASE_TONE[phase] ?? 'pending'
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

  const gate = (checked: boolean, state?: DesyncAutoService) =>
    phaseView(
      !enabled || !checked
        ? 'disabled'
        : !ctx.connected
          ? 'connect'
          : !ruleMode
            ? 'rule'
            : state?.phase || 'queued',
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
                      : automatic?.voice?.phase || 'waiting',
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
