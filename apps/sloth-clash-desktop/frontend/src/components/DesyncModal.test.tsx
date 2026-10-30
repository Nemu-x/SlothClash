import { fireEvent, screen, waitFor, within } from '@testing-library/react'
import { useRef, useState } from 'react'
import { describe, expect, it, vi } from 'vitest'

import type { DesyncSettings, DesyncStatus } from '../api/desync'
import { useDesyncModal } from '../hooks/useDesyncModal'
import { useHomeViewModel } from '../hooks/useHomeViewModel'
import { HomePage } from '../pages/Home'
import { ProfilesPage } from '../pages/Profiles'
import {
  DESYNC_SERVICES,
  desyncAuto,
  desyncSettings,
  desyncSnapshot,
  desyncStatus,
} from '../test/desyncFixtures'
import { renderWithProviders } from '../test/render'
import { wailsApp } from '../test/wailsMock'

import { DesyncModal } from './DesyncModal'
import { DesyncSummaryCard } from './DesyncSummaryCard'

const CONNECTED = { connected: true, mode: 'rule', traffic: 'tun' }
const SUB = { id: 'p1', name: 'Sub', type: 'subscription' }
const NEW_ID = 'profile-20261026120000'

const noop = () => undefined

function stubStatus(status: DesyncStatus = desyncStatus()) {
  wailsApp.stub('GetDesyncStatus', () => status)
  wailsApp.stub('GetDesyncAutoStatus', () => desyncAuto())
}

/** What the onboarding starts from (NewDesyncDraft): a new profile's defaults. */
function stubDraft(over: Partial<DesyncSettings> = {}) {
  return wailsApp.stub('NewDesyncDraft', () =>
    desyncSettings({ enabled: false, discordUDP: false, ...over }),
  )
}

/** Every call that would save Desync settings for an existing profile. */
function stubSavingCalls() {
  return [
    'SetDesyncSettings',
    'AddDesyncCustomDomain',
    'AddDesyncService',
    'RemoveDesyncCustomDomain',
    'SetDesyncCustomDomainEnabled',
  ].map((name) => wailsApp.stub(name, () => desyncStatus()))
}

/** CreateDesyncOnlyProfile appending a profile; `activeId` = what it left active. */
function stubCreate(activeId = 'p1') {
  return wailsApp.stub('CreateDesyncOnlyProfile', (name: string) => ({
    profile: {
      activeProfileId: activeId,
      profiles: [
        SUB,
        {
          id: NEW_ID,
          name: name || 'Desync only',
          type: 'local',
          kind: 'desync-only',
        },
      ],
    },
  }))
}

function renderModal(
  view: 'manage' | 'onboarding',
  over: Partial<Parameters<typeof DesyncModal>[0]> = {},
) {
  const props = {
    refresh: vi.fn(async () => {}),
    onClose: vi.fn(),
    onCreated: vi.fn(),
    ...over,
  }
  renderWithProviders(
    <DesyncModal
      view={view}
      {...CONNECTED}
      profiles={[SUB]}
      refresh={props.refresh}
      onClose={props.onClose}
      onCreated={props.onCreated}
    />,
  )
  return props
}

const createButton = () =>
  screen.getByRole('button', { name: 'Create profile' })

