import { fireEvent, screen, waitFor } from '@testing-library/react'
import { describe, expect, it, vi } from 'vitest'

import { ProfileContextMenu } from '../components/ProfileContextMenu'
import { ProfileModalsHost } from '../components/ProfileModalsHost'
import { useHomeViewModel } from '../hooks/useHomeViewModel'
import { useProfileModals } from '../hooks/useProfileModals'
import {
  desyncAuto,
  desyncService,
  desyncSnapshot,
  desyncStatus,
} from '../test/desyncFixtures'
import { renderWithProviders } from '../test/render'
import { wailsApp } from '../test/wailsMock'
import { type DesyncSnapshot, desyncServiceViews } from '../utils/desyncStatus'

import { HomePage } from './Home'
import { ProfilesPage } from './Profiles'
import { ProxiesPage } from './Proxies'

const noop = () => undefined

const DESYNC_ONLY = {
  id: 'p2',
  name: 'No VPN',
  type: 'local',
  kind: 'desync-only',
  lastUpdated: Math.floor(Date.now() / 1000) - 60,
}

// The App side: useProfileModals holds what is open.
function CreateHarness({
  onDesyncOnlyActivated,
}: {
  onDesyncOnlyActivated: () => void
}) {
  const modals = useProfileModals()
  return (
    <>
      <button type="button" onClick={() => modals.openImport('manual')}>
        open-import
      </button>
      <ProfileModalsHost
        modals={modals}
        profiles={[]}
        proxyGroups={[]}
        run={async (a) => {
          await a()
        }}
        refresh={async () => {}}
        setError={vi.fn()}
        setBanner={vi.fn()}
        pushToast={vi.fn(() => 1)}
        onImported={vi.fn()}
        onDesyncOnlyActivated={onDesyncOnlyActivated}
      />
    </>
  )
}

describe('Add profile dialog: Desync only', () => {
  it('creates a Desync-only profile without a URL', async () => {
    const create = wailsApp.stub('CreateDesyncOnlyProfile', () => ({
      profile: { activeProfileId: 'p2', profiles: [DESYNC_ONLY] },
    }))
    const importUrl = wailsApp.stub('ImportProfileFromURL', () => undefined)
    const activated = vi.fn()
    renderWithProviders(<CreateHarness onDesyncOnlyActivated={activated} />)

    fireEvent.click(screen.getByText('open-import'))
    fireEvent.click(screen.getByRole('tab', { name: 'Desync only (no VPN)' }))
    // No URL field and no clipboard paste: there is nothing to import.
    expect(screen.queryByPlaceholderText('https://…')).toBeNull()
    expect(
      screen.queryByRole('button', { name: 'Paste from clipboard' }),
    ).toBeNull()
    fireEvent.change(screen.getByLabelText(/Display name/), {
      target: { value: ' Free ' },
    })
    fireEvent.click(screen.getByRole('button', { name: 'Create' }))

    await waitFor(() => expect(create).toHaveBeenCalledWith('Free'))
    expect(importUrl).not.toHaveBeenCalled()
    // It became the active profile: the app is told so it can offer Desync.
    await waitFor(() => expect(activated).toHaveBeenCalledTimes(1))
  })
})

describe('Profiles page: Desync-only card', () => {
  it('shows the kind badge and no subscription status', () => {
    const refresh = vi.fn()
    renderWithProviders(
      <ProfilesPage
        profiles={[{ ...DESYNC_ONLY, lastUpdateError: 'stale error' }]}
        activeProfileId=""
        refreshBusyId={null}
        desyncEnabled={false}
        error={null}
        onImport={noop}
        onActivate={noop}
        onRefresh={refresh}
        onContextMenu={noop}
      />,
    )
    const badge = screen.getByText('Desync only')
    expect(badge.getAttribute('title')).toContain('No VPN servers')
    expect(screen.getByText('No VPN · Desync is off')).toBeTruthy()
    // Never an update status, never a refresh button.
    expect(screen.queryByText('Update failed')).toBeNull()
    expect(screen.queryByText('local')).toBeNull()
    const refreshButton = document.querySelector<HTMLButtonElement>(
      '.profileRefreshIcon',
    )
    expect(refreshButton?.hidden).toBe(true)
  })

  it('shows no update time (lastUpdated is its creation time)', () => {
    const subscription = {
      id: 'p1',
      name: 'Sub',
      type: 'subscription',
      url: 'https://example.com/sub',
      lastUpdated: Math.floor(Date.now() / 1000) - 120,
    }
    renderWithProviders(
      <ProfilesPage
        profiles={[subscription, DESYNC_ONLY]}
        activeProfileId="p2"
        refreshBusyId={null}
        desyncEnabled
        error={null}
        onImport={noop}
        onActivate={noop}
        onRefresh={noop}
        onContextMenu={noop}
      />,
    )
    const shown = [...document.querySelectorAll('.profileUpdated')].map(
      (el) => el.textContent,
    )
    // Only the subscription's time; the Desync-only card's 1m is not shown.
    expect(shown).toEqual(['2m ago'])
  })

  it('says when Desync is on', () => {
    renderWithProviders(
      <ProfilesPage
        profiles={[DESYNC_ONLY]}
        activeProfileId="p2"
        refreshBusyId={null}
        desyncEnabled
        error={null}
        onImport={noop}
        onActivate={noop}
        onRefresh={noop}
        onContextMenu={noop}
      />,
    )
    expect(screen.getByText('No VPN · Desync on')).toBeTruthy()
  })
})

