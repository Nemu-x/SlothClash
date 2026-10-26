import { waitFor } from '@testing-library/react'
import { afterEach, describe, expect, it, vi } from 'vitest'

import { desyncAuto, desyncStatus } from '../../test/desyncFixtures'
import { renderHookWithProviders } from '../../test/render'
import { wailsApp } from '../../test/wailsMock'

import { useDesyncStatus } from './useDesyncStatus'

const CTX = { connected: true, mode: 'rule', traffic: 'tun' }

afterEach(() => {
  vi.useRealTimers()
})

async function pollsIn(ms: number, enabled: boolean) {
  vi.useFakeTimers({ shouldAdvanceTime: true })
  const getStatus = wailsApp.stub('GetDesyncStatus', () =>
    desyncStatus({ settings: { enabled } }),
  )
  wailsApp.stub('GetDesyncAutoStatus', () => desyncAuto())
  const { result } = renderHookWithProviders(() => useDesyncStatus(CTX))
  await waitFor(() => expect(result.current.snapshot).not.toBeNull())
  const first = getStatus.mock.calls.length
  await vi.advanceTimersByTimeAsync(ms)
  return getStatus.mock.calls.length - first
}

describe('useDesyncStatus polling', () => {
  it('polls every 2.5 s while Desync is on', async () => {
    expect(await pollsIn(10_000, true)).toBeGreaterThanOrEqual(3)
  })

  it('backs off to 15 s while Desync is off', async () => {
    expect(await pollsIn(10_000, false)).toBe(0)
  })
})
