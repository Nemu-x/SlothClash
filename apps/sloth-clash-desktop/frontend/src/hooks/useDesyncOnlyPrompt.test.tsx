import { act, waitFor } from '@testing-library/react'
import { describe, expect, it, vi } from 'vitest'

import type { DesyncSettings, DesyncStatus } from '../api/desync'
import { desyncStatus } from '../test/desyncFixtures'
import { renderHookWithProviders } from '../test/render'
import { wailsApp } from '../test/wailsMock'

import { useDesyncOnlyPrompt } from './useDesyncOnlyPrompt'

function setup(
  status: DesyncStatus,
  save: (s: DesyncSettings) => Promise<boolean> = async () => true,
) {
  wailsApp.stub('GetDesyncStatus', () => status)
  const saveFn = vi.fn(save)
  const onFailed = vi.fn()
  const { result } = renderHookWithProviders(() =>
    useDesyncOnlyPrompt({ save: saveFn, onFailed }),
  )
  return { result, save: saveFn, onFailed }
}

// Discord chosen for TCP only, Desync off: the state the master switch fixes
// up by turning voice on.
const OFF_DISCORD_TCP = { enabled: false, discord: true, discordUDP: false }

describe('useDesyncOnlyPrompt', () => {
  it('asks only while Desync is off', async () => {
    const on = setup(desyncStatus())
    await act(() => on.result.current.offer())
    expect(on.result.current.open).toBe(false)

    const off = setup(desyncStatus({ settings: { enabled: false } }))
    await act(() => off.result.current.offer())
    expect(off.result.current.open).toBe(true)
  })

  it('turns Discord voice on exactly as the master switch does', async () => {
    const { result, save } = setup(
      desyncStatus({ settings: OFF_DISCORD_TCP, udpCoreSupported: true }),
    )
    await act(() => result.current.offer())
    await act(() => result.current.enable())
    expect(save).toHaveBeenCalledTimes(1)
    expect(save.mock.calls[0][0]).toMatchObject({
      enabled: true,
      youtube: true,
      discord: true,
      discordUDP: true,
    })
    expect(result.current.open).toBe(false)
  })

  it('leaves voice off when the core has no UDP support', async () => {
    const { result, save } = setup(
      desyncStatus({ settings: OFF_DISCORD_TCP, udpCoreSupported: false }),
    )
    await act(() => result.current.offer())
    await act(() => result.current.enable())
    expect(save.mock.calls[0][0]).toMatchObject({
      enabled: true,
      discord: true,
      discordUDP: false,
    })
  })

  it('does not select Discord when it was not selected', async () => {
    const { result, save } = setup(
      desyncStatus({
        settings: { enabled: false, discord: false, discordUDP: false },
      }),
    )
    await act(() => result.current.offer())
    await act(() => result.current.enable())
    expect(save.mock.calls[0][0]).toMatchObject({
      enabled: true,
      discord: false,
      discordUDP: false,
    })
  })

  it('stays open when another write is in flight', async () => {
    const { result } = setup(
      desyncStatus({ settings: { enabled: false } }),
      async () => false,
    )
    await act(() => result.current.offer())
    await act(() => result.current.enable())
    expect(result.current.open).toBe(true)
    expect(result.current.busy).toBe(false)
  })

  it('closes and reports a rejected save', async () => {
    const { result, onFailed } = setup(
      desyncStatus({ settings: { enabled: false } }),
      async () => {
        throw new Error('nothing selected')
      },
    )
    await act(() => result.current.offer())
    await act(() => result.current.enable())
    await waitFor(() => expect(result.current.open).toBe(false))
    expect(onFailed).toHaveBeenCalledWith(new Error('nothing selected'))
  })
})
