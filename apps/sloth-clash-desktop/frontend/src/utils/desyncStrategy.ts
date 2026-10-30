import type { TFunction } from 'i18next'

import type { DesyncStrategy } from '../api/desync'

import type { DesyncPhaseView } from './desyncStatus'

/**
 * Phases in which calibration has applied a strategy (the candidate is the
 * one in use): HTTPS ready, and Discord voice traffic flowing on it. Other
 * phases either still search (the chip says so), carry the route another
 * way (VPN reserve, direct) or are not active.
 */
const APPLIED_PHASES = new Set([
  'ready',
  'transport',
  'media',
  'receiving',
  'partial',
])

/** Strategy kinds with a plain-words phrase (desync.strategy.kind.*). */
export const DESYNC_STRATEGY_KINDS = [
  'tlsrec',
  'tlsrec-random',
  'split',
  'split-random',
  'tlsrec-split',
  'split-wait',
  'disorder',
  'oob',
  'disorder-oob',
  'fake',
  'fake-split',
  'udp-zero',
  'udp-quic',
] as const

const KNOWN_KINDS = new Set<string>(DESYNC_STRATEGY_KINDS)

/** Methods with a plain-words phrase (desync.caps.method.*). */
export const DESYNC_METHODS = [
  'tlsrec',
  'split',
  'split-wait-sent',
  'split-wait-acked',
  'disorder',
  'oob',
  'fake-tcp',
  'fake-udp',
] as const

const KNOWN_METHODS = new Set<string>(DESYNC_METHODS)

/** A method's plain-words name, or its ID for one this UI does not know. */
export function desyncMethodName(t: TFunction, method: string): string {
  return KNOWN_METHODS.has(method) ? t(`desync.caps.method.${method}`) : method
}

export type DesyncStrategyLabel = {
  /** "Split the TLS record, variant 2", or "Automatic" when unknown. */
  text: string
  /** The catalog strategy; null when the ID is not in the catalog. */
  strategy: DesyncStrategy | null
}

/**
 * The strategy a status line uses, in plain words. Null when the line has no
 * strategy to show: calibration has not settled (the chip explains why), the
 * route goes direct or through the VPN, or the line is inactive. A strategy
 * not in the catalog (or the catalog not loaded) reads "Automatic".
 */
export function desyncStrategyLabel(
  t: TFunction,
  view: DesyncPhaseView,
  strategies: DesyncStrategy[],
): DesyncStrategyLabel | null {
  if (!APPLIED_PHASES.has(view.phase)) return null
  const id = view.candidate
  if (!id || id === 'direct' || id === 'profile') return null
  const strategy = strategies.find((s) => s.id === id) ?? null
  if (!strategy || !KNOWN_KINDS.has(strategy.kind))
    return { text: t('desync.strategy.automatic'), strategy: null }
  return {
    text: t('desync.strategy.named', {
      name: t(`desync.strategy.kind.${strategy.kind}`),
      variant: strategy.variant,
    }),
    strategy,
  }
}
