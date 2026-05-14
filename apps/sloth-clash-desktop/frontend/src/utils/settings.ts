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
    }
  } catch {
    return DEFAULT_SETTINGS
  }
}