describe('DesyncModal: manage', () => {
  it('is a labelled dialog with the full controls', async () => {
    stubStatus()
    renderModal('manage')
    const dialog = screen.getByRole('dialog', { name: 'Desync' })
    expect(dialog).toHaveAttribute('aria-modal', 'true')
    // A subscription profile: Desync takes the services off the VPN.
    await waitFor(() =>
      expect(dialog).toHaveAccessibleDescription(
        "YouTube, Discord and the sites you add go directly through Desync, outside your VPN. Where Desync doesn't help, they go through the VPN.",
      ),
    )
    await screen.findByRole('switch', { name: 'YouTube' })
    expect(screen.getByRole('switch', { name: 'Enable Desync' })).toBeVisible()
    expect(screen.getByRole('button', { name: 'Check network' })).toBeVisible()
    expect(
      screen.getByRole('button', { name: 'Copy diagnostics' }),
    ).toBeVisible()
    // Live status chips are shown here.
    expect(document.querySelectorAll('.desyncChip').length).toBeGreaterThan(0)
  })

  it('a Desync-only profile: unblocks without a VPN, no master switch', async () => {
    stubStatus(desyncStatus({ desyncOnly: true }))
    renderModal('manage')
    const dialog = screen.getByRole('dialog', { name: 'Desync' })
    await screen.findByRole('switch', { name: 'YouTube' })
    expect(dialog).toHaveAccessibleDescription(
      'Unblocks YouTube, Discord and the sites you add without a VPN. Everything else connects directly.',
    )
    expect(screen.queryByRole('switch', { name: 'Enable Desync' })).toBeNull()
  })

  it('closes with Close, Escape and a backdrop click', async () => {
    stubStatus()
    const props = renderModal('manage')
    await screen.findByRole('switch', { name: 'YouTube' })
    fireEvent.click(screen.getByRole('button', { name: 'Close' }))
    fireEvent.keyDown(document, { key: 'Escape' })
    const overlay = document.querySelector('.modalOverlay') as HTMLElement
    fireEvent.mouseDown(overlay)
    fireEvent.click(overlay)
    expect(props.onClose).toHaveBeenCalledTimes(3)
  })

  it('a click inside the dialog never closes it', async () => {
    stubStatus()
    const props = renderModal('manage')
    const dialog = screen.getByRole('dialog')
    fireEvent.mouseDown(dialog)
    fireEvent.click(dialog)
    // A press that started inside and ended on the backdrop (text selection).
    fireEvent.mouseDown(dialog)
    fireEvent.click(document.querySelector('.modalOverlay') as HTMLElement)
    expect(props.onClose).not.toHaveBeenCalled()
  })
})

describe('DesyncModal: manage voice', () => {
  it('Turn on TUN goes through the app traffic switch', async () => {
    stubStatus()
    const onEnableTun = vi.fn(async () => '')
    renderWithProviders(
      <DesyncModal
        view="manage"
        {...CONNECTED}
        traffic="proxy"
        profiles={[SUB]}
        refresh={async () => {}}
        onClose={noop}
        onCreated={noop}
        onEnableTun={onEnableTun}
      />,
    )
    fireEvent.click(await screen.findByRole('button', { name: 'Turn on TUN' }))
    await waitFor(() => expect(onEnableTun).toHaveBeenCalledTimes(1))
  })
})

