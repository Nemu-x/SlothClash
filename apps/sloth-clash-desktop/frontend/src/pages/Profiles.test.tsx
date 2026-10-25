import { screen } from '@testing-library/react'
import { describe, expect, it } from 'vitest'

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
    expect(badge.getAttribute('title')).toBe(
      'The last subscription update failed (5m ago)\nsubscription HTTP 403: forbidden',
    )
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
