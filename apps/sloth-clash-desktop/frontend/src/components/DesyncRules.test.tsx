import { fireEvent, screen, waitFor, within } from '@testing-library/react'
import { describe, expect, it } from 'vitest'

import type {
  DesyncAutoStatus,
  DesyncCustomEntry,
  DesyncStatus,
} from '../api/desync'
import {
  DESYNC_TLSREC_2,
  desyncAuto,
  desyncCapabilities,
  desyncService,
  desyncStatus,
} from '../test/desyncFixtures'
import { renderWithProviders } from '../test/render'
import { wailsApp } from '../test/wailsMock'

import { DesyncPanel } from './DesyncPanel'

const CONNECTED = { connected: true, mode: 'rule', traffic: 'tun' }
const SITE: DesyncCustomEntry = {
  id: '0a1b2c3d',
  domain: 'example.org',
  kind: 'suffix',
  route: 'desync',
  enabled: true,
}
const APP_VPN: DesyncCustomEntry = {
  id: '1b2c3d4e',
  domain: '',
  process: 'Telegram.exe',
  kind: 'process',
  route: 'vpn',
  enabled: true,
}
const APP_DESYNC: DesyncCustomEntry = {
  id: '2c3d4e5f',
  domain: '',
  process: 'chrome.exe',
  kind: 'process',
  route: 'desync',
  enabled: true,
}
const LOGIN_DIRECT: DesyncCustomEntry = {
  id: '3d4e5f60',
  domain: 'login.example.org',
  kind: 'domain',
  route: 'direct',
  enabled: true,
}

function stubBackend(
  status: DesyncStatus = desyncStatus(),
  auto: DesyncAutoStatus = desyncAuto(),
) {
  wailsApp.stub('GetDesyncStatus', () => status)
  wailsApp.stub('GetDesyncAutoStatus', () => auto)
  wailsApp.stub('GetDesyncCapabilities', () => desyncCapabilities())
}

async function renderPanel() {
  renderWithProviders(<DesyncPanel {...CONNECTED} />)
  await screen.findByRole('switch', { name: 'YouTube' })
}

function ruleRow(id: string) {
  const row = document.querySelector<HTMLElement>(
    `.desyncCustomRow[data-custom="${id}"]`,
  )
  if (!row) throw new Error(`rule row ${id} not rendered`)
  return row
}

const routeSelect = () => screen.getByRole('combobox', { name: 'Route' })
const kindSelect = () => screen.getByRole('combobox', { name: 'Rule type' })