describe('DesyncModal: onboarding', () => {
  it('asks only what to unblock: no master switch, status or diagnostics', async () => {
    stubStatus(desyncStatus({ settings: { enabled: false } }))
    stubDraft()
    renderModal('onboarding')
    expect(
      screen.getByRole('dialog', { name: 'Unblock without VPN' }),
    ).toBeVisible()
    await screen.findByRole('switch', { name: 'YouTube' })
    expect(screen.queryByRole('switch', { name: 'Enable Desync' })).toBeNull()
    expect(screen.queryByRole('button', { name: 'Check network' })).toBeNull()
    expect(
      screen.queryByRole('button', { name: 'Copy diagnostics' }),
    ).toBeNull()
    expect(document.querySelector('.desyncChip')).toBeNull()
    expect(screen.getByText('Videos and the website')).toBeVisible()
    expect(screen.getByLabelText('Domain')).toBeVisible()
    // No strategies, capabilities or voice actions while choosing.
    expect(wailsApp.fn('GetDesyncCapabilities')).not.toHaveBeenCalled()
    expect(document.querySelector('.desyncCaps')).toBeNull()
    expect(screen.queryByRole('button', { name: 'Reconnect voice' })).toBeNull()
  })

  it('Create makes a Desync-only profile with the selection and activates it', async () => {
    // The active profile has Desync off; the draft starts from the defaults.
    stubStatus(
      desyncStatus({ udpCoreSupported: true, settings: { enabled: false } }),
    )
    stubDraft({ youtube: true, discord: true, discordUDP: false })
    const [set] = stubSavingCalls()
    const create = stubCreate('p1')
    const activate = wailsApp.stub('ActivateProfile', () => ({}))
    const props = renderModal('onboarding')
    await screen.findByRole('switch', { name: 'YouTube' })

    fireEvent.change(screen.getByLabelText(/Profile name/), {
      target: { value: '  Home  ' },
    })
    fireEvent.click(createButton())

    await waitFor(() => expect(props.onClose).toHaveBeenCalledTimes(1))
    // Same patch as the master switch: Discord gets voice when supported.
    // The settings go into the new profile; the active one is not touched.
    expect(create).toHaveBeenCalledWith('Home', {
      ...desyncSettings({ enabled: false, discordUDP: false }),
      enabled: true,
      discord: true,
      discordUDP: true,
    })
    expect(set).not.toHaveBeenCalled()
    expect(activate).toHaveBeenCalledWith(NEW_ID)
    expect(create.mock.invocationCallOrder[0]).toBeLessThan(
      activate.mock.invocationCallOrder[0],
    )
    expect(props.refresh).toHaveBeenCalled()
    expect(props.onCreated).toHaveBeenCalledWith('Home')
  })

  it('keeps the choices locally and saves them in one call on Create', async () => {
    stubStatus(desyncStatus({ settings: { enabled: false } }))
    stubDraft({ youtube: true, discord: false })
    const saving = stubSavingCalls()
    const addSite = wailsApp.stub(
      'DraftDesyncAddCustomDomain',
      (draft: DesyncSettings, domain: string) => ({
        ...draft,
        custom: [
          ...(draft.custom ?? []),
          { id: 'a1b2c3d4', domain, enabled: true },
        ],
      }),
    )
    const create = stubCreate('p1')
    wailsApp.stub('ActivateProfile', () => ({}))
    const props = renderModal('onboarding')
    await screen.findByRole('switch', { name: 'YouTube' })

    fireEvent.click(screen.getByRole('switch', { name: 'YouTube' }))
    await waitFor(() =>
      expect(screen.getByRole('switch', { name: 'YouTube' })).toHaveAttribute(
        'aria-checked',
        'false',
      ),
    )
    fireEvent.change(screen.getByLabelText('Domain'), {
      target: { value: 'example.org' },
    })
    fireEvent.click(screen.getByRole('button', { name: 'Add' }))
    await screen.findByText('example.org')
    expect(addSite).toHaveBeenCalledWith(
      expect.objectContaining({ youtube: false }),
      'example.org',
    )
    // Turn the new site off and on again: still nothing saved.
    const site = screen.getByRole('switch', { name: 'example.org' })
    fireEvent.click(site)
    await waitFor(() => expect(site).toHaveAttribute('aria-checked', 'false'))
    fireEvent.click(site)
    await waitFor(() => expect(site).toHaveAttribute('aria-checked', 'true'))
    for (const call of saving) expect(call).not.toHaveBeenCalled()

    // Enabled again once the last edit settled.
    await waitFor(() => expect(createButton()).toBeEnabled())
    fireEvent.click(createButton())
    await waitFor(() => expect(props.onClose).toHaveBeenCalledTimes(1))
    expect(create).toHaveBeenCalledTimes(1)
    expect(create).toHaveBeenCalledWith(
      'Desync only',
      expect.objectContaining({
        enabled: true,
        youtube: false,
        custom: [{ id: 'a1b2c3d4', domain: 'example.org', enabled: true }],
      }),
    )
    for (const call of saving) expect(call).not.toHaveBeenCalled()
    // An empty name gets the localized default.
    expect(props.onCreated).toHaveBeenCalledWith('Desync only')
  })

  it('does not activate again a profile the backend already made active', async () => {
    stubStatus(desyncStatus())
    stubDraft()
    stubCreate(NEW_ID)
    const activate = wailsApp.stub('ActivateProfile', () => ({}))
    const props = renderModal('onboarding')
    await screen.findByRole('switch', { name: 'YouTube' })
    fireEvent.click(createButton())
    await waitFor(() => expect(props.onClose).toHaveBeenCalledTimes(1))
    expect(activate).not.toHaveBeenCalled()
  })

  it('activates the new profile, not an older Desync-only one', async () => {
    stubStatus(desyncStatus())
    stubDraft()
    wailsApp.stub('CreateDesyncOnlyProfile', () => ({
      profile: {
        activeProfileId: 'p1',
        profiles: [
          SUB,
          // Not in the (stale) list the dialog was given either.
          { id: 'old', name: 'Old', type: 'local', kind: 'desync-only' },
          { id: NEW_ID, name: 'New', type: 'local', kind: 'desync-only' },
        ],
      },
    }))
    const activate = wailsApp.stub('ActivateProfile', () => ({}))
    const props = renderModal('onboarding')
    await screen.findByRole('switch', { name: 'YouTube' })
    fireEvent.click(createButton())
    await waitFor(() => expect(props.onClose).toHaveBeenCalledTimes(1))
    expect(activate).toHaveBeenCalledWith(NEW_ID)
    expect(props.onCreated).toHaveBeenCalledWith('New')
  })

  it('Cancel after changing the choices changes nothing', async () => {
    stubStatus(desyncStatus({ settings: { enabled: false } }))
    stubDraft({ youtube: true, discord: false })
    const saving = stubSavingCalls()
    wailsApp.stub('DraftDesyncAddCustomDomain', (draft: DesyncSettings) => ({
      ...draft,
      custom: [{ id: 'a1b2c3d4', domain: 'example.org', enabled: true }],
    }))
    const create = stubCreate()
    const props = renderModal('onboarding')
    await screen.findByRole('switch', { name: 'YouTube' })
    for (const name of ['YouTube', 'Discord']) {
      const before = screen.getByRole('switch', { name }).ariaChecked
      fireEvent.click(screen.getByRole('switch', { name }))
      await waitFor(() =>
        expect(screen.getByRole('switch', { name }).ariaChecked).not.toBe(
          before,
        ),
      )
    }
    fireEvent.change(screen.getByLabelText('Domain'), {
      target: { value: 'example.org' },
    })
    fireEvent.click(screen.getByRole('button', { name: 'Add' }))
    await screen.findByText('example.org')
    fireEvent.click(screen.getByRole('button', { name: 'Remove example.org' }))
    await waitFor(() => expect(screen.queryByText('example.org')).toBeNull())

    fireEvent.click(screen.getByRole('button', { name: 'Cancel' }))
    expect(props.onClose).toHaveBeenCalledTimes(1)
    expect(create).not.toHaveBeenCalled()
    for (const call of saving) expect(call).not.toHaveBeenCalled()
  })

  it('starts from the defaults, not from the active profile', async () => {
    stubStatus(
      desyncStatus({
        settings: {
          youtube: false,
          discord: false,
          discordUDP: false,
          custom: [{ id: 'a1', domain: 'example.org', enabled: true }],
        },
      }),
    )
    stubDraft({ youtube: true, discord: false })
    renderModal('onboarding')
    expect(
      await screen.findByRole('switch', { name: 'YouTube' }),
    ).toHaveAttribute('aria-checked', 'true')
    expect(screen.queryByText('example.org')).toBeNull()
  })

  it('cannot create without a service or a site', async () => {
    stubStatus(desyncStatus({ settings: { enabled: false } }))
    stubDraft({ youtube: false, discord: false, discordUDP: false })
    const create = stubCreate()
    renderModal('onboarding')
    await screen.findByRole('switch', { name: 'YouTube' })
    expect(createButton()).toBeDisabled()
    expect(screen.getByText('Turn on a service or add a site.')).toBeVisible()
    fireEvent.click(createButton())
    expect(create).not.toHaveBeenCalled()
  })

  it('a failed activation keeps the dialog; a retry does not create twice', async () => {
    stubStatus(desyncStatus())
    stubDraft()
    const create = stubCreate('p1')
    const activate = wailsApp.stub('ActivateProfile', () =>
      Promise.reject(new Error('core start failed')),
    )
    const props = renderModal('onboarding')
    await screen.findByRole('switch', { name: 'YouTube' })
    fireEvent.click(createButton())
    expect(await screen.findByRole('alert')).toHaveTextContent(
      'core start failed',
    )
    expect(props.onClose).not.toHaveBeenCalled()

    activate.mockImplementation(() => Promise.resolve({}))
    fireEvent.click(createButton())
    await waitFor(() => expect(props.onClose).toHaveBeenCalledTimes(1))
    expect(create).toHaveBeenCalledTimes(1)
    expect(activate).toHaveBeenCalledTimes(2)
  })

  it('a refused creation keeps the dialog and shows the reason', async () => {
    stubStatus(desyncStatus({ settings: { enabled: false } }))
    stubDraft()
    const activate = wailsApp.stub('ActivateProfile', () => ({}))
    wailsApp.stub('CreateDesyncOnlyProfile', () =>
      Promise.reject(new Error('the embedded core does not support desync')),
    )
    const props = renderModal('onboarding')
    await screen.findByRole('switch', { name: 'YouTube' })
    fireEvent.click(createButton())
    expect(await screen.findByRole('alert')).toHaveTextContent(
      'does not support desync',
    )
    expect(activate).not.toHaveBeenCalled()
    expect(props.onClose).not.toHaveBeenCalled()
  })
})

