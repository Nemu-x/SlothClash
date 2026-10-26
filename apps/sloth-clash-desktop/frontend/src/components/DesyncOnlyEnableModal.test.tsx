import { fireEvent, screen } from '@testing-library/react'
import { describe, expect, it, vi } from 'vitest'

import { renderWithProviders } from '../test/render'

import { DesyncOnlyEnableModal } from './DesyncOnlyEnableModal'

describe('DesyncOnlyEnableModal', () => {
  it('focuses Turn on Desync and closes on Escape', () => {
    const onClose = vi.fn()
    renderWithProviders(
      <DesyncOnlyEnableModal
        open
        busy={false}
        onClose={onClose}
        onEnable={vi.fn()}
      />,
    )
    expect(
      screen.getByRole('dialog', { name: 'Turn on Desync?' }),
    ).toHaveAccessibleDescription(/This profile has no VPN servers/)
    expect(screen.getByRole('button', { name: 'Turn on Desync' })).toHaveFocus()
    fireEvent.keyDown(document, { key: 'Escape' })
    expect(onClose).toHaveBeenCalledTimes(1)
  })

  it('stays while turning Desync on', () => {
    const onClose = vi.fn()
    renderWithProviders(
      <DesyncOnlyEnableModal open busy onClose={onClose} onEnable={vi.fn()} />,
    )
    fireEvent.keyDown(document, { key: 'Escape' })
    const overlay = document.querySelector('.modalOverlay') as HTMLElement
    fireEvent.mouseDown(overlay)
    fireEvent.click(overlay)
    expect(onClose).not.toHaveBeenCalled()
  })

  it('closes on a backdrop click, never on one that started inside', () => {
    const onClose = vi.fn()
    renderWithProviders(
      <DesyncOnlyEnableModal
        open
        busy={false}
        onClose={onClose}
        onEnable={vi.fn()}
      />,
    )
    const overlay = document.querySelector('.modalOverlay') as HTMLElement
    const dialog = screen.getByRole('dialog')
    // Inside the dialog.
    fireEvent.mouseDown(dialog)
    fireEvent.click(dialog)
    // Pressed inside, released on the backdrop (a dragged text selection).
    fireEvent.mouseDown(dialog)
    fireEvent.click(overlay)
    expect(onClose).not.toHaveBeenCalled()
    fireEvent.mouseDown(overlay)
    fireEvent.click(overlay)
    expect(onClose).toHaveBeenCalledTimes(1)
  })
})
