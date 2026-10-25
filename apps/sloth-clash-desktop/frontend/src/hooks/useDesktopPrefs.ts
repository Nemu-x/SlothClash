import { type Dispatch, type SetStateAction, useEffect, useState } from 'react'

import {
  SetConnectionSettings,
  SetRealitySettings,
  SetTrafficSettings,
  SetTunSettings,
} from '../api/core'
import { main } from '../api/models'
import {
  GetDesktopPrefs,
  GetLaunchOnStartupPreference,
  GetTrayAvailability,
  SetAppAutoUpdateEnabled,
  SetCloseToTrayPreference,
  SetExperimentalSettings,
  SetHwidEnabled,
  SetLaunchOnStartupPreference,
  SetTraySettings,
  SetUISettings,
} from '../api/prefs'
import { LS_SETTINGS } from '../constants'
import type { CompactSettings } from '../types/app'
import { applyUiScale, loadCompactSettings } from '../utils/settings'

import type { Toast } from './useToasts'

/** Startup / window toggles owned by the backend (prefs.json `ui`). */
export type UiPrefKey =
  | 'startMinimized'
  | 'autoConnectOnStartup'
  | 'closeToTray'

/** Free-text TUN fields, committed together with a TunSettings patch. */
export type TunDrafts = { dnsHijack?: string; mtu?: string; device?: string }

export type DesktopPrefs = {
  /** Compact UI settings (localStorage), with the `ui` toggles mirrored in. */
  settings: CompactSettings
  setSettings: Dispatch<SetStateAction<CompactSettings>>
  setSetting: <K extends keyof CompactSettings>(
    key: K,
    value: CompactSettings[K],
  ) => void
  setUiPref: (key: UiPrefKey, value: boolean) => void
  setLaunchOnStartup: (next: boolean) => void
  /** Busy flag shared by the Settings bulk actions. */
  settingsBusy: boolean
  setSettingsBusy: Dispatch<SetStateAction<boolean>>
  trayAvailable: boolean
  // '' = platform default, 'colorful' | 'mono'.
  trayIconStyle: string
  commitTrayIconStyle: (next: string) => void
  tunPrefs: main.TunSettings
  trafficPrefs: main.TrafficSettings
  connectionPrefs: main.ConnectionSettings
  realityPrefs: main.RealitySettings
  tunDnsHijackDraft: string
  tunMtuDraft: string
  tunDeviceDraft: string
  setTunDnsHijackDraft: (next: string) => void
  setTunMtuDraft: (next: string) => void
  setTunDeviceDraft: (next: string) => void
  /** One save at a time across the TUN / traffic / connection commits. */
  tunPrefsSaving: boolean
  commitTunPrefs: (
    patch: Partial<main.TunSettings>,
    drafts?: TunDrafts,
  ) => Promise<void>
  commitTrafficPrefs: (patch: Partial<main.TrafficSettings>) => Promise<void>
  commitConnectionPrefs: (
    patch: Partial<main.ConnectionSettings>,
  ) => Promise<void>
  commitRealityPrefs: (patch: Partial<main.RealitySettings>) => Promise<void>
  hwidEnabled: boolean
  hwidSaving: boolean
  toggleHwid: (next: boolean) => void
  appUpdateEnabled: boolean
  toggleAppUpdate: (next: boolean) => void
  corpVpnEnabled: boolean
  toggleCorpVpn: (next: boolean) => void
}

/**
 * Desktop preferences: the localStorage compact settings plus the
 * backend-owned prefs.json sections, loaded once on mount and written through
 * the Go setters (each echo is authoritative).
 *
 * Called once, from App: App reads some of these itself (auto-connect, the
 * corp-VPN tab, diagnostics export, reset) and hands the same object to
 * SettingsPage and TunSettingsModal. A second call would be a second,
 * divergent copy of the state.
 */
