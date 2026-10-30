import { fireEvent, screen, waitFor, within } from '@testing-library/react'
import { describe, expect, it } from 'vitest'

import type {
  DesyncAutoStatus,
  DesyncCustomEntry,
  DesyncStatus,
} from '../api/desync'
import { desyncAuto, desyncService, desyncStatus } from '../test/desyncFixtures'
import { renderWithProviders } from '../test/render'
import { wailsApp } from '../test/wailsMock'

import { DesyncPanel } from './DesyncPanel'

const CONNECTED = { connected: true, mode: 'rule', traffic: 'tun' }
const ORG: DesyncCustomEntry = {
  id: '0a1b2c3d',
  domain: 'example.org',
  enabled: true,
}
const NET: DesyncCustomEntry = {
  id: '5a6b7c8d',
  domain: 'example.net',
  enabled: true,
}

function stubBackend(
  status: DesyncStatus = desyncStatus(),
  auto: DesyncAutoStatus = desyncAuto(),
) {
  wailsApp.stub('GetDesyncStatus', () => status)
  wailsApp.stub('GetDesyncAutoStatus', () => auto)
}

async function renderCard() {
  renderWithProviders(<DesyncPanel {...CONNECTED} />)
  await screen.findByRole('switch', { name: 'YouTube' })
}

function customRow(id: string) {
  const row = document.querySelector<HTMLElement>(
    `.desyncCustomRow[data-custom="${id}"]`,
  )
  if (!row) throw new Error(`custom row ${id} not rendered`)
  return row
}

function chip(id: string) {
  const el = customRow(id).querySelector<HTMLElement>('.desyncChip')
  if (!el) throw new Error('no chip')
  return el
}

