import { act, waitFor } from '@testing-library/react'
import { beforeEach, describe, expect, it, vi } from 'vitest'

import { LS_SETTINGS } from '../constants'
import { renderHookWithProviders } from '../test/render'
import { wailsApp } from '../test/wailsMock'

import { useDesktopPrefs } from './useDesktopPrefs'

// prefs.json as GetDesktopPrefs returns it after a 0.9.3+ write.
const SAVED_PREFS = {
  ui: { startMinimized: true, autoConnectOnStartup: true, closeToTray: false },
  tun: { stack: 'gvisor', dnsHijack: ['any:53', 'tcp://any:53'], mtu: 1400 },
  traffic: { snifferEnabled: true },
  connection: { allowLan: true, mixedPort: 7890 },
  reality: { clientVersion: '1.8.0' },
  privacy: { hwidEnabled: false },
  tray: { iconStyle: 'mono' },
  appUpdate: { autoCheckEnabled: false },
  experimental: { corpVpnEnabled: true },
}

function stubMount(prefs: unknown = SAVED_PREFS) {
  wailsApp.stub('GetLaunchOnStartupPreference', () => true)
  wailsApp.stub('GetDesktopPrefs', () => prefs)
  wailsApp.stub('GetTrayAvailability', () => true)
  wailsApp.stub('SetCloseToTrayPreference', () => undefined)
}

function setup() {
  const deps = {
    setError: vi.fn(),
    pushToast: vi.fn(() => 1),
    onCorpVpnDisabled: vi.fn(),
  }
  const view = renderHookWithProviders(() => useDesktopPrefs(deps))
  return { deps, ...view }
}

// Mount + wait for the one-shot prefs load to land.
async function setupLoaded(prefs: unknown = SAVED_PREFS) {
  stubMount(prefs)
  const view = setup()
  await waitFor(() =>
    expect(wailsApp.fn('GetDesktopPrefs')).toHaveBeenCalledTimes(1),
  )
  await waitFor(() => expect(view.result.current.tunMtuDraft).not.toBe(''))
  return view
}

beforeEach(() => {
  localStorage.clear()
})

describe('useDesktopPrefs: mount load', () => {
  it('starts from the defaults before the backend answers', () => {
    stubMount()
    const { result } = setup()
    expect(result.current.settings.closeToTray).toBe(true)
    expect(result.current.hwidEnabled).toBe(true)
    expect(result.current.appUpdateEnabled).toBe(true)
    expect(result.current.corpVpnEnabled).toBe(false)
    expect(result.current.tunPrefsSaving).toBe(false)
  })

  it('mirrors prefs.json and the registry autostart flag', async () => {
    const { result, deps } = await setupLoaded()
    const p = result.current
    expect(p.settings).toMatchObject({
      launchOnStartup: true,
      startMinimized: true,
      autoConnectOnStartup: true,
      closeToTray: false,
    })
    expect(p.tunPrefs.stack).toBe('gvisor')
    expect(p.tunDnsHijackDraft).toBe('any:53, tcp://any:53')
    expect(p.tunMtuDraft).toBe('1400')
    expect(p.tunDeviceDraft).toBe('')
    expect(p.trafficPrefs.snifferEnabled).toBe(true)
    expect(p.connectionPrefs.allowLan).toBe(true)
    expect(p.realityPrefs.clientVersion).toBe('1.8.0')
    expect(p.hwidEnabled).toBe(false)
    expect(p.trayIconStyle).toBe('mono')
    expect(p.appUpdateEnabled).toBe(false)
    expect(p.corpVpnEnabled).toBe(true)
    expect(deps.onCorpVpnDisabled).not.toHaveBeenCalled()
    // A populated `ui` section is never re-migrated.
    expect(wailsApp.fn('SetUISettings')).not.toHaveBeenCalled()
  })

  it('migrates the localStorage startup toggles once when prefs.json has none', async () => {
    localStorage.setItem(
      LS_SETTINGS,
      JSON.stringify({ startMinimized: true, closeToTray: false }),
    )
    const setUi = wailsApp.stub('SetUISettings', (ui: any) => ({ ui }))
    const { result, deps } = await setupLoaded({
      ui: {
        startMinimized: null,
        autoConnectOnStartup: null,
        closeToTray: null,
      },
      tun: { mtu: 1500 },
    })
    expect(setUi).toHaveBeenCalledTimes(1)
    expect(setUi.mock.calls[0][0]).toMatchObject({
      startMinimized: true,
      autoConnectOnStartup: false,
      closeToTray: false,
    })
    expect(result.current.settings.startMinimized).toBe(true)
    expect(result.current.settings.closeToTray).toBe(false)
    // Missing *bool fields keep their defaults; corp VPN is opt-in.
    expect(result.current.hwidEnabled).toBe(true)
    expect(result.current.appUpdateEnabled).toBe(true)
    expect(result.current.corpVpnEnabled).toBe(false)
    expect(deps.onCorpVpnDisabled).toHaveBeenCalledTimes(1)
  })

  it('persists the compact settings to localStorage', async () => {
    const { result } = await setupLoaded()
    act(() => result.current.setSetting('uiScale', 1.25))
    const stored = JSON.parse(localStorage.getItem(LS_SETTINGS) ?? '{}')
    expect(stored.uiScale).toBe(1.25)
    expect(stored.launchOnStartup).toBe(true)
  })
})

