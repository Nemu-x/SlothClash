import { act, fireEvent, renderHook } from '@testing-library/react'
import { describe, expect, it, vi } from 'vitest'

import { useProfileModals } from './useProfileModals'

const P = { id: 'p1', name: 'Work' }

describe('useProfileModals', () => {
  it('starts with everything closed', () => {
    const { result } = renderHook(() => useProfileModals())
    const m = result.current
    expect(m.importOpen).toBe(false)
    expect(m.importReason).toBe('manual')
    for (const target of [
      m.menu,
      m.merge,
      m.file,
      m.proxy,
      m.rules,
      m.script,
      m.editInfo,
      m.remove,
    ]) {
      expect(target).toBeNull()
    }
    expect(m.isAnyEditorModalOpen).toBe(false)
  })

  it('opens the import dialog with a reason and closes it', () => {
    const { result } = renderHook(() => useProfileModals())
    act(() => result.current.openImport('connect'))
    expect(result.current.importOpen).toBe(true)
    expect(result.current.importReason).toBe('connect')
    act(() => result.current.closeImport())
    expect(result.current.importOpen).toBe(false)
    // The reason sticks until the next open (the closing dialog keeps its title).
    expect(result.current.importReason).toBe('connect')
    act(() => result.current.openImport('beacon'))
    expect(result.current.importReason).toBe('beacon')
  })

  it('flags only the YAML editors as editor modals', () => {
    const { result } = renderHook(() => useProfileModals())
    const editors = ['setMerge', 'setFile', 'setProxy', 'setRules'] as const
    for (const set of editors) {
      act(() => result.current[set](P))
      expect(result.current.isAnyEditorModalOpen).toBe(true)
      act(() => result.current[set](null))
      expect(result.current.isAnyEditorModalOpen).toBe(false)
    }
    act(() => {
      result.current.setScript(P)
      result.current.setEditInfo({ ...P, url: 'https://sub.example/x' })
      result.current.setRemove(P)
      result.current.openImport('manual')
    })
    expect(result.current.script).toEqual(P)
    expect(result.current.editInfo?.url).toBe('https://sub.example/x')
    expect(result.current.remove).toEqual(P)
    expect(result.current.isAnyEditorModalOpen).toBe(false)
  })

  it('closes the context menu on a mousedown outside it only', () => {
    const inside = document.createElement('div')
    inside.className = 'ctxMenu'
    const item = document.createElement('button')
    inside.append(item)
    document.body.append(inside)
    try {
      const { result } = renderHook(() => useProfileModals())
      act(() => result.current.setMenu({ ...P, x: 10, y: 20 }))
      expect(result.current.menu).toEqual({ ...P, x: 10, y: 20 })

      act(() => {
        fireEvent.mouseDown(item)
      })
      expect(result.current.menu).not.toBeNull()

      act(() => {
        fireEvent.mouseDown(document.body)
      })
      expect(result.current.menu).toBeNull()
    } finally {
      inside.remove()
    }
  })

  it('removes the outside-click listener once the menu closes', () => {
    const remove = vi.spyOn(document, 'removeEventListener')
    try {
      const { result } = renderHook(() => useProfileModals())
      act(() => result.current.setMenu({ ...P, x: 0, y: 0 }))
      expect(remove).not.toHaveBeenCalledWith('mousedown', expect.anything())
      act(() => result.current.setMenu(null))
      expect(remove).toHaveBeenCalledWith('mousedown', expect.any(Function))
    } finally {
      remove.mockRestore()
    }
  })
})
