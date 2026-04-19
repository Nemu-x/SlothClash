import i18n from 'i18next'
import { initReactI18next } from 'react-i18next'

import en from './locales/en.json'
import ru from './locales/ru.json'
import zh from './locales/zh.json'

export const LS_LANG = 'sloth-lang'

export function readStoredLang(): 'en' | 'ru' | 'zh' {
  try {
    const v = localStorage.getItem(LS_LANG)
    if (v === 'ru' || v === 'zh' || v === 'en') return v
  } catch {
    /* no localStorage */
  }
  return 'en'
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
