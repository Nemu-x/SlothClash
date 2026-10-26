import { fireEvent, screen, within } from '@testing-library/react'
import { useState } from 'react'
import { describe, expect, it, vi } from 'vitest'

import { renderWithProviders } from '../test/render'

import { type ImportMode, ImportProfileModal } from './ImportProfileModal'

const noop = () => undefined

function Harness({
  hidePaste,
  busy = false,
  onClose = noop,
  onStartDesyncOnly = noop,
}: {
  hidePaste?: boolean
  busy?: boolean
  onClose?: () => void
  onStartDesyncOnly?: () => void
}) {
  const [mode, setMode] = useState<ImportMode>('url')
  const [content, setContent] = useState('')
  return (
    <ImportProfileModal
      open
      title="Import subscription"
      blurb="Paste your provider link."
      mode={mode}
      url=""
      name=""
      content={content}
      busy={busy}
      hidePaste={hidePaste}
      onModeChange={setMode}
      onUrlChange={vi.fn()}
      onNameChange={vi.fn()}
      onContentChange={setContent}
      onPasteFromClipboard={vi.fn()}
      onClose={onClose}
      onSubmit={vi.fn()}
      onStartDesyncOnly={onStartDesyncOnly}
    />
  )
}

describe('ImportProfileModal: source switch', () => {
  it('has exactly two modes in one horizontal segmented control', () => {
    renderWithProviders(<Harness />)
    const tablist = screen.getByRole('tablist', { name: 'Import from' })
    // The Home Mode / Traffic control: one row, two slots, a sliding thumb.
    expect(tablist).toHaveClass('segmentInset', 'segmentInset2')
    expect(tablist.querySelector('.segmentGlider')).not.toBeNull()
    const tabs = within(tablist).getAllByRole('tab')
    expect(tabs.map((b) => b.textContent)).toEqual([
      'Subscription URL',
      'Paste / File',
    ])
    for (const tab of tabs) expect(tab).toHaveClass('segmentInsetBtn')
    expect(tabs[0]).toHaveAttribute('aria-selected', 'true')
    expect(tabs[0]).toHaveClass('isOn')
    expect(tabs[1]).toHaveAttribute('aria-selected', 'false')
    // Roving focus: only the selected tab is in the Tab order.
    expect(tabs[0]).toHaveAttribute('tabindex', '0')
    expect(tabs[1]).toHaveAttribute('tabindex', '-1')
    expect(screen.getByRole('tabpanel')).toHaveAttribute(
      'aria-labelledby',
      tabs[0].id,
    )
  })

  it('switches modes by click and by arrow keys', () => {
    renderWithProviders(<Harness />)
    const [url, paste] = screen.getAllByRole('tab')
    expect(
      screen.getByRole('textbox', { name: 'Subscription URL' }),
    ).toBeVisible()

    fireEvent.click(paste)
    expect(paste).toHaveAttribute('aria-selected', 'true')
    expect(screen.getByLabelText('Config or share links')).toBeVisible()
    expect(
      screen.queryByRole('textbox', { name: 'Subscription URL' }),
    ).toBeNull()
    const glider = document.querySelector('.segmentGlider') as HTMLElement
    expect(glider.style.getPropertyValue('--seg-i')).toBe('1')

    fireEvent.keyDown(paste, { key: 'ArrowRight' })
    expect(url).toHaveAttribute('aria-selected', 'true')
    expect(url).toHaveFocus()
    fireEvent.keyDown(url, { key: 'End' })
    expect(paste).toHaveAttribute('aria-selected', 'true')
    fireEvent.keyDown(paste, { key: 'Home' })
    expect(url).toHaveAttribute('aria-selected', 'true')
  })

  it('shows helper text, replaced by the detection result while pasting', () => {
    renderWithProviders(<Harness />)
    expect(
      screen.getByText('The https:// link from your provider.'),
    ).toBeVisible()
    fireEvent.click(screen.getAllByRole('tab')[1])
    const area = screen.getByLabelText('Config or share links')
    expect(area).toHaveAccessibleDescription(
      'A Clash / mihomo YAML config, or share links one per line.',
    )
    fireEvent.change(area, { target: { value: 'vless://a\nvless://b' } })
    expect(area).toHaveAccessibleDescription('2 share links')
    expect(screen.getByRole('button', { name: 'Import' })).toBeEnabled()
    fireEvent.change(area, { target: { value: 'nonsense' } })
    expect(area).toHaveAttribute('aria-invalid', 'true')
    expect(screen.getByRole('button', { name: 'Import' })).toBeDisabled()
  })

  it('focuses the URL field on open and closes on Escape', () => {
    const onClose = vi.fn()
    renderWithProviders(<Harness onClose={onClose} />)
    expect(
      screen.getByRole('textbox', { name: 'Subscription URL' }),
    ).toHaveFocus()
    fireEvent.keyDown(document, { key: 'Escape' })
    expect(onClose).toHaveBeenCalledTimes(1)
  })

  it('stays open on Escape while importing', () => {
    const onClose = vi.fn()
    renderWithProviders(<Harness busy onClose={onClose} />)
    fireEvent.keyDown(document, { key: 'Escape' })
    expect(onClose).not.toHaveBeenCalled()
  })
})

describe('ImportProfileModal: Desync-only line', () => {
  it('sits under the actions as a quiet link', () => {
    const start = vi.fn()
    renderWithProviders(<Harness onStartDesyncOnly={start} />)
    const link = screen.getByRole('button', { name: 'Desync only (no VPN)' })
    expect(link).toHaveClass('linkBtn')
    expect(link).not.toHaveClass('btn')
    const line = link.closest('.desyncAltLine')
    expect(line).toHaveTextContent('No VPN subscription?')
    // After the footer, i.e. below Cancel / Import.
    const footer = document.querySelector('.modalFooter') as HTMLElement
    expect(
      footer.compareDocumentPosition(line as Node) &
        Node.DOCUMENT_POSITION_FOLLOWING,
    ).toBeTruthy()
    fireEvent.click(link)
    expect(start).toHaveBeenCalledTimes(1)
  })

  it('is hidden with the paste mode when the brand hides local configs', () => {
    renderWithProviders(<Harness hidePaste />)
    expect(screen.queryByRole('tablist')).toBeNull()
    expect(
      screen.getByRole('textbox', { name: 'Subscription URL' }),
    ).toBeVisible()
    expect(screen.queryByText('No VPN subscription?')).toBeNull()
    expect(
      screen.queryByRole('button', { name: 'Desync only (no VPN)' }),
    ).toBeNull()
  })
})