export function useDesktopPrefs({
  setError,
  pushToast,
  onCorpVpnDisabled,
}: {
  setError: (message: string) => void
  pushToast: (toast: Omit<Toast, 'id'>) => number
  /** Prefs say the corp-VPN tab is off (mount load only). */
  onCorpVpnDisabled: () => void
}): DesktopPrefs {
  const [settings, setSettings] = useState<CompactSettings>(() =>
    loadCompactSettings(),
  )
  const [settingsBusy, setSettingsBusy] = useState(false)
  const [trayAvailable, setTrayAvailable] = useState(false)
  const [tunPrefs, setTunPrefs] = useState<main.TunSettings>(
    () => new main.TunSettings({}),
  )
  const [trafficPrefs, setTrafficPrefs] = useState<main.TrafficSettings>(
    () => new main.TrafficSettings({}),
  )
  // Backend-owned (prefs.json), not localStorage: allow-lan changes what the
  // core binds to, so the Go side must be the source of truth.
  const [connectionPrefs, setConnectionPrefs] =
    useState<main.ConnectionSettings>(() => new main.ConnectionSettings({}))
  // Backend-owned (prefs.json): REALITY key-share mode + client version, written
  // into the core config (core patch series).
  const [realityPrefs, setRealityPrefs] = useState<main.RealitySettings>(
    () => new main.RealitySettings({}),
  )
  // HWID is enabled by default; the prefs.json field uses an optional bool so
  // a fresh install (or any pre-0.4.1 prefs file lacking `privacy`) lands on
  // `true` here. Setting this to false omits the x-hwid header on subscription
  // import / refresh — other identity headers remain.
  const [hwidEnabled, setHwidEnabled] = useState<boolean>(true)
  const [appUpdateEnabled, setAppUpdateEnabled] = useState<boolean>(true)
  // Experimental: Corporate VPN (OpenConnect) tab is opt-in, hidden by default.
  const [corpVpnEnabled, setCorpVpnEnabled] = useState<boolean>(false)
  const [hwidSaving, setHwidSaving] = useState(false)
  // Tray icon look, backend-owned (prefs.json): '' = platform default,
  // 'colorful' | 'mono'. The native tray reads it directly; we only mirror it.
  const [trayIconStyle, setTrayIconStyle] = useState<string>('')
  const [tunDnsHijackDraft, setTunDnsHijackDraft] = useState<string>('')
  const [tunMtuDraft, setTunMtuDraft] = useState<string>('')
  const [tunDeviceDraft, setTunDeviceDraft] = useState<string>('')
  const [tunPrefsSaving, setTunPrefsSaving] = useState(false)

  // Sync the persisted autostart state with the OS-level registry on mount.
  // "Start minimized" is honoured natively by Go (StartHidden from prefs.json)
  // before the window exists — the old WindowHide() here fired after the window
  // had already flashed, and depended on localStorage having survived.
  //
  // Carried over from App.tsx unchanged: runs once, and the migration reads
  // the settings from the first render on purpose (the localStorage values).
  /* eslint-disable @eslint-react/exhaustive-deps, react-hooks/exhaustive-deps */
  useEffect(() => {
    void (async () => {
      try {
        const actual = await GetLaunchOnStartupPreference()
        setSetting('launchOnStartup', Boolean(actual))
      } catch {
        /* ignore: registry not available (non-Windows / permission denied) */
      }
      try {
        const prefs = await GetDesktopPrefs()
        // Startup / window toggles are backend-owned (prefs.json). A build that
        // never wrote the section reports every field null: migrate the
        // localStorage values once, after which prefs.json is the only source
        // and localStorage is just a stale cache.
        const ui = (prefs as any)?.ui
        const uiIsSet =
          ui &&
          (ui.startMinimized != null ||
            ui.autoConnectOnStartup != null ||
            ui.closeToTray != null)
        if (uiIsSet) {
          applyUiPrefs(ui)
        } else {
          try {
            const migrated = await SetUISettings(
              new main.UISettings({
                startMinimized: settings.startMinimized,
                autoConnectOnStartup: settings.autoConnectOnStartup,
                closeToTray: settings.closeToTray,
              }),
            )
            applyUiPrefs((migrated as any)?.ui)
          } catch {
            /* keep the localStorage values for this session; retried next launch */
          }
        }
        const nextTun = new main.TunSettings(prefs?.tun ?? {})
        const nextTraffic = new main.TrafficSettings(prefs?.traffic ?? {})
        setTunPrefs(nextTun)
        setTrafficPrefs(nextTraffic)
        setConnectionPrefs(new main.ConnectionSettings(prefs?.connection ?? {}))
        setRealityPrefs(new main.RealitySettings(prefs?.reality ?? {}))
        setTunDnsHijackDraft((nextTun.dnsHijack ?? []).join(', '))
        setTunMtuDraft(nextTun.mtu ? String(nextTun.mtu) : '')
        setTunDeviceDraft(nextTun.device ?? '')
        // Privacy.hwidEnabled is *bool on the Go side: undefined/null on the
        // wire means "default → on". Only an explicit `false` should flip the
        // toggle off.
        const rawHwid = prefs?.privacy?.hwidEnabled
        setHwidEnabled(rawHwid === false ? false : true)
        setTrayIconStyle(String((prefs as any)?.tray?.iconStyle ?? ''))
        // AppUpdate.autoCheckEnabled is *bool too: undefined/null → default on.
        const rawAppUpd = (prefs as any)?.appUpdate?.autoCheckEnabled
        setAppUpdateEnabled(rawAppUpd === false ? false : true)
        // Experimental.corpVpnEnabled is opt-in: only an explicit true shows it.
        const rawCorp = (prefs as any)?.experimental?.corpVpnEnabled
        const corpOn = rawCorp === true
        setCorpVpnEnabled(corpOn)
        // If it was disabled while the tab was open, fall back to Home so the
        // user isn't stranded on a now-hidden screen.
        if (!corpOn) onCorpVpnDisabled()
      } catch {
        /* ignore: prefs API unavailable */
      }
    })()
  }, [])
  /* eslint-enable @eslint-react/exhaustive-deps, react-hooks/exhaustive-deps */

  useEffect(() => {
    localStorage.setItem(LS_SETTINGS, JSON.stringify(settings))
  }, [settings])

  useEffect(() => {
    applyUiScale(settings.uiScale)
  }, [settings.uiScale])

  useEffect(() => {
    let cancelled = false
    let ticks = 0
    const refreshTray = async () => {
      try {
        const ok = await GetTrayAvailability()
        if (!cancelled) setTrayAvailable(Boolean(ok))
      } catch {
        if (!cancelled) setTrayAvailable(false)
      }
    }
    void (async () => {
      await refreshTray()
    })()
    const id = window.setInterval(() => {
      ticks += 1
      void refreshTray()
      if (ticks >= 20) window.clearInterval(id)
    }, 500)
    return () => {
      cancelled = true
      window.clearInterval(id)
    }
  }, [])

  useEffect(() => {
    void SetCloseToTrayPreference(
      Boolean(settings.closeToTray && trayAvailable),
    )
  }, [settings.closeToTray, trayAvailable])

  const setSetting = <K extends keyof CompactSettings>(
    key: K,
    value: CompactSettings[K],
  ) => {
    setSettings((prev) => ({ ...prev, [key]: value }))
  }

  // Mirror the backend-owned startup/window toggles into `settings` so every
  // consumer (auto-connect effect, close-to-tray push, Settings page) keeps
  // reading one object. Go normalizes nulls to defaults on write, so a
  // missing field here only happens before migration.
  const applyUiPrefs = (ui: any) => {
    if (!ui) return
    setSettings((prev) => ({
      ...prev,
      startMinimized: ui.startMinimized === true,
      autoConnectOnStartup: ui.autoConnectOnStartup === true,
      closeToTray: ui.closeToTray !== false,
    }))
  }

  // Toggle one of the backend-owned startup/window prefs: optimistic flip,
  // then the backend's echo is authoritative; rolled back with a toast on
  // error. The write is atomic on the Go side, which is the whole point —
  // localStorage could silently lose these on an abrupt shutdown.
  const setUiPref = (key: UiPrefKey, value: boolean) => {
    const prevValue = settings[key]
    setSetting(key, value)
    const next = {
      startMinimized: settings.startMinimized,
      autoConnectOnStartup: settings.autoConnectOnStartup,
      closeToTray: settings.closeToTray,
      [key]: value,
    }
    void SetUISettings(new main.UISettings(next))
      .then((prefs) => applyUiPrefs((prefs as any)?.ui))
      .catch((e) => {
        setSetting(key, prevValue)
        pushToast({ kind: 'error', message: String(e) })
      })
  }

  const setLaunchOnStartup = (next: boolean) => {
    setSetting('launchOnStartup', next)
    void (async () => {
      try {
        await SetLaunchOnStartupPreference(next)
      } catch (e: any) {
        setError(String(e))
        setSetting('launchOnStartup', !next)
      }
    })()
  }

  const commitTunPrefs = async (
    patch: Partial<main.TunSettings>,
    drafts?: TunDrafts,
  ) => {
    if (tunPrefsSaving) return
    setTunPrefsSaving(true)
    try {
      const dnsHijackRaw = drafts?.dnsHijack ?? tunDnsHijackDraft
      const dnsHijack = dnsHijackRaw
        .split(/[,\n;\r]+/)
        .map((s) => s.trim())
        .filter(Boolean)
      const mtuRaw = drafts?.mtu ?? tunMtuDraft
      const mtu = mtuRaw.trim() === '' ? 0 : Math.max(0, Number(mtuRaw) || 0)
      const device = (drafts?.device ?? tunDeviceDraft).trim()
      const next: main.TunSettings = new main.TunSettings({
        ...tunPrefs,
        ...patch,
        dnsHijack: dnsHijack.length > 0 ? dnsHijack : undefined,
        mtu: mtu > 0 ? mtu : undefined,
        device: device !== '' ? device : undefined,
      })
      const updated = await SetTunSettings(next)
      const updatedTun = new main.TunSettings(updated?.tun ?? {})
      setTunPrefs(updatedTun)
      setTunDnsHijackDraft((updatedTun.dnsHijack ?? []).join(', '))
      setTunMtuDraft(updatedTun.mtu ? String(updatedTun.mtu) : '')
      setTunDeviceDraft(updatedTun.device ?? '')
    } catch (e: any) {
      setError(String(e))
    } finally {
      setTunPrefsSaving(false)
    }
  }

  const commitConnectionPrefs = async (
    patch: Partial<main.ConnectionSettings>,
  ) => {
    if (tunPrefsSaving) return
    setTunPrefsSaving(true)
    try {
      const next: main.ConnectionSettings = new main.ConnectionSettings({
        ...connectionPrefs,
        ...patch,
      })
      const updated = await SetConnectionSettings(next)
      setConnectionPrefs(new main.ConnectionSettings(updated?.connection ?? {}))
    } catch (e: any) {
      setError(String(e))
    } finally {
      setTunPrefsSaving(false)
    }
  }

  // Invalid input is rejected by Go (the card validates first); on error the
  // card resyncs to the last saved value.
  const commitRealityPrefs = async (patch: Partial<main.RealitySettings>) => {
    try {
      const updated = await SetRealitySettings(
        new main.RealitySettings({ ...realityPrefs, ...patch }),
      )
      setRealityPrefs(new main.RealitySettings(updated?.reality ?? {}))
    } catch (e: any) {
      pushToast({ kind: 'error', message: String(e) })
    }
  }

  const commitTrafficPrefs = async (patch: Partial<main.TrafficSettings>) => {
    if (tunPrefsSaving) return
    setTunPrefsSaving(true)
    try {
      const next: main.TrafficSettings = new main.TrafficSettings({
        ...trafficPrefs,
        ...patch,
      })
      const updated = await SetTrafficSettings(next)
      setTrafficPrefs(new main.TrafficSettings(updated?.traffic ?? {}))
    } catch (e: any) {
      setError(String(e))
    } finally {
      setTunPrefsSaving(false)
    }
  }

  const commitTrayIconStyle = (next: string) => {
    // Optimistic; the backend echoes the normalized value back
    // and the native tray re-reads prefs on its next tick.
    setTrayIconStyle(next)
    void SetTraySettings(new main.TraySettings({ iconStyle: next }))
      .then((prefs) => {
        setTrayIconStyle(String((prefs as any)?.tray?.iconStyle ?? ''))
      })
      .catch((e) => {
        pushToast({ kind: 'error', message: String(e) })
      })
  }

  const toggleHwid = (next: boolean) => {
    if (hwidSaving) return
    setHwidSaving(true)
    // Optimistic flip so the toggle reflects user intent
    // immediately; rolled back on backend error.
    setHwidEnabled(next)
    void SetHwidEnabled(next)
      .then((prefs) => {
        const raw = prefs?.privacy?.hwidEnabled
        setHwidEnabled(raw === false ? false : true)
      })
      .catch((e) => {
        setHwidEnabled(!next)
        pushToast({ kind: 'error', message: String(e) })
      })
      .finally(() => setHwidSaving(false))
  }

  const toggleAppUpdate = (next: boolean) => {
    setAppUpdateEnabled(next)
    void SetAppAutoUpdateEnabled(next)
      .then((prefs: any) => {
        const raw = prefs?.appUpdate?.autoCheckEnabled
        setAppUpdateEnabled(raw === false ? false : true)
      })
      .catch(() => setAppUpdateEnabled(!next))
  }

  const toggleCorpVpn = (next: boolean) => {
    setCorpVpnEnabled(next)
    void SetExperimentalSettings({ corpVpnEnabled: next } as any)
      .then((prefs: any) => {
        setCorpVpnEnabled(prefs?.experimental?.corpVpnEnabled === true)
      })
      .catch(() => setCorpVpnEnabled(!next))
  }

  return {
    settings,
    setSettings,
    setSetting,
    setUiPref,
    setLaunchOnStartup,
    settingsBusy,
    setSettingsBusy,
    trayAvailable,
    trayIconStyle,
    commitTrayIconStyle,
    tunPrefs,
    trafficPrefs,
    connectionPrefs,
    realityPrefs,
    tunDnsHijackDraft,
    tunMtuDraft,
    tunDeviceDraft,
    setTunDnsHijackDraft,
    setTunMtuDraft,
    setTunDeviceDraft,
    tunPrefsSaving,
    commitTunPrefs,
    commitTrafficPrefs,
    commitConnectionPrefs,
    commitRealityPrefs,
    hwidEnabled,
    hwidSaving,
    toggleHwid,
    appUpdateEnabled,
    toggleAppUpdate,
    corpVpnEnabled,
    toggleCorpVpn,
  }
}