describe('useDesktopPrefs: commits', () => {
  it('commitTunPrefs sends the parsed drafts and adopts the echo', async () => {
    const { result } = await setupLoaded()
    const setTun = wailsApp.stub('SetTunSettings', (tun: any) => ({
      tun: { ...tun, mtu: 9000 },
    }))
    await act(() =>
      result.current.commitTunPrefs(
        { autoRoute: false },
        { dnsHijack: ' 1.1.1.1:53 ;\n8.8.8.8:53,', mtu: '1280', device: ' ' },
      ),
    )
    const sent = setTun.mock.calls[0][0]
    expect(sent).toMatchObject({
      stack: 'gvisor',
      autoRoute: false,
      dnsHijack: ['1.1.1.1:53', '8.8.8.8:53'],
      mtu: 1280,
    })
    expect(sent.device).toBeUndefined()
    expect(result.current.tunPrefs.mtu).toBe(9000)
    expect(result.current.tunMtuDraft).toBe('9000')
    expect(result.current.tunDnsHijackDraft).toBe('1.1.1.1:53, 8.8.8.8:53')
    expect(result.current.tunPrefsSaving).toBe(false)
  })

  it('commitTunPrefs keeps the saved prefs and reports the error on failure', async () => {
    const { result, deps } = await setupLoaded()
    wailsApp.stub('SetTunSettings', () =>
      Promise.reject(new Error('mtu out of range')),
    )
    const before = result.current.tunPrefs
    await act(() => result.current.commitTunPrefs({}, { mtu: '99999' }))
    expect(deps.setError).toHaveBeenCalledWith('Error: mtu out of range')
    expect(result.current.tunPrefs).toBe(before)
    expect(result.current.tunPrefsSaving).toBe(false)
  })

  it('commitConnectionPrefs merges the patch over the saved section', async () => {
    const { result } = await setupLoaded()
    const setConn = wailsApp.stub('SetConnectionSettings', (connection) => ({
      connection,
    }))
    await act(() => result.current.commitConnectionPrefs({ smartDns: true }))
    expect(setConn.mock.calls[0][0]).toMatchObject({
      allowLan: true,
      mixedPort: 7890,
      smartDns: true,
    })
    expect(result.current.connectionPrefs.smartDns).toBe(true)
  })

  it('commitRealityPrefs toasts and keeps the saved value on rejection', async () => {
    const { result, deps } = await setupLoaded()
    wailsApp.stub('SetRealitySettings', () =>
      Promise.reject(new Error('invalid version')),
    )
    await act(() => result.current.commitRealityPrefs({ clientVersion: 'x' }))
    expect(deps.pushToast).toHaveBeenCalledWith({
      kind: 'error',
      message: 'Error: invalid version',
    })
    expect(result.current.realityPrefs.clientVersion).toBe('1.8.0')
  })
})