// The App wiring: every entry point opens the modal through useDesyncModal.
function ModalHost({
  onActivate = noop,
  profilesActive = 'p1',
}: {
  onActivate?: (id: string) => void
  profilesActive?: string
}) {
  const modal = useDesyncModal()
  return (
    <>
      <EntryPoints
        onOpen={modal.openManage}
        onActivate={onActivate}
        profilesActive={profilesActive}
      />
      {modal.view ? (
        <DesyncModal
          view={modal.view}
          {...CONNECTED}
          profiles={[]}
          refresh={async () => {}}
          onClose={modal.close}
          onCreated={noop}
        />
      ) : null}
    </>
  )
}

/**
 * App on an empty Home: "No VPN subscription?" opens the onboarding, and
 * Create replaces the empty Home (and with it the link) by the new profile's.
 */
function EmptyHomeHost({ withFallback }: { withFallback: boolean }) {
  const modal = useDesyncModal()
  const connectRef = useRef<HTMLButtonElement>(null)
  const [profiles, setProfiles] = useState<any[]>([])
  const state = {
    profile: { activeProfileId: profiles[0]?.id ?? '', profiles },
    connection: { status: 'disconnected' },
    mode: { current: 'rule' },
    traffic: 'tun',
  }
  const vm = useHomeViewModel({
    state,
    connectBusy: false,
    optimisticMode: null,
    optimisticTraffic: null,
    updateSnap: null,
  })
  return (
    <>
      <HomePage
        state={state}
        vm={vm}
        service={null}
        linkToast=""
        error={null}
        updateSnap={null}
        connectBusy={false}
        homeActiveNodeOpen={false}
        homeActiveNodeRef={null}
        connectButtonRef={connectRef}
        showBuiltinProxyGroups={false}
        onOpenOperator={noop}
        onOpenImport={noop}
        onOpenSupport={noop}
        onOpenUpdate={noop}
        onSetMode={noop}
        onSwitchTraffic={noop}
        onStartDesyncOnly={modal.openOnboarding}
        onConnectClick={noop}
        onInstallService={noop}
        onRefreshService={noop}
        onSelectGroup={noop}
        onSelectNode={noop}
        onToggleActiveNodeOpen={noop}
      />
      {modal.view ? (
        <DesyncModal
          view={modal.view}
          connected={false}
          mode="rule"
          traffic="tun"
          profiles={profiles}
          refresh={async () =>
            setProfiles([
              {
                id: NEW_ID,
                name: 'Desync only',
                type: 'local',
                kind: 'desync-only',
              },
            ])
          }
          onClose={modal.close}
          onCreated={noop}
          fallbackFocus={withFallback ? () => connectRef.current : undefined}
        />
      ) : null}
    </>
  )
}