describe('Profile context menu: Desync-only', () => {
  it('offers only the name and deletion', () => {
    renderWithProviders(
      <ProfileContextMenu
        target={{ id: 'p2', name: 'No VPN', x: 1, y: 1 }}
        profile={DESYNC_ONLY}
        onUpdate={noop}
        onEditInfo={noop}
        onCopyUrl={noop}
        onOpenRules={noop}
        onDelete={noop}
        onOpenExtendConfig={noop}
        onOpenProxyGroups={noop}
        onOpenScript={noop}
        onOpenEditFile={noop}
      />,
    )
    const items = [...document.querySelectorAll('.ctxItem')].map(
      (b) => b.textContent,
    )
    expect(items).toEqual(['Edit info', 'Delete profile'])
  })
})

const HOME_STATE = {
  profile: { activeProfileId: 'p2', profiles: [DESYNC_ONLY] },
  connection: { status: 'connected', health: 'ready' },
  mode: { current: 'rule' },
  traffic: 'proxy',
  proxy: {
    activeGroup: 'GLOBAL',
    groups: [
      {
        name: 'GLOBAL',
        type: 'Selector',
        proxies: ['DIRECT'],
        selected: 'DIRECT',
      },
    ],
  },
}

function DesyncOnlyHome({
  desync,
  onOpenDesync = noop,
}: {
  desync: DesyncSnapshot
  onOpenDesync?: () => void
}) {
  const vm = useHomeViewModel({
    state: HOME_STATE,
    connectBusy: false,
    optimisticMode: null,
    optimisticTraffic: null,
    updateSnap: null,
    desync,
  })
  return (
    <HomePage
      state={HOME_STATE}
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
      onConnectClick={noop}
      onInstallService={noop}
      onRefreshService={noop}
      onSelectGroup={noop}
      onSelectNode={noop}
      onToggleActiveNodeOpen={noop}
    />
  )
}

describe('Home: Desync-only profile', () => {
  it('has no node picker and no Global mode', () => {
    renderWithProviders(
      <DesyncOnlyHome
        desync={desyncSnapshot(desyncStatus({ desyncOnly: true }))}
      />,
    )
    expect(screen.getByText('Desync only · no VPN')).toBeTruthy()
    expect(screen.getByText('None · Desync routes only')).toBeTruthy()
    expect(screen.queryByLabelText('Active proxy group')).toBeNull()
    expect(screen.queryByRole('button', { name: 'Global' })).toBeNull()
    const rule = screen.getByRole('button', { name: 'Rule' })
    expect(rule.getAttribute('title')).toContain('Rule mode only')
    expect(screen.getByText('Rule only')).toBeTruthy()
  })

  it('says that Desync is off and opens its settings', () => {
    const open = vi.fn()
    renderWithProviders(
      <DesyncOnlyHome
        desync={desyncSnapshot(
          desyncStatus({ desyncOnly: true, settings: { enabled: false } }),
        )}
        onOpenDesync={open}
      />,
    )
    fireEvent.click(
      screen.getByText('Desync is off: all traffic goes directly. Turn it on'),
    )
    expect(open).toHaveBeenCalledTimes(1)
  })
})

describe('Proxies: Desync-only profile', () => {
  it('explains that there are no proxy servers', () => {
    renderWithProviders(
      <ProxiesPage
        groups={[{ name: 'GLOBAL', proxies: ['DIRECT'] }]}
        desyncOnly
        activeGroup=""
        connectionStatus="connected"
        displayMode="rule"
        showBuiltin={false}
        proxyDelayBusy={{}}
        proxyDelayMap={{}}
        proxyDelayErr={{}}
        error={null}
        onRefreshProxies={noop}
        onToggleShowBuiltin={noop}
        onSetMode={noop}
        onSelectGroup={noop}
        onSelectNode={noop}
        onPingAll={noop}
      />,
    )
    expect(screen.getByText(/This profile has no proxy servers/)).toBeTruthy()
    expect(screen.queryByRole('button', { name: 'Global' })).toBeNull()
  })
})

describe('Desync status lines without a reserve', () => {
  const ctx = { connected: true, mode: 'rule', traffic: 'tun' }

  it('words a failure without the subscription reserve', () => {
    const snap = desyncSnapshot(
      desyncStatus({ desyncOnly: true }),
      desyncAuto({
        youtube: desyncService('failed'),
        discord: desyncService('no_strategy'),
      }),
    )
    const [youtube, discord] = desyncServiceViews(snap, ctx)
    expect(youtube.status.phase).toBe('failed_direct')
    expect(youtube.status.tone).toBe('problem')
    expect(discord.status.phase).toBe('no_strategy')
    expect(discord.status.tone).toBe('problem')
  })

  it('keeps the reserve wording for a subscription profile', () => {
    const snap = desyncSnapshot(
      desyncStatus(),
      desyncAuto({ youtube: desyncService('failed') }),
    )
    expect(desyncServiceViews(snap, ctx)[0].status.phase).toBe('failed')
  })
})
