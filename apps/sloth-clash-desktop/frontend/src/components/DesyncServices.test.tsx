import { fireEvent, screen, waitFor, within } from '@testing-library/react'
import { describe, expect, it, vi } from 'vitest'

import type {
  DesyncAutoStatus,
  DesyncCustomEntry,
  DesyncService,
  DesyncStatus,
} from '../api/desync'
import {
  DESYNC_SERVICES,
  desyncAuto,
  desyncService,
  desyncStatus,
} from '../test/desyncFixtures'
import { renderWithProviders } from '../test/render'
import { wailsApp } from '../test/wailsMock'

import { DesyncModal } from './DesyncModal'
import { DesyncPanel, type DesyncPanelVariant } from './DesyncPanel'

const CONNECTED = { connected: true, mode: 'rule', traffic: 'tun' }
const FB: DesyncCustomEntry = {
  id: 'fb01fb01',
  domain: '',
  service: 'facebook',
  enabled: true,
}
const IG: DesyncCustomEntry = {
  id: 'ig01ig01',
  domain: '',
  service: 'instagram',
  enabled: true,
}
const ORG: DesyncCustomEntry = {
  id: '0a1b2c3d',
  domain: 'example.org',
  enabled: true,
}

function stubBackend(
  status: DesyncStatus = desyncStatus(),
  auto: DesyncAutoStatus = desyncAuto(),
  catalog: DesyncService[] = DESYNC_SERVICES,
) {
  wailsApp.stub('GetDesyncStatus', () => status)
  wailsApp.stub('GetDesyncAutoStatus', () => auto)
  wailsApp.stub('ListDesyncServices', () => catalog)
}

const withCustom = (custom: DesyncCustomEntry[], over = {}) =>
  desyncStatus({ settings: { custom, ...over } })

async function renderPanel(variant: DesyncPanelVariant = 'manage') {
  renderWithProviders(<DesyncPanel {...CONNECTED} variant={variant} />)
  await screen.findByRole('group', { name: 'Popular services' })
}

function serviceRow(id: string) {
  const row = document.querySelector<HTMLElement>(
    `.desyncPopularRow[data-popular="${id}"]`,
  )
  if (!row) throw new Error(`service row ${id} not rendered`)
  return row
}

const serviceSwitch = (name: string) =>
  within(screen.getByRole('group', { name: 'Popular services' })).getByRole(
    'switch',
    {
      name,
    },
  )