describe('DesyncModal: focus after creating from an empty Home', () => {
  async function createFromEmptyHome(withFallback: boolean) {
    stubStatus(desyncStatus())
    stubDraft()
    stubCreate(NEW_ID)
    renderWithProviders(<EmptyHomeHost withFallback={withFallback} />)
    const link = screen.getByRole('button', {
      name: /Unblock YouTube & Discord without VPN/,
    })
    link.focus()
    fireEvent.click(link)
    await screen.findByRole('switch', { name: 'YouTube' })
    fireEvent.click(createButton())
    await waitFor(() => expect(screen.queryByRole('dialog')).toBeNull())
    // The link that opened the dialog is gone with the empty Home.
    expect(link.isConnected).toBe(false)
  }

  it('lands on Connect, not on the page body', async () => {
    await createFromEmptyHome(true)
    expect(screen.getByRole('button', { name: 'Connect' })).toHaveFocus()
  })

  it('without a fallback focus would be lost (the case it fixes)', async () => {
    await createFromEmptyHome(false)
    expect(document.activeElement).toBe(document.body)
  })
})

const HOME_STATE = {
  profile: { activeProfileId: 'p1', profiles: [SUB] },
  connection: { status: 'connected', health: 'ready' },
  mode: { current: 'rule' },
  traffic: 'tun',
}

