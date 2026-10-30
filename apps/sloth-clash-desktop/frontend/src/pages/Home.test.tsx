import { fireEvent, screen } from '@testing-library/react'
import { describe, expect, it, vi } from 'vitest'

import { useHomeViewModel } from '../hooks/useHomeViewModel'
import {
  desyncAuto,
  desyncService,
  desyncSnapshot,
  desyncStatus,
} from '../test/desyncFixtures'
import { renderWithProviders } from '../test/render'
import type { DesyncSnapshot } from '../utils/desyncStatus'

import { HomePage } from './Home'

const STATE = {
  profile: { activeProfileId: 'p1', profiles: [{ id: 'p1', name: 'Main' }] },
  connection: { status: 'connected', health: 'ready' },
  mode: { current: 'rule' },
  traffic: 'tun',
}

const noop = () => undefined

// The Home wiring from App.tsx: the real view-model fed the Desync snapshot.
function HomeScreen({
  desync,
  onOpenDesync,
  onStartDesyncOnly,
}: {
  desync: DesyncSnapshot | null
  onOpenDesync: () => void
  onStartDesyncOnly?: () => void
}) {
  const vm = useHomeViewModel({
    state: STATE,
    connectBusy: false,
    optimisticMode: null,
    optimisticTraffic: null,
    updateSnap: null,
    desync,
  })
  return (
    <HomePage
      state={STATE}
      vm={vm}
      service={null}
      linkToast=""
      error={null}
      updateSnap={null}
      connectBusy={false}
      homeActiveNodeOpen={false}
      homeActiveNodeRef={null}
      showBuiltinProxyGroups={false}
      onOpenOperator={noop}
      onOpenImport={noop}
      onOpenSupport={noop}
      onOpenUpdate={noop}
      onSetMode={noop}
      onSwitchTraffic={noop}
      onOpenDesync={onOpenDesync}
      onStartDesyncOnly={onStartDesyncOnly}
      onConnectClick={noop}
      onInstallService={noop}
      onRefreshService={noop}
      onSelectGroup={noop}
      onSelectNode={noop}
      onToggleActiveNodeOpen={noop}
    />
  )
}

function chip() {
  return document.querySelector<HTMLButtonElement>('.homeDesyncChip')
}

function marks() {
  return [
    ...document.querySelectorAll<HTMLElement>('.homeDesyncChipService'),
  ].map((el) => ({
    text: el.textContent,
    tone: el.getAttribute('data-tone'),
  }))
}

describe('Home Desync chip', () => {
  it('is hidden while Desync is off', () => {
    renderWithProviders(
      <HomeScreen
        desync={desyncSnapshot(desyncStatus({ settings: { enabled: false } }))}
        onOpenDesync={noop}
      />,
    )
    expect(chip()).toBeNull()
  })

  it('is hidden before the first Desync snapshot', () => {
    renderWithProviders(<HomeScreen desync={null} onOpenDesync={noop} />)
    expect(chip()).toBeNull()
  })

  it('shows a mark per enabled service with the full status in its tooltip', () => {
    renderWithProviders(
      <HomeScreen
        desync={desyncSnapshot(
          desyncStatus(),
          desyncAuto({
            youtube: desyncService('ready'),
            discord: desyncService('failed'),
          }),
        )}
        onOpenDesync={noop}
      />,
    )
    const el = chip()
    expect(el).not.toBeNull()
    expect(el).toHaveTextContent('Desync:YouTube✓·Discord!›')
    expect(marks()).toEqual([
      { text: 'YouTube✓', tone: 'ok' },
      { text: 'Discord!', tone: 'problem' },
    ])
    expect(el?.title).toContain('YouTube: HTTPS available · automatic')
    expect(el?.title).toContain(
      'Discord: Desync and VPN unavailable · retrying automatically',
    )
    expect(
      screen.getByRole('button', { name: /Desync status · open Desync/ }),
    ).toBe(el)
  })

  it('marks an in-progress check with an ellipsis', () => {
    renderWithProviders(
      <HomeScreen
        desync={desyncSnapshot(
          desyncStatus({ settings: { discord: false, discordUDP: false } }),
          desyncAuto({ youtube: desyncService('testing') }),
        )}
        onOpenDesync={noop}
      />,
    )
    expect(marks()).toEqual([{ text: 'YouTube…', tone: 'pending' }])
  })

  it('opens the Desync modal on click', () => {
    const open = vi.fn()
    renderWithProviders(
      <HomeScreen desync={desyncSnapshot()} onOpenDesync={open} />,
    )
    const el = chip()
    if (!el) throw new Error('chip not rendered')
    fireEvent.click(el)
    expect(open).toHaveBeenCalledTimes(1)
  })
})

const EMPTY_STATE = {
  profile: { activeProfileId: '', profiles: [] },
  connection: { status: 'disconnected' },
  mode: { current: 'rule' },
  traffic: 'proxy',
}

function EmptyHome({
  onOpenImport = noop,
  onStartDesyncOnly,
}: {
  onOpenImport?: (reason: 'beacon' | 'manual') => void
  onStartDesyncOnly?: () => void
}) {
  const vm = useHomeViewModel({
    state: EMPTY_STATE,
    connectBusy: false,
    optimisticMode: null,
    optimisticTraffic: null,
    updateSnap: null,
    desync: null,
  })
  return (
    <HomePage
      state={EMPTY_STATE}
      vm={vm}
      service={null}
      linkToast=""
      error={null}
      updateSnap={null}
      connectBusy={false}
      homeActiveNodeOpen={false}
      homeActiveNodeRef={null}
      showBuiltinProxyGroups={false}
      onOpenOperator={noop}
      onOpenImport={onOpenImport}
      onOpenSupport={noop}
      onOpenUpdate={noop}
      onSetMode={noop}
      onSwitchTraffic={noop}
      onStartDesyncOnly={onStartDesyncOnly}
      onConnectClick={noop}
      onInstallService={noop}
      onRefreshService={noop}
      onSelectGroup={noop}
      onSelectNode={noop}
      onToggleActiveNodeOpen={noop}
    />
  )
}

describe('Home without a profile', () => {
  it('offers Desync only as a quiet line under Add subscription', () => {
    const start = vi.fn()
    const openImport = vi.fn()
    renderWithProviders(
      <EmptyHome onOpenImport={openImport} onStartDesyncOnly={start} />,
    )
    const add = screen.getByRole('button', { name: 'Add subscription' })
    const link = screen.getByRole('button', {
      name: 'Unblock YouTube & Discord without VPN',
    })
    expect(link).toHaveClass('linkBtn')
    // Same column, right under the main action.
    expect(link.closest('.homeFirstRun')).toBe(add.parentElement)
    expect(link.closest('.desyncAltLine')).toHaveTextContent(
      'No VPN subscription?',
    )
    fireEvent.click(link)
    expect(start).toHaveBeenCalledTimes(1)
    expect(openImport).not.toHaveBeenCalled()
  })

  it('has no Desync-only line when the brand hides local configs', () => {
    renderWithProviders(<EmptyHome />)
    expect(
      screen.getByRole('button', { name: 'Add subscription' }),
    ).toBeVisible()
    expect(screen.queryByText('No VPN subscription?')).toBeNull()
  })

  it('has no Desync-only line once a profile exists', () => {
    renderWithProviders(
      <HomeScreen desync={null} onOpenDesync={noop} onStartDesyncOnly={noop} />,
    )
    expect(screen.queryByText('No VPN subscription?')).toBeNull()
  })
})
