import i18n from 'i18next'
import { initReactI18next } from 'react-i18next'

import en from './locales/en.json'
import ru from './locales/ru.json'
import zh from './locales/zh.json'

export const LS_LANG = 'sloth-lang'

function detectSystemUiLang(): 'en' | 'ru' | 'zh' {
  if (typeof navigator === 'undefined') return 'en'
  const n = String(navigator.language || '').toLowerCase()
  if (n.startsWith('zh')) return 'zh'
  if (n.startsWith('ru')) return 'ru'
  return 'en'
}

export function readStoredLang(): 'en' | 'ru' | 'zh' {
  try {
    const v = localStorage.getItem(LS_LANG)
    if (v === 'ru' || v === 'zh' || v === 'en') return v
  } catch {
    /* no localStorage */
  }
  return detectSystemUiLang()
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

export default i18n