function EntryPoints({
  onOpen,
  onActivate,
  profilesActive,
}: {
  onOpen: () => void
  onActivate: (id: string) => void
  /** The active profile on the Profiles page. */
  profilesActive: string
}) {
  const vm = useHomeViewModel({
    state: HOME_STATE,
    connectBusy: false,
    optimisticMode: null,
    optimisticTraffic: null,
    updateSnap: null,
    desync: desyncSnapshot(),
  })
  return (
    <>
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
        onOpenDesync={onOpen}
        onConnectClick={noop}
        onInstallService={noop}
        onRefreshService={noop}
        onSelectGroup={noop}
        onSelectNode={noop}
        onToggleActiveNodeOpen={noop}
      />
      <DesyncSummaryCard {...CONNECTED} onOpen={onOpen} />
      <ProfilesPage
        profiles={[
          SUB,
          { id: 'p2', name: 'No VPN', type: 'local', kind: 'desync-only' },
        ]}
        activeProfileId={profilesActive}
        refreshBusyId={null}
        error={null}
        onImport={noop}
        onActivate={onActivate}
        onRefresh={noop}
        onContextMenu={noop}
        onOpenDesync={onOpen}
      />
    </>
  )
}

describe('Desync modal entry points', () => {
  it('the Home chip opens the modal', async () => {
    stubStatus()
    renderWithProviders(<ModalHost />)
    expect(screen.queryByRole('dialog')).toBeNull()
    const chip = document.querySelector('.homeDesyncChip') as HTMLElement
    chip.focus()
    fireEvent.click(chip)
    expect(screen.getByRole('dialog', { name: 'Desync' })).toBeVisible()
    // Escape closes it and gives focus back to the chip.
    fireEvent.keyDown(document, { key: 'Escape' })
    expect(screen.queryByRole('dialog')).toBeNull()
    expect(chip).toHaveFocus()
  })

  it('the Settings summary shows the state and opens the modal', async () => {
    stubStatus(
      desyncStatus({
        settings: {
          custom: [
            { id: 'a1', domain: 'example.org', enabled: true },
            { id: 'a2', domain: 'example.net', enabled: false },
          ],
        },
      }),
    )
    renderWithProviders(<ModalHost />)
    const card = screen.getByRole('group', { name: 'Desync' })
    await within(card).findByText('YouTube, Discord, Sites 1')
    expect(within(card).getByText('On')).toBeVisible()
    fireEvent.click(within(card).getByRole('button', { name: 'Open' }))
    expect(screen.getByRole('dialog', { name: 'Desync' })).toBeVisible()
  })

  it('the Settings summary names switched-on services', async () => {
    stubStatus(
      desyncStatus({
        settings: {
          custom: [
            { id: 'a1', domain: 'example.org', enabled: true },
            { id: 'ig', domain: '', service: 'instagram', enabled: true },
            { id: 'a2', domain: 'example.net', enabled: true },
            { id: 'fb', domain: '', service: 'facebook', enabled: true },
            { id: 'xx', domain: '', service: 'x', enabled: false },
          ],
        },
      }),
    )
    wailsApp.stub('ListDesyncServices', () => DESYNC_SERVICES)
    renderWithProviders(
      <DesyncSummaryCard {...CONNECTED} onOpen={() => undefined} />,
    )
    const card = screen.getByRole('group', { name: 'Desync' })
    expect(
      await within(card).findByText(
        'YouTube, Discord, Facebook, Instagram, Sites 2',
      ),
    ).toBeVisible()
  })

  it('the active Desync-only profile card opens the modal without activating', async () => {
    stubStatus()
    const activate = vi.fn()
    renderWithProviders(<ModalHost onActivate={activate} profilesActive="p2" />)
    const link = screen.getByRole('button', { name: 'Desync settings' })
    // Enter on the link must not reach the card (that activates it).
    fireEvent.keyDown(link, { key: 'Enter' })
    fireEvent.click(link)
    expect(screen.getByRole('dialog', { name: 'Desync' })).toBeVisible()
    expect(activate).not.toHaveBeenCalled()
  })
})
