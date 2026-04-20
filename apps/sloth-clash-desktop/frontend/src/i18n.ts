import i18n from 'i18next'
import { initReactI18next } from 'react-i18next'

import en from './locales/en.json'
import ru from './locales/ru.json'
import zh from './locales/zh.json'

export const LS_LANG = 'sloth-lang'

/**
 * Prefer the first language in the user's preferred list (Windows: Settings → Language).
 * Using only `navigator.language` often yields the regional default (e.g. ru-RU) even when
 * the display/UI language is English — that made first-run UI Russian for English installs.
 */
function detectSystemUiLang(): 'en' | 'ru' | 'zh' {
  if (typeof navigator === 'undefined') return 'en'
  const list: string[] = []
  try {
    if (Array.isArray(navigator.languages) && navigator.languages.length > 0) {
      list.push(...navigator.languages)
    }
  } catch {
    /* */
  }
  if (navigator.language) list.push(navigator.language)
  for (const raw of list) {
    const n = String(raw || '').toLowerCase()
    if (n.startsWith('zh')) return 'zh'
    if (n.startsWith('ru')) return 'ru'
    if (n.startsWith('en')) return 'en'
  }
  return 'en'
}

export function readStoredLang(): 'en' | 'ru' | 'zh' {
  const systemLang = detectSystemUiLang()
  try {
    const v = localStorage.getItem(LS_LANG)
    if (v === 'ru' || v === 'zh' || v === 'en') {
      // Migration guard: many users had stale "en" persisted before we improved language detection.
      // If system UI is ru/zh, prefer system language unless user explicitly switches again.
      if (v === 'en' && systemLang !== 'en') return systemLang
      return v
    }
  } catch {
    /* no localStorage */
  }
  return systemLang
}

function langBase(code: string): string {
  return String(code || '')
    .split('-')[0]
    .toLowerCase()
}

void i18n.use(initReactI18next).init({
  lng: readStoredLang(),
  fallbackLng: 'en',
  resources: {
    en: { translation: en },
    ru: { translation: ru },
    zh: { translation: zh },
  },
  interpolation: { escapeValue: false },
})

// init() finishes asynchronously; align once with persisted / detected language.
i18n.on('initialized', () => {
  const want = readStoredLang()
  if (langBase(i18n.language) !== want) {
    void i18n.changeLanguage(want)
  }
})

export default i18n