describe('DesyncPanel: services', () => {
  it('lists the catalog the backend returns, above the own-sites list', async () => {
    const extra = { id: 'demo', name: 'Demo', domains: ['demo.example'] }
    stubBackend(withCustom([ORG]), desyncAuto(), [...DESYNC_SERVICES, extra])
    await renderPanel()
    const group = screen.getByRole('group', { name: 'Popular services' })
    expect(
      within(group)
        .getAllByRole('switch')
        .map((s) => s.getAttribute('aria-label')),
    ).toEqual(['Facebook', 'Instagram', 'X (Twitter)', 'Demo'])
    // Popular comes first inside Custom domains, then the user's own sites.
    const own = screen.getByRole('group', { name: 'Other sites' })
    expect(
      group.compareDocumentPosition(own) & Node.DOCUMENT_POSITION_FOLLOWING,
    ).toBeTruthy()
    expect(within(own).getByText('example.org')).toBeVisible()
    // A single-domain service has no "+N more".
    expect(within(serviceRow('demo')).queryByRole('button')).toBeNull()
  })

  it('summarizes the domains and discloses the full list', async () => {
    stubBackend()
    await renderPanel()
    const row = serviceRow('facebook')
    const summary = within(row).getByText('facebook.com, fbcdn.net')
    expect(summary).toHaveAttribute(
      'title',
      DESYNC_SERVICES[0].domains.join(', '),
    )
    // The switch is described by its domains.
    expect(serviceSwitch('Facebook')).toHaveAccessibleDescription(
      'facebook.com, fbcdn.net',
    )
    const more = within(row).getByRole('button', { name: '+7 more' })
    expect(more).toHaveAttribute('aria-expanded', 'false')
    const all = document.getElementById(
      String(more.getAttribute('aria-controls')),
    ) as HTMLElement
    expect(all).not.toBeVisible()
    fireEvent.click(more)
    expect(more).toHaveAttribute('aria-expanded', 'true')
    expect(more).toHaveTextContent('Hide')
    expect(all).toBeVisible()
    expect(all).toHaveTextContent(
      'facebook.com, fbcdn.net, facebook.net, fbsbx.com, fb.com, fb.me, fb.watch, messenger.com, m.me',
    )
  })

  it('switching a new service on adds it by catalog ID', async () => {
    stubBackend()
    const add = wailsApp.stub('AddDesyncService', () => withCustom([FB]))
    await renderPanel()
    expect(serviceSwitch('Facebook')).toHaveAttribute('aria-checked', 'false')
    fireEvent.click(serviceSwitch('Facebook'))
    await waitFor(() => expect(add).toHaveBeenCalledWith('facebook'))
    await waitFor(() =>
      expect(serviceSwitch('Facebook')).toHaveAttribute('aria-checked', 'true'),
    )
    expect(wailsApp.fn('SetDesyncCustomDomainEnabled')).not.toHaveBeenCalled()
  })

  it('switching an existing service toggles its entry and never removes it', async () => {
    stubBackend(withCustom([FB]))
    const toggle = wailsApp.stub(
      'SetDesyncCustomDomainEnabled',
      (_id: string, enabled: boolean) => withCustom([{ ...FB, enabled }]),
    )
    await renderPanel()
    fireEvent.click(serviceSwitch('Facebook'))
    await waitFor(() => expect(toggle).toHaveBeenLastCalledWith(FB.id, false))
    await waitFor(() =>
      expect(serviceSwitch('Facebook')).toHaveAttribute(
        'aria-checked',
        'false',
      ),
    )
    fireEvent.click(serviceSwitch('Facebook'))
    await waitFor(() => expect(toggle).toHaveBeenLastCalledWith(FB.id, true))
    expect(wailsApp.fn('AddDesyncService')).not.toHaveBeenCalled()
    expect(wailsApp.fn('RemoveDesyncCustomDomain')).not.toHaveBeenCalled()
  })

  it('keeps service entries out of the free-form list', async () => {
    stubBackend(withCustom([FB, ORG, { ...IG, enabled: false }]))
    await renderPanel()
    const rows = document.querySelectorAll('.desyncCustomRow')
    expect(
      [...rows].map((r) => r.textContent?.includes('example.org')),
    ).toEqual([true])
    expect(
      [...document.querySelectorAll('.desyncCustomDomain')].map(
        (d) => d.textContent,
      ),
    ).toEqual(['example.org'])
  })

  it('manage: an enabled service shows its status chip, a disabled one none', async () => {
    stubBackend(
      withCustom([FB, { ...IG, enabled: false }]),
      desyncAuto({ custom: { [FB.id]: desyncService('not_needed') } }),
    )
    await renderPanel()
    const chip = serviceRow('facebook').querySelector('.desyncChip')
    expect(chip).toHaveAttribute('data-phase', 'not_needed')
    expect(chip).toHaveAttribute('data-tone', 'ok')
    expect(chip).toHaveTextContent('Not blocked · Desync not needed')
    expect(serviceRow('instagram').querySelector('.desyncChip')).toBeNull()
    expect(serviceRow('x').querySelector('.desyncChip')).toBeNull()
  })

  it.each([
    ['checking', 'pending', 'Confirming a connection failure'],
    ['ready', 'ok', 'HTTPS available · automatic'],
    ['unavailable', 'problem', "Desync can't unblock this site"],
  ])('manage: phase %s reads like other rows', async (phase, tone, text) => {
    stubBackend(
      withCustom([FB]),
      desyncAuto({ custom: { [FB.id]: desyncService(phase) } }),
    )
    await renderPanel()
    const chip = serviceRow('facebook').querySelector('.desyncChip')
    expect(chip).toHaveAttribute('data-tone', tone)
    expect(chip).toHaveTextContent(text)
    expect(
      within(serviceRow('facebook')).queryByText(
        'Likely blocked by IP address; traffic follows your profile.',
      ) !== null,
    ).toBe(phase === 'unavailable')
  })

  it('onboarding: the same rows without status chips', async () => {
    stubBackend(
      withCustom([FB]),
      desyncAuto({ custom: { [FB.id]: desyncService('ready') } }),
    )
    await renderPanel('onboarding')
    expect(serviceSwitch('Facebook')).toHaveAttribute('aria-checked', 'true')
    expect(document.querySelectorAll('.desyncChip')).toHaveLength(0)
  })

  it.each([
    [
      'custom domain: limit',
      'Up to 32 entries, popular services included. Remove a site first.',
    ],
    [
      'custom domain: unknown_service',
      'This service is not available in this version.',
    ],
    ['custom domain: duplicate', 'This service is already on the list.'],
  ])('shows %s as a sentence under Popular services', async (raw, text) => {
    stubBackend()
    wailsApp.stub('AddDesyncService', () => {
      throw raw
    })
    await renderPanel()
    fireEvent.click(serviceSwitch('Instagram'))
    const alert = await within(
      screen.getByRole('group', { name: 'Popular services' }),
    ).findByRole('alert')
    expect(alert).toHaveTextContent(text)
    expect(serviceSwitch('Instagram')).toHaveAccessibleDescription(
      `instagram.com, cdninstagram.com ${text}`,
    )
    // Only the switch that failed is described by the error.
    expect(serviceSwitch('Facebook')).toHaveAccessibleDescription(
      'facebook.com, fbcdn.net',
    )
    // The domain field is not blamed for a service error.
    expect(screen.getByRole('textbox', { name: 'Domain' })).not.toHaveAttribute(
      'aria-invalid',
    )
  })

  it('a typed domain a service covers names the service', async () => {
    stubBackend(withCustom([FB]))
    wailsApp.stub('AddDesyncCustomDomain', () => {
      throw 'custom domain: service_overlap: facebook'
    })
    await renderPanel()
    const input = screen.getByRole('textbox', { name: 'Domain' })
    fireEvent.change(input, { target: { value: 'm.facebook.com' } })
    fireEvent.submit(input)
    expect(await screen.findByRole('alert')).toHaveTextContent(
      'Already covered by Facebook in Popular services.',
    )
    expect(input).toHaveAttribute('aria-invalid', 'true')
  })

  it('notes which plain entries a new service replaced', async () => {
    const typed = { id: '9f9f9f9f', domain: 'facebook.com', enabled: true }
    stubBackend(withCustom([typed, ORG]))
    wailsApp.stub('AddDesyncService', () => withCustom([ORG, FB]))
    await renderPanel()
    fireEvent.click(serviceSwitch('Facebook'))
    const note = await within(serviceRow('facebook')).findByText(
      'Replaced facebook.com: Facebook covers it.',
    )
    // Announced politely, like the status chips.
    expect(note).toHaveAttribute('role', 'status')
    expect(
      [...document.querySelectorAll('.desyncCustomDomain')].map(
        (d) => d.textContent,
      ),
    ).toEqual(['example.org'])
  })

  it('adds no note when nothing was replaced', async () => {
    stubBackend(withCustom([ORG]))
    const add = wailsApp.stub('AddDesyncService', () => withCustom([ORG, FB]))
    await renderPanel()
    fireEvent.click(serviceSwitch('Facebook'))
    await waitFor(() => expect(add).toHaveBeenCalled())
    await waitFor(() =>
      expect(serviceSwitch('Facebook')).toHaveAttribute('aria-checked', 'true'),
    )
    expect(document.querySelector('[data-note="replaced"]')).toBeNull()
  })

  describe('shared fbcdn.net', () => {
    const hint = (id: string) =>
      serviceRow(id).querySelector('[data-note="shared"]')

    it('is noted under Instagram while Facebook is off', async () => {
      stubBackend(withCustom([IG, { ...FB, enabled: false }]))
      await renderPanel()
      expect(hint('instagram')).toHaveTextContent(
        'Also routes fbcdn.net: it is shared with Facebook, so Facebook images on other sites go this way too.',
      )
      expect(hint('facebook')).toBeNull()
    })

    it('is not noted while Facebook carries it, or for Facebook itself', async () => {
      stubBackend(withCustom([FB, IG]))
      await renderPanel()
      expect(hint('instagram')).toBeNull()
      expect(hint('facebook')).toBeNull()
    })

    it('is not noted while Instagram is off', async () => {
      stubBackend(withCustom([{ ...IG, enabled: false }]))
      await renderPanel()
      expect(hint('instagram')).toBeNull()
    })
  })

  it('names a stored service by its ID when the catalog is unavailable', async () => {
    wailsApp.stub('GetDesyncStatus', () => withCustom([FB]))
    wailsApp.stub('GetDesyncAutoStatus', () => desyncAuto())
    wailsApp.stub('ListDesyncServices', () => {
      throw new Error('no catalog')
    })
    renderWithProviders(<DesyncPanel {...CONNECTED} />)
    expect(
      await screen.findByRole('switch', { name: 'facebook' }),
    ).toHaveAttribute('aria-checked', 'true')
    expect(document.querySelectorAll('.desyncCustomRow')).toHaveLength(0)
  })
})

describe('DesyncModal onboarding: services', () => {
  const NOTHING = { youtube: false, discord: false, discordUDP: false }

  function renderOnboarding() {
    renderWithProviders(
      <DesyncModal
        view="onboarding"
        {...CONNECTED}
        profiles={[]}
        refresh={vi.fn(async () => {})}
        onClose={vi.fn()}
        onCreated={vi.fn()}
      />,
    )
  }

  it('a switched-on service alone allows Create profile', async () => {
    stubBackend(withCustom([FB], { ...NOTHING, enabled: false }))
    renderOnboarding()
    await screen.findByRole('group', { name: 'Popular services' })
    expect(screen.getByRole('button', { name: 'Create profile' })).toBeEnabled()
    expect(screen.queryByText('Turn on a service or add a site.')).toBeNull()
  })

  it('a switched-off service does not', async () => {
    stubBackend(
      withCustom([{ ...FB, enabled: false }], { ...NOTHING, enabled: false }),
    )
    renderOnboarding()
    await screen.findByRole('group', { name: 'Popular services' })
    expect(
      screen.getByRole('button', { name: 'Create profile' }),
    ).toBeDisabled()
    expect(screen.getByText('Turn on a service or add a site.')).toBeVisible()
  })
})
