import { fireEvent, screen, waitFor, within } from '@testing-library/react'
import { afterEach, describe, expect, it, vi } from 'vitest'

import type { DesyncAutoStatus, DesyncStatus } from '../api/desync'
import { desyncAuto, desyncService, desyncStatus } from '../test/desyncFixtures'
import { renderWithProviders } from '../test/render'
import { wailsApp } from '../test/wailsMock'

import { DesyncSettingsCard } from './DesyncSettingsCard'

type Ctx = { connected: boolean; mode: string; traffic: string }
const CONNECTED: Ctx = { connected: true, mode: 'rule', traffic: 'tun' }

function stubBackend(
  status: DesyncStatus = desyncStatus(),
  auto: DesyncAutoStatus = desyncAuto(),
) {
  return {
    getStatus: wailsApp.stub('GetDesyncStatus', () => status),
    getAuto: wailsApp.stub('GetDesyncAutoStatus', () => auto),
  }
}

async function renderCard(ctx: Ctx = CONNECTED) {
  renderWithProviders(<DesyncSettingsCard {...ctx} />)
  // Rows render once the first poll lands.
  await screen.findByRole('switch', { name: 'YouTube' })
}

function serviceRow(service: 'youtube' | 'discord') {
  const row = document.querySelector<HTMLElement>(
    `.desyncService[data-service="${service}"]`,
  )
  if (!row) throw new Error(`row ${service} not rendered`)
  return row
}

function chips(service: 'youtube' | 'discord') {
  return [...serviceRow(service).querySelectorAll<HTMLElement>('.desyncChip')]
}

describe('DesyncSettingsCard: controls', () => {
  it('uses switches for the master toggle and every service, no checkboxes', async () => {
    stubBackend()
    await renderCard()

    const master = screen.getByRole('switch', { name: 'Enable Desync' })
    const youtube = screen.getByRole('switch', { name: 'YouTube' })
    const discord = screen.getByRole('switch', { name: 'Discord' })
    for (const sw of [master, youtube, discord]) {
      expect(sw).toHaveAttribute('aria-checked', 'true')
      expect(sw).toHaveClass('settingsSwitch', 'on')
    }
    expect(screen.queryAllByRole('checkbox')).toHaveLength(0)
    expect(document.querySelector('input[type="checkbox"]')).toBeNull()
  })

  it('shows an unchecked service as an off switch', async () => {
    stubBackend(desyncStatus({ settings: { youtube: false } }))
    await renderCard()
    const youtube = screen.getByRole('switch', { name: 'YouTube' })
    expect(youtube).toHaveAttribute('aria-checked', 'false')
    expect(youtube).not.toHaveClass('on')
  })

  it('toggling YouTube saves the full settings with youtube flipped', async () => {
    const status = desyncStatus()
    stubBackend(status)
    const set = wailsApp.stub('SetDesyncSettings', (s) =>
      desyncStatus({ settings: s }),
    )
    await renderCard()

    fireEvent.click(screen.getByRole('switch', { name: 'YouTube' }))
    await waitFor(() => expect(set).toHaveBeenCalledTimes(1))
    expect(set).toHaveBeenCalledWith({ ...status.settings, youtube: false })
    // The saved result is reflected at once.
    await waitFor(() =>
      expect(screen.getByRole('switch', { name: 'YouTube' })).toHaveAttribute(
        'aria-checked',
        'false',
      ),
    )
  })

  it('toggling Discord off clears both discord flags', async () => {
    const status = desyncStatus()
    stubBackend(status)
    const set = wailsApp.stub('SetDesyncSettings', (s) =>
      desyncStatus({ settings: s }),
    )
    await renderCard()

    fireEvent.click(screen.getByRole('switch', { name: 'Discord' }))
    await waitFor(() => expect(set).toHaveBeenCalledTimes(1))
    expect(set).toHaveBeenCalledWith({
      ...status.settings,
      discord: false,
      discordUDP: false,
    })
  })

  it('toggling Discord on enables UDP only when the core supports it', async () => {
    const status = desyncStatus({
      udpCoreSupported: false,
      settings: { discord: false, discordUDP: false },
    })
    stubBackend(status)
    const set = wailsApp.stub('SetDesyncSettings', (s) =>
      desyncStatus({ settings: s }),
    )
    await renderCard()

    fireEvent.click(screen.getByRole('switch', { name: 'Discord' }))
    await waitFor(() => expect(set).toHaveBeenCalledTimes(1))
    expect(set).toHaveBeenCalledWith({
      ...status.settings,
      discord: true,
      discordUDP: false,
    })
  })

  it('master switch turns Desync off with the same patch as before', async () => {
    const status = desyncStatus()
    stubBackend(status)
    const set = wailsApp.stub('SetDesyncSettings', (s) =>
      desyncStatus({ settings: s }),
    )
    await renderCard()
    fireEvent.click(screen.getByRole('switch', { name: 'Enable Desync' }))
    await waitFor(() => expect(set).toHaveBeenCalledTimes(1))
    expect(set).toHaveBeenCalledWith({ ...status.settings, enabled: false })
  })

  it('Discord cannot be turned on off-Windows', async () => {
    stubBackend(
      desyncStatus({
        platform: 'darwin',
        settings: { discord: false, discordUDP: false },
      }),
    )
    await renderCard()
    expect(screen.getByRole('switch', { name: 'Discord' })).toBeDisabled()
    expect(screen.getByRole('switch', { name: 'YouTube' })).toBeEnabled()
  })

  it('shows a save error inline', async () => {
    stubBackend()
    wailsApp.stub('SetDesyncSettings', () => {
      throw new Error('boom')
    })
    await renderCard()
    fireEvent.click(screen.getByRole('switch', { name: 'YouTube' }))
    expect(await screen.findByRole('alert')).toHaveTextContent('boom')
  })
})

