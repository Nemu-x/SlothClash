import { fireEvent, screen, waitFor, within } from '@testing-library/react'
import { describe, expect, it, vi } from 'vitest'

import type { DesyncAutoStatus, DesyncStatus } from '../api/desync'
import {
  DESYNC_SERVICES,
  DESYNC_TLSREC_2,
  DESYNC_UDP_ZERO_1,
  desyncAuto,
  desyncCapabilities,
  desyncService,
  desyncStatus,
} from '../test/desyncFixtures'
import { renderWithProviders } from '../test/render'
import { wailsApp } from '../test/wailsMock'

import { DesyncPanel } from './DesyncPanel'

type Ctx = { connected: boolean; mode: string; traffic: string }
const CONNECTED: Ctx = { connected: true, mode: 'rule', traffic: 'tun' }

function stubBackend(
  status: DesyncStatus = desyncStatus(),
  auto: DesyncAutoStatus = desyncAuto(),
) {
  wailsApp.stub('GetDesyncStatus', () => status)
  wailsApp.stub('GetDesyncAutoStatus', () => auto)
}

function stubCapabilities(report = desyncCapabilities()) {
  return wailsApp.stub('GetDesyncCapabilities', () => report)
}

async function renderPanel(
  ctx: Ctx = CONNECTED,
  onEnableTun?: () => Promise<string | void>,
) {
  const r = renderWithProviders(
    <DesyncPanel {...ctx} onEnableTun={onEnableTun} />,
  )
  await screen.findByRole('switch', { name: 'YouTube' })
  return r
}

function serviceRow(service: 'youtube' | 'discord') {
  const row = document.querySelector<HTMLElement>(
    `.desyncService[data-service="${service}"]`,
  )
  if (!row) throw new Error(`row ${service} not rendered`)
  return row
}

const find = (sel: string) => document.querySelector<HTMLElement>(sel)

describe('DesyncPanel: strategy in use', () => {
  it('names the strategy calibration chose for YouTube and Discord voice', async () => {
    stubBackend(
      desyncStatus(),
      desyncAuto({
        youtube: desyncService('ready', { candidate: DESYNC_TLSREC_2 }),
        discord: desyncService('testing', {
          candidate: DESYNC_TLSREC_2,
          attempt: 2,
          total: 9,
        }),
        voice: desyncService('media', { candidate: DESYNC_UDP_ZERO_1 }),
      }),
    )
    stubCapabilities()
    await renderPanel()
    await waitFor(() =>
      expect(
        find('[data-service="youtube"] .desyncStrategy'),
      ).toHaveTextContent(
        'Strategy: Split the TLS record, variant 2 · chosen automatically',
      ),
    )
    // The library's technical description stays a tooltip.
    expect(find('[data-service="youtube"] .desyncStrategy')).toHaveAttribute(
      'title',
      'TLS record split at the SNI',
    )
    // Discord HTTPS is still searching (its chip says so): no strategy line
    // for it; its voice runs on a decoy strategy.
    const discord = [
      ...serviceRow('discord').querySelectorAll('.desyncStrategy'),
    ]
    expect(discord).toHaveLength(1)
    expect(discord[0]).toHaveTextContent(
      'Voice strategy: Send decoy datagrams first, variant 1 · chosen automatically',
    )
  })

  it('an ID the catalog does not list reads "chosen automatically"', async () => {
    stubBackend(
      desyncStatus(),
      desyncAuto({
        youtube: desyncService('ready', { candidate: 'sunknown000' }),
      }),
    )
    stubCapabilities()
    await renderPanel()
    await waitFor(() =>
      expect(
        find('[data-service="youtube"] .desyncStrategy'),
      ).toHaveTextContent('Strategy: chosen automatically'),
    )
  })

  it('sites: a strategy where one is applied, none where the site is not blocked', async () => {
    wailsApp.stub('ListDesyncServices', () => DESYNC_SERVICES)
    stubBackend(
      desyncStatus({
        settings: {
          custom: [
            { id: 'c1', domain: 'blocked.example', enabled: true },
            { id: 'c2', domain: 'open.example', enabled: true },
            { id: 'c3', domain: '', service: 'x', enabled: true },
          ],
        },
      }),
      desyncAuto({
        custom: {
          c1: desyncService('ready', { candidate: DESYNC_TLSREC_2 }),
          c2: desyncService('not_needed', { candidate: 'direct' }),
          c3: desyncService('ready', { candidate: 's0000000001' }),
        },
      }),
    )
    stubCapabilities()
    await renderPanel()
    await waitFor(() =>
      expect(find('[data-custom="c1"] .desyncStrategy')).toHaveTextContent(
        'Strategy: Split the TLS record, variant 2 · chosen automatically',
      ),
    )
    expect(find('[data-custom="c2"] .desyncStrategy')).toBeNull()
    expect(find('[data-custom="c2"] .desyncChip')).toHaveTextContent(
      'Not blocked · Desync not needed',
    )
    await waitFor(() =>
      expect(find('[data-popular="x"] .desyncStrategy')).toHaveTextContent(
        'Strategy: Split the TLS record, variant 1 · chosen automatically',
      ),
    )
  })
})