describe('useDesktopPrefs: optimistic toggles', () => {
  it('setUiPref flips at once and keeps the backend echo', async () => {
    const { result } = await setupLoaded()
    const setUi = wailsApp.stub('SetUISettings', (ui: any) => ({ ui }))
    act(() => result.current.setUiPref('closeToTray', true))
    expect(result.current.settings.closeToTray).toBe(true)
    expect(setUi.mock.calls[0][0]).toMatchObject({
      startMinimized: true,
      autoConnectOnStartup: true,
      closeToTray: true,
    })
    await waitFor(() => expect(setUi).toHaveBeenCalledTimes(1))
    expect(result.current.settings.closeToTray).toBe(true)
  })

  it('setUiPref rolls back and toasts when the write fails', async () => {
    const { result, deps } = await setupLoaded()
    wailsApp.stub('SetUISettings', () => Promise.reject(new Error('disk full')))
    act(() => result.current.setUiPref('startMinimized', false))
    expect(result.current.settings.startMinimized).toBe(false)
    await waitFor(() =>
      expect(result.current.settings.startMinimized).toBe(true),
    )
    expect(deps.pushToast).toHaveBeenCalledWith({
      kind: 'error',
      message: 'Error: disk full',
    })
  })

  it('setLaunchOnStartup rolls back and reports the error on failure', async () => {
    const { result, deps } = await setupLoaded()
    wailsApp.stub('SetLaunchOnStartupPreference', () =>
      Promise.reject(new Error('access denied')),
    )
    act(() => result.current.setLaunchOnStartup(false))
    expect(result.current.settings.launchOnStartup).toBe(false)
    await waitFor(() =>
      expect(result.current.settings.launchOnStartup).toBe(true),
    )
    expect(deps.setError).toHaveBeenCalledWith('Error: access denied')
  })

  it('toggleHwid rolls back on failure and ignores clicks while saving', async () => {
    const { result, deps } = await setupLoaded()
    let reject: (e: Error) => void = () => {}
    const setHwid = wailsApp.stub(
      'SetHwidEnabled',
      () =>
        new Promise((_res, rej) => {
          reject = rej
        }),
    )
    act(() => result.current.toggleHwid(true))
    expect(result.current.hwidEnabled).toBe(true)
    expect(result.current.hwidSaving).toBe(true)
    act(() => result.current.toggleHwid(false))
    expect(setHwid).toHaveBeenCalledTimes(1)
    await act(async () => reject(new Error('nope')))
    await waitFor(() => expect(result.current.hwidSaving).toBe(false))
    expect(result.current.hwidEnabled).toBe(false)
    expect(deps.pushToast).toHaveBeenCalledWith({
      kind: 'error',
      message: 'Error: nope',
    })
  })

  it('toggleCorpVpn adopts the backend echo', async () => {
    const { result } = await setupLoaded()
    wailsApp.stub('SetExperimentalSettings', () => ({
      experimental: { corpVpnEnabled: false },
    }))
    act(() => result.current.toggleCorpVpn(false))
    expect(result.current.corpVpnEnabled).toBe(false)
    await waitFor(() =>
      expect(wailsApp.fn('SetExperimentalSettings')).toHaveBeenCalledTimes(1),
    )
    expect(result.current.corpVpnEnabled).toBe(false)
  })

  it('toggleAppUpdate rolls back silently on failure', async () => {
    const { result, deps } = await setupLoaded()
    wailsApp.stub('SetAppAutoUpdateEnabled', () =>
      Promise.reject(new Error('x')),
    )
    act(() => result.current.toggleAppUpdate(true))
    expect(result.current.appUpdateEnabled).toBe(true)
    await waitFor(() => expect(result.current.appUpdateEnabled).toBe(false))
    expect(deps.pushToast).not.toHaveBeenCalled()
  })

  it('commitTrayIconStyle is optimistic and keeps the normalized echo', async () => {
    const { result } = await setupLoaded()
    wailsApp.stub('SetTraySettings', () => ({ tray: { iconStyle: '' } }))
    act(() => result.current.commitTrayIconStyle('colorful'))
    expect(result.current.trayIconStyle).toBe('colorful')
    await waitFor(() => expect(result.current.trayIconStyle).toBe(''))
  })
})