describe('DesyncSettingsCard: status chips', () => {
  const cases: {
    title: string
    ctx?: Ctx
    status?: DesyncStatus
    auto?: DesyncAutoStatus
    service: 'youtube' | 'discord'
    index?: number
    tone: string
    text: string
  }[] = [
    {
      title: 'ready → ok',
      service: 'youtube',
      tone: 'ok',
      text: 'HTTPS available · automatic',
    },
    {
      title: 'subscription reserve → ok',
      auto: desyncAuto({ youtube: desyncService('profile') }),
      service: 'youtube',
      tone: 'ok',
      text: 'Using subscription reserve',
    },
    {
      title: 'testing → pending with progress',
      auto: desyncAuto({
        youtube: desyncService('testing', { attempt: 2, total: 5 }),
      }),
      service: 'youtube',
      tone: 'pending',
      text: 'Finding a strategy · 2/5',
    },
    {
      title: 'checking → pending',
      auto: desyncAuto({ discord: desyncService('checking') }),
      service: 'discord',
      tone: 'pending',
      text: 'Confirming a connection failure',
    },
    {
      title: 'no calibration yet → queued (pending)',
      auto: desyncAuto({ youtube: desyncService('') }),
      service: 'youtube',
      tone: 'pending',
      text: 'Waiting for a check',
    },
    {
      title: 'failed → problem',
      auto: desyncAuto({ youtube: desyncService('failed', { code: 'x' }) }),
      service: 'youtube',
      tone: 'problem',
      text: 'Desync and reserve unavailable · retrying automatically',
    },
    {
      title: 'reserve failed → problem',
      auto: desyncAuto({ discord: desyncService('profile_failed') }),
      service: 'discord',
      tone: 'problem',
      text: 'Subscription reserve unavailable · retrying',
    },
    {
      title: 'service off → off',
      status: desyncStatus({ settings: { youtube: false } }),
      service: 'youtube',
      tone: 'off',
      text: 'Off',
    },
    {
      title: 'Desync off → off',
      status: desyncStatus({ settings: { enabled: false } }),
      service: 'discord',
      tone: 'off',
      text: 'Off',
    },
    {
      title: 'not connected → connect hint',
      ctx: { ...CONNECTED, connected: false },
      service: 'youtube',
      tone: 'off',
      text: 'Connect on Home',
    },
    {
      title: 'Global mode → rule hint',
      ctx: { ...CONNECTED, mode: 'global' },
      service: 'youtube',
      tone: 'off',
      text: 'Select Rule mode',
    },
    {
      title: 'voice: UDP exchange → ok sub-line',
      service: 'discord',
      index: 1,
      tone: 'ok',
      text: 'Discord voice: UDP exchange detected',
    },
    {
      title: 'voice: proxy traffic → needs TUN',
      ctx: { ...CONNECTED, traffic: 'proxy' },
      service: 'discord',
      index: 1,
      tone: 'off',
      text: 'Discord voice: Enable TUN for voice',
    },
    {
      title: 'voice: no phase yet → waiting',
      auto: desyncAuto({ voice: desyncService('') }),
      service: 'discord',
      index: 1,
      tone: 'pending',
      text: 'Discord voice: Waiting for a Discord voice connection',
    },
    {
      title: 'voice: interrupted → problem',
      auto: desyncAuto({ voice: desyncService('interrupted') }),
      service: 'discord',
      index: 1,
      tone: 'problem',
      text: 'Discord voice: UDP traffic interrupted; waiting for recovery',
    },
    {
      title: 'voice: UDP off → off',
      status: desyncStatus({ settings: { discordUDP: false } }),
      service: 'discord',
      index: 1,
      tone: 'off',
      text: 'Discord voice: Off',
    },
  ]

  it.each(cases)('$title', async (c) => {
    stubBackend(c.status ?? desyncStatus(), c.auto ?? desyncAuto())
    await renderCard(c.ctx ?? CONNECTED)
    await waitFor(() => {
      const chip = chips(c.service)[c.index ?? 0]
      expect(chip).toBeDefined()
      expect(chip).toHaveAttribute('data-tone', c.tone)
      expect(chip).toHaveTextContent(c.text)
    })
  })

  it('voice sub-line sits in the Discord status cell, under its chip', async () => {
    stubBackend()
    await renderCard()
    const cell = serviceRow('discord').querySelector('.desyncServiceStatus')
    if (!cell) throw new Error('status cell missing')
    const lines = within(cell as HTMLElement).getAllByRole('status')
    expect(lines).toHaveLength(2)
    expect(lines[1]).toHaveClass('desyncChipSub')
    // YouTube has no voice line.
    expect(chips('youtube')).toHaveLength(1)
  })

  it('no voice sub-line while Desync is off', async () => {
    stubBackend(desyncStatus({ settings: { enabled: false } }))
    await renderCard()
    expect(chips('discord')).toHaveLength(1)
  })
})