describe('DesyncPanel: what this computer can do', () => {
  it('lists the methods, collapsed, with an honest note', async () => {
    stubBackend()
    stubCapabilities()
    await renderPanel()
    const section = await waitFor(() => {
      const el =
        document.querySelector<HTMLDetailsElement>('details.desyncCaps')
      expect(el).not.toBeNull()
      return el as HTMLDetailsElement
    })
    expect(section.open).toBe(false)
    const summary = section.querySelector('summary')
    expect(summary).toHaveTextContent('What this computer can do')
    expect(summary).toHaveTextContent('7 of 8 methods available')
    expect(section).toHaveTextContent(
      'This is not a network test: which method works depends on your network.',
    )
    const method = (m: string) =>
      section.querySelector<HTMLElement>(`[data-method="${m}"]`)
    expect(method('tlsrec')).toHaveTextContent('Splitting the TLS record')
    expect(method('tlsrec')).toHaveTextContent('Available')
    expect(method('split-wait-sent')).toHaveTextContent(
      'Available · timing estimated',
    )
    expect(method('split-wait-acked')).toHaveTextContent(
      'Waiting until a piece is delivered',
    )
    expect(method('split-wait-acked')).toHaveTextContent('Not available')
    expect(method('split-wait-acked')).toHaveTextContent(
      'acked split-wait needs TCP_INFO',
    )
    expect(method('fake-udp')).toHaveTextContent('Decoy packets (voice)')
    expect(section).toHaveTextContent(
      'Automatic selection tries 3 of 4 strategies on this computer.',
    )
  })

  it('is left out until the backend reports it', async () => {
    stubBackend()
    // GetDesyncCapabilities not stubbed: the call rejects.
    await renderPanel()
    expect(find('.desyncCaps')).toBeNull()
  })
})

describe('DesyncPanel: voice actions', () => {
  it('Reconnect voice: shown while voice runs through Desync, closes the flows', async () => {
    stubBackend()
    const reconnect = wailsApp.stub('ReconnectDiscordUDP', () => 2)
    await renderPanel()
    fireEvent.click(
      within(serviceRow('discord')).getByRole('button', {
        name: 'Reconnect voice',
      }),
    )
    await waitFor(() => expect(reconnect).toHaveBeenCalledTimes(1))
    expect(
      await within(serviceRow('discord')).findByText(
        'Discord is reconnecting voice',
      ),
    ).toBeVisible()
  })

  it('Reconnect voice says when there was nothing to reconnect', async () => {
    stubBackend()
    wailsApp.stub('ReconnectDiscordUDP', () => 0)
    await renderPanel()
    fireEvent.click(screen.getByRole('button', { name: 'Reconnect voice' }))
    expect(
      await screen.findByText('No active voice connection to reconnect'),
    ).toBeVisible()
  })

  it.each([
    ['voice off', desyncStatus({ settings: { discordUDP: false } }), CONNECTED],
    ['proxy traffic', desyncStatus(), { ...CONNECTED, traffic: 'proxy' }],
    ['disconnected', desyncStatus(), { ...CONNECTED, connected: false }],
    ['global mode', desyncStatus(), { ...CONNECTED, mode: 'global' }],
    [
      'Discord off',
      desyncStatus({ settings: { discord: false, discordUDP: false } }),
      CONNECTED,
    ],
  ])('Reconnect voice is hidden: %s', async (_name, status, ctx) => {
    stubBackend(status)
    await renderPanel(ctx)
    expect(screen.queryByRole('button', { name: 'Reconnect voice' })).toBeNull()
  })

  it('Turn on TUN: shown while voice needs TUN, uses the given switch', async () => {
    stubBackend()
    const onEnableTun = vi.fn(async () => '')
    await renderPanel({ ...CONNECTED, traffic: 'proxy' }, onEnableTun)
    const discord = serviceRow('discord')
    expect(within(discord).getByText(/Enable TUN for voice/)).toBeVisible()
    fireEvent.click(
      within(discord).getByRole('button', { name: 'Turn on TUN' }),
    )
    await waitFor(() => expect(onEnableTun).toHaveBeenCalledTimes(1))
    expect(screen.queryByRole('alert')).toBeNull()
  })

  it('Turn on TUN shows the switch failure', async () => {
    stubBackend()
    await renderPanel(
      { ...CONNECTED, traffic: 'proxy' },
      async () => 'Error: service not installed',
    )
    fireEvent.click(screen.getByRole('button', { name: 'Turn on TUN' }))
    expect(await screen.findByRole('alert')).toHaveTextContent(
      'Error: service not installed',
    )
  })

  it.each([
    ['TUN already on', desyncStatus(), 'tun', true],
    [
      'voice off',
      desyncStatus({ settings: { discordUDP: false } }),
      'proxy',
      true,
    ],
    ['no switch given', desyncStatus(), 'proxy', false],
  ])(
    'Turn on TUN is hidden: %s',
    async (_name, status, traffic, withSwitch) => {
      stubBackend(status)
      await renderPanel(
        { ...CONNECTED, traffic },
        withSwitch ? async () => '' : undefined,
      )
      expect(screen.queryByRole('button', { name: 'Turn on TUN' })).toBeNull()
    },
  )
})
