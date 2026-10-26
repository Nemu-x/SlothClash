import { fireEvent, render, screen } from '@testing-library/react'
import { useRef, useState } from 'react'
import { describe, expect, it, vi } from 'vitest'

import { useDialogFocus } from './useDialogFocus'

function Dialog({
  onEscape,
  autofocus = false,
}: {
  onEscape?: () => void
  autofocus?: boolean
}) {
  const ref = useRef<HTMLDivElement>(null)
  useDialogFocus(ref, true, onEscape)
  return (
    <div ref={ref} role="dialog" aria-label="Test" tabIndex={-1}>
      <button type="button">first</button>
      <input aria-label="field" data-autofocus={autofocus || undefined} />
      <button type="button" disabled>
        disabled
      </button>
      <button type="button">last</button>
    </div>
  )
}

function Harness({ autofocus }: { autofocus?: boolean }) {
  const [open, setOpen] = useState(false)
  return (
    <>
      <button type="button" onClick={() => setOpen(true)}>
        opener
      </button>
      {open ? (
        <Dialog onEscape={() => setOpen(false)} autofocus={autofocus} />
      ) : null}
    </>
  )
}

describe('useDialogFocus', () => {
  it('focuses the dialog itself, or its data-autofocus element', () => {
    const { unmount } = render(<Dialog />)
    expect(screen.getByRole('dialog')).toHaveFocus()
    unmount()
    render(<Dialog autofocus />)
    expect(screen.getByLabelText('field')).toHaveFocus()
  })

  it('cycles Tab and Shift+Tab inside the dialog, skipping disabled controls', () => {
    render(<Dialog />)
    const first = screen.getByText('first')
    const last = screen.getByText('last')
    last.focus()
    fireEvent.keyDown(document, { key: 'Tab' })
    expect(first).toHaveFocus()
    fireEvent.keyDown(document, { key: 'Tab', shiftKey: true })
    expect(last).toHaveFocus()
    // From the dialog container, Shift+Tab wraps to the end too.
    screen.getByRole('dialog').focus()
    fireEvent.keyDown(document, { key: 'Tab', shiftKey: true })
    expect(last).toHaveFocus()
  })

  it('closes on Escape and returns focus to the opener', () => {
    render(<Harness />)
    const opener = screen.getByText('opener')
    opener.focus()
    fireEvent.click(opener)
    expect(screen.getByRole('dialog')).toHaveFocus()
    fireEvent.keyDown(document, { key: 'Escape' })
    expect(screen.queryByRole('dialog')).toBeNull()
    expect(opener).toHaveFocus()
  })

  it('ignores Escape while no handler is given (a save in flight)', () => {
    const onEscape = vi.fn()
    const { rerender } = render(<Dialog />)
    fireEvent.keyDown(document, { key: 'Escape' })
    rerender(<Dialog onEscape={onEscape} />)
    fireEvent.keyDown(document, { key: 'Escape' })
    expect(onEscape).toHaveBeenCalledTimes(1)
  })
})