describe('DesyncSettingsCard: actions', () => {
  afterEach(() => {
    vi.restoreAllMocks()
  })

  it('Check again is the primary action and calls RetryDesyncAuto', async () => {
    stubBackend()
    const retry = wailsApp.stub('RetryDesyncAuto', () =>
      desyncAuto({ youtube: desyncService('queued') }),
    )
    await renderCard()
    const btn = screen.getByRole('button', { name: 'Check again' })
    expect(btn).toHaveClass('btn', 'primary')
    fireEvent.click(btn)
    await waitFor(() => expect(retry).toHaveBeenCalledTimes(1))
    await waitFor(() =>
      expect(chips('youtube')[0]).toHaveTextContent('Waiting for a check'),
    )
  })

  it('Check again is disabled when not connected or not in Rule mode', async () => {
    stubBackend()
    renderWithProviders(
      <DesyncSettingsCard connected={false} mode="rule" traffic="tun" />,
    )
    await screen.findByRole('switch', { name: 'YouTube' })
    expect(screen.getByRole('button', { name: 'Check again' })).toBeDisabled()
  })

  it('Copy diagnostics is a quiet link that copies the report', async () => {
    stubBackend()
    const report = wailsApp.stub('GetDesyncDiagnosticReport', () => 'REPORT')
    const w = window as unknown as { runtime: unknown }
    const runtime = w.runtime
    const clip = vi.fn(() => Promise.resolve(true))
    w.runtime = { ClipboardSetText: clip }
    try {
      await renderCard()
      const btn = screen.getByRole('button', { name: 'Copy diagnostics' })
      expect(btn).toHaveClass('linkBtn')
      expect(btn).not.toHaveClass('btn')
      fireEvent.click(btn)
      await waitFor(() => expect(report).toHaveBeenCalledTimes(1))
      expect(clip).toHaveBeenCalledWith('REPORT')
      expect(
        await screen.findByRole('button', { name: 'Copied' }),
      ).toBeVisible()
    } finally {
      w.runtime = runtime
    }
  })

  it('Copy diagnostics reports a clipboard failure', async () => {
    stubBackend()
    wailsApp.stub('GetDesyncDiagnosticReport', () => 'REPORT')
    // The default runtime mock's ClipboardSetText resolves to undefined.
    await renderCard()
    fireEvent.click(screen.getByRole('button', { name: 'Copy diagnostics' }))
    expect(await screen.findByRole('alert')).toHaveTextContent(
      'Could not copy diagnostics',
    )
  })
})

describe('DesyncSettingsCard: shared poll', () => {
  it('a poll that started before a save cannot revert the saved result', async () => {
    const before = desyncStatus()
    const getStatus = wailsApp.stub('GetDesyncStatus', () => before)
    wailsApp.stub('GetDesyncAutoStatus', () => desyncAuto())
    wailsApp.stub('SetDesyncSettings', (s) => desyncStatus({ settings: s }))
    const { queryClient } = renderWithProviders(
      <DesyncSettingsCard {...CONNECTED} />,
    )
    await screen.findByRole('switch', { name: 'YouTube' })

    // A slow poll is in flight (pre-save snapshot) ...
    let resolvePoll: (s: DesyncStatus) => void = () => undefined
    getStatus.mockImplementation(
      () =>
        new Promise<DesyncStatus>((r) => {
          resolvePoll = r
        }),
    )
    const poll = queryClient.refetchQueries({ queryKey: ['desync-status'] })
    await waitFor(() => expect(getStatus).toHaveBeenCalledTimes(2))

    // ... the user saves, the save lands first ...
    fireEvent.click(screen.getByRole('switch', { name: 'YouTube' }))
    await waitFor(() =>
      expect(screen.getByRole('switch', { name: 'YouTube' })).toHaveAttribute(
        'aria-checked',
        'false',
      ),
    )

    // ... then the stale poll resolves: the saved state must stay.
    resolvePoll(before)
    await poll
    expect(screen.getByRole('switch', { name: 'YouTube' })).toHaveAttribute(
      'aria-checked',
      'false',
    )
    expect(
      queryClient.getQueryData<{ status: DesyncStatus }>(['desync-status'])
        ?.status.settings.youtube,
    ).toBe(false)
  })
})
