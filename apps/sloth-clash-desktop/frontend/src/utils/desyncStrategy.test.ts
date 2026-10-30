import i18next, { type TFunction } from 'i18next'
import { beforeAll, describe, expect, it } from 'vitest'

import en from '../locales/en.json'
import ru from '../locales/ru.json'
import zh from '../locales/zh.json'
import {
  DESYNC_TLSREC_2,
  DESYNC_UDP_ZERO_1,
  desyncCapabilities,
} from '../test/desyncFixtures'

import type { DesyncPhaseView } from './desyncStatus'
import {
  DESYNC_METHODS,
  DESYNC_STRATEGY_KINDS,
  desyncMethodName,
  desyncStrategyLabel,
} from './desyncStrategy'

let tEn: TFunction
let tRu: TFunction
let tZh: TFunction

beforeAll(async () => {
  const i18n = i18next.createInstance()
  await i18n.init({
    resources: {
      en: { translation: en },
      ru: { translation: ru },
      zh: { translation: zh },
    },
    lng: 'en',
    fallbackLng: false,
    interpolation: { escapeValue: false },
  })
  tEn = i18n.getFixedT('en')
  tRu = i18n.getFixedT('ru')
  tZh = i18n.getFixedT('zh')
})

const STRATEGIES = desyncCapabilities().strategies

function view(phase: string, candidate?: string): DesyncPhaseView {
  return { phase, tone: 'ok', attempt: 1, total: 1, candidate }
}

describe('desyncStrategyLabel', () => {
  it('names an applied catalog strategy by kind and variant', () => {
    const label = desyncStrategyLabel(
      tEn,
      view('ready', DESYNC_TLSREC_2),
      STRATEGIES,
    )
    expect(label?.text).toBe('Split the TLS record, variant 2')
    expect(label?.strategy?.id).toBe(DESYNC_TLSREC_2)
  })

  it('names the voice strategy while voice traffic flows', () => {
    for (const phase of ['transport', 'media', 'receiving', 'partial'])
      expect(
        desyncStrategyLabel(tEn, view(phase, DESYNC_UDP_ZERO_1), STRATEGIES)
          ?.text,
      ).toBe('Send decoy datagrams first, variant 1')
  })

  it('reads "chosen automatically" for an ID the catalog does not list', () => {
    const label = desyncStrategyLabel(
      tEn,
      view('ready', 'sdeadbeef00'),
      STRATEGIES,
    )
    expect(label).toEqual({ text: 'chosen automatically', strategy: null })
    // The catalog not loaded yet: the same.
    expect(
      desyncStrategyLabel(tEn, view('ready', DESYNC_TLSREC_2), []),
    ).toEqual({ text: 'chosen automatically', strategy: null })
  })

  it('reads "chosen automatically" for a kind this UI does not know', () => {
    const odd = [{ ...STRATEGIES[0], kind: 'future-kind' }]
    expect(desyncStrategyLabel(tEn, view('ready', odd[0].id), odd)?.text).toBe(
      'chosen automatically',
    )
  })

  it('shows nothing while calibration has not settled or is inactive', () => {
    for (const phase of [
      'queued',
      'testing',
      'checking',
      'waiting',
      'observing',
      'interrupted',
      'disabled',
      'connect',
      'rule',
      'needs_tun',
      'failed',
      'no_strategy',
    ])
      expect(
        desyncStrategyLabel(tEn, view(phase, DESYNC_TLSREC_2), STRATEGIES),
      ).toBeNull()
  })

  it('shows nothing for a site that is not blocked or goes through the VPN', () => {
    expect(
      desyncStrategyLabel(tEn, view('not_needed', 'direct'), STRATEGIES),
    ).toBeNull()
    expect(
      desyncStrategyLabel(tEn, view('profile', 'profile'), STRATEGIES),
    ).toBeNull()
    expect(
      desyncStrategyLabel(tEn, view('ready', 'direct'), STRATEGIES),
    ).toBeNull()
    expect(desyncStrategyLabel(tEn, view('ready'), STRATEGIES)).toBeNull()
  })

  it('translates every kind and method in every language', () => {
    for (const t of [tEn, tRu, tZh]) {
      for (const kind of DESYNC_STRATEGY_KINDS) {
        const text = t(`desync.strategy.kind.${kind}`)
        expect(text).not.toContain('desync.')
      }
      for (const method of DESYNC_METHODS)
        expect(desyncMethodName(t, method)).not.toContain('desync.')
    }
    expect(desyncMethodName(tEn, 'unknown-method')).toBe('unknown-method')
    expect(
      desyncStrategyLabel(tRu, view('ready', DESYNC_TLSREC_2), STRATEGIES)
        ?.text,
    ).toBe('Разделение TLS-записи, вариант 2')
  })
})