describe('DesyncPanel: custom domains', () => {
  it('adds a domain through the backend and clears the field', async () => {
    stubBackend()
    const add = wailsApp.stub('AddDesyncRule', () =>
      desyncStatus({ settings: { custom: [ORG] } }),
    )
    await renderCard()
    const input = screen.getByRole('textbox', { name: 'Domain' })
    fireEvent.change(input, { target: { value: '  https://Example.org/x ' } })
    fireEvent.click(screen.getByRole('button', { name: 'Add' }))
    await waitFor(() => expect(add).toHaveBeenCalledTimes(1))
    // Normalization is the backend's job; the UI only trims.
    expect(add).toHaveBeenCalledWith({
      kind: 'suffix',
      value: 'https://Example.org/x',
      route: 'desync',
    })
    await waitFor(() => expect(input).toHaveValue(''))
    expect(within(customRow(ORG.id)).getByText('example.org')).toBeVisible()
  })

  it('shows a translated validation error and keeps the input', async () => {
    stubBackend()
    wailsApp.stub('AddDesyncRule', () => {
      throw 'desync: builtin'
    })
    await renderCard()
    const input = screen.getByRole('textbox', { name: 'Domain' })
    fireEvent.change(input, { target: { value: 'youtube.com' } })
    fireEvent.submit(input)
    expect(await screen.findByRole('alert')).toHaveTextContent(
      'This domain is covered by the built-in YouTube or Discord switch.',
    )
    expect(input).toHaveValue('youtube.com')
    expect(input).toHaveAttribute('aria-invalid', 'true')
    // Typing again clears the error.
    fireEvent.change(input, { target: { value: 'example.org' } })
    expect(screen.queryByRole('alert')).toBeNull()
  })

  it('shows an unknown backend error verbatim', async () => {
    stubBackend()
    wailsApp.stub('AddDesyncRule', () => {
      throw 'could not allocate a custom domain id'
    })
    await renderCard()
    const input = screen.getByRole('textbox', { name: 'Domain' })
    fireEvent.change(input, { target: { value: 'example.org' } })
    fireEvent.submit(input)
    expect(await screen.findByRole('alert')).toHaveTextContent(
      'could not allocate a custom domain id',
    )
  })

  it('toggles and removes an entry by id', async () => {
    stubBackend(desyncStatus({ settings: { custom: [ORG, NET] } }))
    const toggle = wailsApp.stub('SetDesyncCustomDomainEnabled', () =>
      desyncStatus({ settings: { custom: [{ ...ORG, enabled: false }, NET] } }),
    )
    const remove = wailsApp.stub('RemoveDesyncCustomDomain', () =>
      desyncStatus({ settings: { custom: [{ ...ORG, enabled: false }] } }),
    )
    await renderCard()
    fireEvent.click(screen.getByRole('switch', { name: 'example.org' }))
    await waitFor(() => expect(toggle).toHaveBeenCalledWith('0a1b2c3d', false))
    await waitFor(() =>
      expect(
        screen.getByRole('switch', { name: 'example.org' }),
      ).toHaveAttribute('aria-checked', 'false'),
    )
    expect(chip(ORG.id)).toHaveAttribute('data-phase', 'disabled')
    fireEvent.click(screen.getByRole('button', { name: 'Remove example.net' }))
    await waitFor(() => expect(remove).toHaveBeenCalledWith('5a6b7c8d'))
    await waitFor(() =>
      expect(document.querySelector(`[data-custom="${NET.id}"]`)).toBeNull(),
    )
  })

  it('a built-in toggle saves settings without touching the custom list', async () => {
    const status = desyncStatus({ settings: { custom: [ORG] } })
    stubBackend(status)
    const set = wailsApp.stub('SetDesyncSettings', (s) =>
      desyncStatus({ settings: s }),
    )
    await renderCard()
    fireEvent.click(screen.getByRole('switch', { name: 'YouTube' }))
    await waitFor(() => expect(set).toHaveBeenCalledTimes(1))
    expect(wailsApp.fn('AddDesyncRule')).not.toHaveBeenCalled()
    expect(wailsApp.fn('RemoveDesyncCustomDomain')).not.toHaveBeenCalled()
  })

  const phases: {
    title: string
    state?: ReturnType<typeof desyncService>
    tone: string
    text: string
    ipHint?: boolean
  }[] = [
    {
      title: 'no state yet → queued',
      tone: 'pending',
      text: 'Waiting for a check',
    },
    {
      title: 'testing → progress',
      state: desyncService('testing', { attempt: 3, total: 7 }),
      tone: 'pending',
      text: 'Finding a strategy · 3/7',
    },
    {
      title: 'ready → ok',
      state: desyncService('ready', { candidate: 'r2' }),
      tone: 'ok',
      text: 'HTTPS available · automatic',
    },
    {
      title: 'not needed → ok',
      state: desyncService('not_needed'),
      tone: 'ok',
      text: 'Not blocked · Desync not needed',
    },
    {
      title: 'unavailable → problem with the IP hint',
      state: desyncService('unavailable'),
      tone: 'problem',
      text: "Desync can't unblock this site",
      ipHint: true,
    },
  ]
  for (const c of phases) {
    it(`status chip: ${c.title}`, async () => {
      stubBackend(
        desyncStatus({ settings: { custom: [ORG] } }),
        desyncAuto({ custom: c.state ? { [ORG.id]: c.state } : undefined }),
      )
      await renderCard()
      await waitFor(() => expect(chip(ORG.id)).toHaveTextContent(c.text))
      expect(chip(ORG.id)).toHaveAttribute('data-tone', c.tone)
      const hint = within(customRow(ORG.id)).queryByText(
        'Likely blocked by IP address; it goes through your VPN.',
      )
      expect(hint !== null).toBe(c.ipHint === true)
    })
  }

  it('on a Desync-only profile the IP hint says the site stays blocked', async () => {
    stubBackend(
      desyncStatus({ desyncOnly: true, settings: { custom: [ORG] } }),
      desyncAuto({ custom: { [ORG.id]: desyncService('unavailable') } }),
    )
    await renderCard()
    expect(
      await within(customRow(ORG.id)).findByText(
        'Likely blocked by IP address. Without a VPN this site stays blocked.',
      ),
    ).toBeVisible()
    expect(within(customRow(ORG.id)).queryByText(/through your VPN/)).toBeNull()
  })

  it('lists the rules under "My rules"', async () => {
    stubBackend(desyncStatus({ settings: { custom: [ORG] } }))
    await renderCard()
    const sites = screen.getByRole('region', { name: 'My rules' })
    expect(within(sites).getByText(ORG.domain)).toBeVisible()
    expect(screen.queryByText('Custom domains')).toBeNull()
    expect(screen.queryByText('Other sites')).toBeNull()
  })

  it('disables adding at the limit', async () => {
    const many = Array.from({ length: 128 }, (_, i) => ({
      id: `e${i}`,
      domain: `site${i}.example.org`,
      kind: 'suffix',
      route: i < 32 ? 'desync' : 'direct',
      enabled: true,
    }))
    stubBackend(desyncStatus({ settings: { custom: many } }))
    await renderCard()
    expect(screen.getByRole('textbox', { name: 'Domain' })).toBeDisabled()
    expect(screen.getByText('Up to 128 rules.')).toBeVisible()
  })
})