describe('DesyncPanel: my rules', () => {
  it('adds an app rule to the VPN', async () => {
    stubBackend()
    const add = wailsApp.stub('AddDesyncRule', () =>
      desyncStatus({ settings: { custom: [APP_VPN] } }),
    )
    await renderPanel()
    fireEvent.change(kindSelect(), { target: { value: 'process' } })
    const input = screen.getByRole('textbox', { name: 'Process name' })
    expect(input).toHaveAttribute('placeholder', 'Telegram.exe')
    fireEvent.change(input, { target: { value: ' Telegram.exe ' } })
    fireEvent.change(routeSelect(), { target: { value: 'vpn' } })
    fireEvent.click(screen.getByRole('button', { name: 'Add' }))
    await waitFor(() =>
      expect(add).toHaveBeenCalledWith({
        kind: 'process',
        value: 'Telegram.exe',
        route: 'vpn',
      }),
    )
    await waitFor(() => expect(input).toHaveValue(''))
    // The form starts over with a site through Desync.
    expect(kindSelect()).toHaveValue('suffix')
    expect(routeSelect()).toHaveValue('desync')
  })

  it('shows what each rule matches and where it goes', async () => {
    stubBackend(
      desyncStatus({
        settings: { custom: [SITE, APP_VPN, APP_DESYNC, LOGIN_DIRECT] },
      }),
      desyncAuto({
        custom: {
          [SITE.id]: desyncService('ready', { candidate: DESYNC_TLSREC_2 }),
          [APP_DESYNC.id]: desyncService('fixed', {
            candidate: DESYNC_TLSREC_2,
          }),
        },
      }),
    )
    await renderPanel()
    const site = ruleRow(SITE.id)
    expect(within(site).getByText('example.org')).toBeVisible()
    expect(
      within(site).getByText('with subdomains → Desync (auto)'),
    ).toBeVisible()
    expect(within(site).getByText('HTTPS available · automatic')).toBeVisible()
    const app = ruleRow(APP_VPN.id)
    expect(within(app).getByText('Telegram.exe')).toBeVisible()
    expect(within(app).getByText('app → VPN')).toBeVisible()
    expect(within(app).getByText('Through VPN')).toBeVisible()
    const login = ruleRow(LOGIN_DIRECT.id)
    expect(within(login).getByText('exact domain → Direct')).toBeVisible()
    expect(within(login).getByText('Directly')).toBeVisible()
    // An app through Desync uses the default strategy: it is not tested.
    const chrome = ruleRow(APP_DESYNC.id)
    expect(
      within(chrome).getByText('Default strategy · apps aren’t tested'),
    ).toBeVisible()
    expect(
      await within(chrome).findByText(
        'Strategy: Split the TLS record, variant 2 · default',
      ),
    ).toBeVisible()
  })

  it('edits a rule in place and can cancel', async () => {
    stubBackend(desyncStatus({ settings: { custom: [SITE, APP_VPN] } }))
    const update = wailsApp.stub('UpdateDesyncRule', () =>
      desyncStatus({
        settings: {
          custom: [SITE, { ...APP_VPN, id: '9f9f9f9f', route: 'direct' }],
        },
      }),
    )
    await renderPanel()
    fireEvent.click(screen.getByRole('button', { name: 'Edit Telegram.exe' }))
    expect(kindSelect()).toHaveValue('process')
    expect(routeSelect()).toHaveValue('vpn')
    const input = screen.getByRole('textbox', { name: 'Process name' })
    expect(input).toHaveValue('Telegram.exe')
    expect(ruleRow(APP_VPN.id)).toHaveAttribute('aria-current', 'true')
    // Cancel leaves the rule alone.
    fireEvent.click(screen.getByRole('button', { name: 'Cancel' }))
    expect(input).toHaveValue('')
    expect(screen.queryByRole('button', { name: 'Save' })).toBeNull()

    fireEvent.click(screen.getByRole('button', { name: 'Edit Telegram.exe' }))
    fireEvent.change(routeSelect(), { target: { value: 'direct' } })
    fireEvent.click(screen.getByRole('button', { name: 'Save' }))
    await waitFor(() =>
      expect(update).toHaveBeenCalledWith(APP_VPN.id, {
        kind: 'process',
        value: 'Telegram.exe',
        route: 'direct',
      }),
    )
    await waitFor(() =>
      expect(
        within(ruleRow('9f9f9f9f')).getByText('app → Direct'),
      ).toBeVisible(),
    )
    expect(screen.getByRole('button', { name: 'Add' })).toBeVisible()
    expect(wailsApp.fn('AddDesyncRule')).not.toHaveBeenCalled()
  })

  it('offers no VPN route on a Desync-only profile', async () => {
    stubBackend(desyncStatus({ desyncOnly: true }))
    await renderPanel()
    const options = within(routeSelect())
      .getAllByRole('option')
      .map((o) => o.textContent)
    expect(options).toEqual(['Desync (auto)', 'Direct'])
  })

  it.each([
    [
      'suffix',
      'desync: no_vpn',
      'This profile has no VPN. Choose Desync or Direct.',
    ],
    [
      'process',
      'desync: invalid_process',
      'Enter an app’s process name, for example Telegram.exe.',
    ],
    [
      'process',
      'desync: wildcard',
      'Enter the exact process name, without * or ?.',
    ],
    ['process', 'desync: duplicate', 'This app is already in the list.'],
    ['suffix', 'desync: duplicate', 'This rule is already in the list.'],
    ['suffix', 'desync: rule_limit', 'You can add up to 128 rules.'],
  ])('translates a %s rule error %s', async (kind, raw, text) => {
    stubBackend()
    wailsApp.stub('AddDesyncRule', () => {
      throw raw
    })
    await renderPanel()
    fireEvent.change(kindSelect(), { target: { value: kind } })
    const input = screen.getByRole('textbox', {
      name: kind === 'process' ? 'Process name' : 'Domain',
    })
    fireEvent.change(input, { target: { value: 'x' } })
    fireEvent.submit(input)
    expect(await screen.findByRole('alert')).toHaveTextContent(text)
    expect(input).toHaveValue('x')
  })

  it('explains the default strategy while an app rule through Desync is being typed', async () => {
    stubBackend()
    await renderPanel()
    fireEvent.change(kindSelect(), { target: { value: 'process' } })
    expect(
      screen.getByText(
        'An app can’t be tested like a site, so Desync uses its default strategy for it.',
      ),
    ).toBeVisible()
    fireEvent.change(routeSelect(), { target: { value: 'direct' } })
    expect(screen.queryByText(/default strategy for it/)).toBeNull()
  })
})
