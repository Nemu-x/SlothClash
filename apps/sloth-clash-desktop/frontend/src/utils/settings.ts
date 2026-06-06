import { LS_SETTINGS } from '../constants'
import type { CompactSettings } from '../types/app'

export const DEFAULT_SETTINGS: CompactSettings = {
  startMinimized: false,
  launchOnStartup: false,
  closeToTray: true,
  dnsSmartFallback: true,
  dnsIpv6: false,
  dnsAllowLan: false,
  logLevel: 'info',
  defaultAutoUpdateMinutes: 360,
  reconnectOnManualProfileUpdate: true,
  uiScale: 1,
}

/** Selectable UI zoom steps (label derived as `${x * 100}%`). */
export const UI_SCALE_OPTIONS = [0.8, 0.9, 1, 1.1, 1.25, 1.5] as const

export function clampUiScale(v: unknown): number {
  const n = Number(v)
  if (!Number.isFinite(n) || n < 0.5 || n > 2) return 1
  return n
}

/** Apply the UI zoom factor to the document root (scales the whole UI). */
export function applyUiScale(scale: number): void {
  // CSS `zoom` (Chromium/WebView2) scales layout uniformly — px, rem and
  // embedded widgets (Monaco) alike — without a px→rem refactor.
  document.documentElement.style.zoom = String(clampUiScale(scale))
}

export function loadCompactSettings(): CompactSettings {
  const raw = localStorage.getItem(LS_SETTINGS)
  if (!raw) return DEFAULT_SETTINGS
  try {
    const parsed = JSON.parse(raw) as Partial<CompactSettings>
    return {
      ...DEFAULT_SETTINGS,
      ...parsed,
      defaultAutoUpdateMinutes:
        Number(parsed.defaultAutoUpdateMinutes) > 0
          ? Number(parsed.defaultAutoUpdateMinutes)
          : DEFAULT_SETTINGS.defaultAutoUpdateMinutes,
      reconnectOnManualProfileUpdate:
        parsed.reconnectOnManualProfileUpdate ?? true,
      uiScale: clampUiScale(parsed.uiScale),
    }
  } catch {
    return DEFAULT_SETTINGS
  }
}
