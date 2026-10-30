import i18next, { type TFunction } from 'i18next'
import { beforeAll, describe, expect, it } from 'vitest'

import en from '../locales/en.json'
import ru from '../locales/ru.json'
import zh from '../locales/zh.json'

import { desyncErrorText } from './desyncStatus'

const SETTINGS_CODES = [
  'unsupported_core',
  'unsupported_udp_core',
  'no_selection',
  'discord_platform',
  'no_profile',
]
const CUSTOM_CODES = [
  'empty',
  'invalid',
  'ip',
  'wildcard',
  'public_suffix',
  'builtin',
  'duplicate',
  'limit',
  'not_found',
  'service_overlap',
  'unknown_service',
]

let tEn: TFunction
let tRu: TFunction

beforeAll(async () => {
  const i18n = i18next.createInstance()
  await i18n.init({
    resources: {
      en: { translation: en },
      ru: { translation: ru },
    },
    lng: 'en',
    fallbackLng: false,
    interpolation: { escapeValue: false },
  })
  tEn = i18n.getFixedT('en')
  tRu = i18n.getFixedT('ru')
})

describe('desyncErrorText', () => {
  it.each(SETTINGS_CODES)('translates the settings code %s', (code) => {
    for (const t of [tEn, tRu]) {
      const text = desyncErrorText(t, `Error: desync: ${code}`)
      expect(text).not.toContain('desync')
      expect(text).not.toBe(`desync.error.${code}`)
    }
  })

  it('words settings failures in Russian for Russian users', () => {
    expect(desyncErrorText(tRu, 'desync: discord_platform')).toBe(
      'Discord через Desync работает только в Windows.',
    )
    expect(desyncErrorText(tRu, 'desync: no_selection')).toBe(
      'Сначала включи сервис или добавь сайт.',
    )
  })

  it.each(CUSTOM_CODES)('translates the site code %s', (code) => {
    const text = desyncErrorText(tRu, `desync: ${code}`)
    expect(text).not.toMatch(/desync[.:]/)
  })

  it('names the popular service that covers a typed site', () => {
    expect(
      desyncErrorText(tEn, 'desync: service_overlap: facebook', {
        serviceName: () => 'Facebook',
      }),
    ).toBe('Already covered by Facebook in Popular services.')
  })

  it('words list codes for a popular-service switch', () => {
    expect(
      desyncErrorText(tEn, 'desync: duplicate', { forService: true }),
    ).toBe('This service is already on the list.')
  })

  it('shows anything else verbatim', () => {
    expect(desyncErrorText(tEn, 'core probe: HTTP 503')).toBe(
      'core probe: HTTP 503',
    )
    expect(desyncErrorText(tEn, 'desync: something_new')).toBe(
      'desync: something_new',
    )
  })
})

/** Every key path under `obj`, plural suffixes folded. */
function keys(obj: unknown, prefix = ''): Set<string> {
  const out = new Set<string>()
  if (typeof obj !== 'object' || obj === null) {
    out.add(prefix.replace(/_(zero|one|two|few|many|other)$/, ''))
    return out
  }
  for (const [k, v] of Object.entries(obj)) {
    for (const key of keys(v, prefix ? `${prefix}.${k}` : k)) out.add(key)
  }
  return out
}

describe('Desync strings', () => {
  const desyncKeys = (locale: Record<string, any>) =>
    new Set([
      ...keys(locale.desync, 'desync'),
      ...keys(locale.ui.profiles.desyncOnly, 'ui.profiles.desyncOnly'),
    ])

  it.each([
    ['ru', ru],
    ['zh', zh],
  ])('%s has every English Desync key and no other', (_, locale) => {
    expect([...desyncKeys(locale)].sort()).toEqual([...desyncKeys(en)].sort())
  })

  it('has the keys the settings and onboarding use', () => {
    for (const locale of [en, ru]) {
      const all = desyncKeys(locale)
      for (const code of SETTINGS_CODES)
        expect(all).toContain(`desync.error.${code}`)
      for (const code of CUSTOM_CODES)
        expect(all).toContain(`desync.custom.error.${code}`)
      expect(all).toContain('desync.services.title')
      expect(all).toContain('desync.onboarding.defaultName')
    }
    expect(ru.desync.onboarding.defaultName).toBe('Только Desync')
  })
})
