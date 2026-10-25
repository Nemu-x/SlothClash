import { act } from '@testing-library/react'
import { afterEach, describe, expect, it, vi } from 'vitest'

import { renderHookWithProviders } from '../test/render'
import { wailsEvents } from '../test/wailsMock'

import { useBackendEvents } from './useBackendEvents'

const EVENTS = [
  'app:state',
  'app:update',
  'app:update:progress',
  'app:navigate',
  'app:install-config',
] as const

function setup(initial: { isAnyEditorModalOpen?: boolean } = {}) {
  const deps = {
    refresh: vi.fn(() => Promise.resolve()),
    invalidateUpdateState: vi.fn(),
    setUpdateProgress: vi.fn(),
    setScreen: vi.fn(),
    setError: vi.fn(),
    pushToast: vi.fn(() => 1),
  }
  const view = renderHookWithProviders(
    (props: { isAnyEditorModalOpen: boolean }) =>
      useBackendEvents({ ...deps, ...props }),
    {
      initialProps: {
        isAnyEditorModalOpen: initial.isAnyEditorModalOpen ?? false,
      },
    },
  )
  return { deps, ...view }
}

afterEach(() => {
  vi.useRealTimers()
})

describe('useBackendEvents', () => {
  it('subscribes once per event and unsubscribes on unmount', () => {
    const { unmount } = setup()
    for (const name of EVENTS) expect(wailsEvents.listenerCount(name)).toBe(1)
    unmount()
    for (const name of EVENTS) expect(wailsEvents.listenerCount(name)).toBe(0)
  })

  it('re-subscribes app:state without doubling when the editor flag flips', () => {
    const { rerender } = setup()
    rerender({ isAnyEditorModalOpen: true })
    expect(wailsEvents.listenerCount('app:state')).toBe(1)
    rerender({ isAnyEditorModalOpen: false })
    expect(wailsEvents.listenerCount('app:state')).toBe(1)
    // The other events do not depend on the flag.
    expect(wailsEvents.listenerCount('app:navigate')).toBe(1)
  })

  it('coalesces a burst of app:state pushes into one refresh after 120 ms', () => {
    vi.useFakeTimers()
    const { deps } = setup()
    act(() => {
      wailsEvents.emit('app:state')
      wailsEvents.emit('app:state')
      wailsEvents.emit('app:state')
    })
    act(() => vi.advanceTimersByTime(119))
    expect(deps.refresh).not.toHaveBeenCalled()
    act(() => vi.advanceTimersByTime(1))
    expect(deps.refresh).toHaveBeenCalledTimes(1)
    // The next push after the window schedules a new refresh.
    act(() => wailsEvents.emit('app:state'))
    act(() => vi.advanceTimersByTime(120))
    expect(deps.refresh).toHaveBeenCalledTimes(2)
  })

  it('ignores app:state while a YAML editor modal is open', () => {
    vi.useFakeTimers()
    const { deps } = setup({ isAnyEditorModalOpen: true })
    act(() => wailsEvents.emit('app:state'))
    act(() => vi.advanceTimersByTime(500))
    expect(deps.refresh).not.toHaveBeenCalled()
  })

  it('drops a pending app:state refresh on unmount', () => {
    vi.useFakeTimers()
    const { deps, unmount } = setup()
    act(() => wailsEvents.emit('app:state'))
    unmount()
    act(() => vi.advanceTimersByTime(500))
    expect(deps.refresh).not.toHaveBeenCalled()
  })

  it('invalidates the update state on app:update', () => {
    const { deps } = setup()
    act(() => wailsEvents.emit('app:update'))
    expect(deps.invalidateUpdateState).toHaveBeenCalledTimes(1)
  })

  it('normalizes app:update:progress payloads and skips empty ones', () => {
    const { deps } = setup()
    act(() => wailsEvents.emit('app:update:progress', undefined))
    expect(deps.setUpdateProgress).not.toHaveBeenCalled()
    act(() =>
      wailsEvents.emit('app:update:progress', {
        downloaded: 10,
        total: 100,
        pct: 10,
      }),
    )
    act(() => wailsEvents.emit('app:update:progress', {}))
    expect(deps.setUpdateProgress.mock.calls).toEqual([
      [{ downloaded: 10, total: 100, pct: 10 }],
      [{ downloaded: 0, total: 0, pct: -1 }],
    ])
  })

  it('navigates only to the screens the tray may open', () => {
    const { deps } = setup()
    act(() => wailsEvents.emit('app:navigate', { screen: ' rules ' }))
    expect(deps.setScreen).toHaveBeenCalledWith('rules')
    expect(deps.refresh).toHaveBeenCalledTimes(1)
    for (const screen of ['connections', 'logs', 'corp', '', undefined]) {
      act(() => wailsEvents.emit('app:navigate', { screen }))
    }
    act(() => wailsEvents.emit('app:navigate', undefined))
    expect(deps.setScreen).toHaveBeenCalledTimes(1)
    expect(deps.refresh).toHaveBeenCalledTimes(1)
  })

  it('reports a deep-link install success on Home', () => {
    const { deps } = setup()
    act(() =>
      wailsEvents.emit('app:install-config', {
        success: true,
        profileName: 'Work',
      }),
    )
    expect(deps.refresh).toHaveBeenCalledTimes(1)
    expect(deps.setScreen).toHaveBeenCalledWith('home')
    expect(deps.setError).toHaveBeenCalledWith('')
    expect(deps.pushToast).toHaveBeenCalledWith({
      kind: 'success',
      message: expect.stringContaining('Work'),
    })
  })

  it('reports a deep-link install failure as a sticky error toast', () => {
    const { deps } = setup()
    act(() =>
      wailsEvents.emit('app:install-config', {
        success: false,
        message: 'bad url',
      }),
    )
    expect(deps.setScreen).toHaveBeenCalledWith('home')
    expect(deps.setError).not.toHaveBeenCalled()
    expect(deps.pushToast).toHaveBeenCalledWith({
      kind: 'error',
      message: 'bad url',
      durationMs: 0,
    })
  })
})
