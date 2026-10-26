import { fireEvent, screen } from '@testing-library/react'
import { describe, expect, it, vi } from 'vitest'

import { renderWithProviders } from '../test/render'

import { ProfilesPage } from './Profiles'

const noop = () => {}

function renderProfiles(profiles: any[]) {
  return renderWithProviders(
    <ProfilesPage
      profiles={profiles}
      activeProfileId=""
      refreshBusyId={null}
      error={null}
      onImport={noop}
      onActivate={noop}
      onRefresh={noop}
      onContextMenu={noop}
    />,
  )
}

const nowUnix = () => Math.floor(Date.now() / 1000)

describe('ProfilesPage update status badge', () => {
  it('shows the badge with the attempt time and the error in its tooltip', () => {
    renderProfiles([
      {
        id: 'p1',
        name: 'Stale',
        type: 'subscription',
        url: 'https://sub.example.com/s/token',
        lastUpdated: nowUnix() - 3 * 3600,
        lastUpdateAttempt: nowUnix() - 5 * 60,
        lastUpdateError: 'subscription HTTP 403: forbidden',
      },
    ])
    const badge = screen.getByText('Update failed')
    const label =
      'The last subscription update failed (5m ago)\nsubscription HTTP 403: forbidden'
    expect(badge.getAttribute('title')).toBe(label)
    expect(badge.getAttribute('aria-label')).toBe(label)
    // Not a live region: a list of failing cards must not announce each one.
    expect(badge.getAttribute('role')).toBeNull()
    expect(screen.queryByRole('status')).toBeNull()
    // "updated X ago" keeps showing the last real update, not the attempt.
    expect(screen.getByText('3h ago')).toBeTruthy()
  })

  it('falls back to a timeless title when no attempt time is known', () => {
    renderProfiles([
      {
        id: 'p1',
        name: 'Stale',
        type: 'subscription',
        url: 'https://sub.example.com/s/token',
        lastUpdateError: 'empty subscription body',
      },
    ])
    expect(screen.getByText('Update failed').getAttribute('title')).toBe(
      'The last subscription update failed\nempty subscription body',
    )
  })

  it('shows no badge without an update error', () => {
    renderProfiles([
      {
        id: 'p1',
        name: 'Healthy',
        type: 'subscription',
        url: 'https://sub.example.com/s/token',
        lastUpdated: nowUnix() - 60,
        lastUpdateAttempt: nowUnix() - 60,
        lastUpdateError: '   ',
      },
    ])
    expect(screen.getByText('Healthy')).toBeTruthy()
    expect(screen.queryByText('Update failed')).toBeNull()
  })
})

describe('ProfilesPage keyboard', () => {
  const SUB = {
    id: 'p1',
    name: 'Sub',
    type: 'subscription',
    url: 'https://sub.example.com/s/token',
  }

  function renderWith(onActivate = vi.fn(), onRefresh = vi.fn()) {
    renderWithProviders(
      <ProfilesPage
        profiles={[SUB]}
        activeProfileId=""
        refreshBusyId={null}
        error={null}
        onImport={noop}
        onActivate={onActivate}
        onRefresh={onRefresh}
        onContextMenu={noop}
      />,
    )
    return { onActivate, onRefresh }
  }

  it('Enter and Space on the card activate it', () => {
    const { onActivate } = renderWith()
    const card = screen.getByText('Sub').closest('.profileCard') as HTMLElement
    // Handled (default prevented: Space must not scroll the page).
    expect(fireEvent.keyDown(card, { key: 'Enter' })).toBe(false)
    expect(fireEvent.keyDown(card, { key: ' ' })).toBe(false)
    expect(onActivate).toHaveBeenCalledTimes(2)
    expect(onActivate).toHaveBeenCalledWith('p1')
  })

  it('Enter and Space on the refresh button are left to the button', () => {
    const { onActivate } = renderWith()
    const refresh = screen.getByRole('button', {
      name: 'Refresh subscription',
    })
    // Not prevented, so the browser turns them into the button's click.
    expect(fireEvent.keyDown(refresh, { key: 'Enter' })).toBe(true)
    expect(fireEvent.keyDown(refresh, { key: ' ' })).toBe(true)
    expect(onActivate).not.toHaveBeenCalled()
  })
})
