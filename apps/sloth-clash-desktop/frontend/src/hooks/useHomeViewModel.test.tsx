import { describe, expect, it } from 'vitest'

import {
  DESYNC_PRESETS,
  desyncAuto,
  desyncService,
  desyncSnapshot,
  desyncStatus,
} from '../test/desyncFixtures'
import { renderHookWithProviders } from '../test/render'

import { useHomeViewModel } from './useHomeViewModel'

type Input = Parameters<typeof useHomeViewModel>[0]

function vm(over: Partial<Input> = {}) {
  const input: Input = {
    state: null,
    connectBusy: false,
    optimisticMode: null,
    optimisticTraffic: null,
    updateSnap: null,
    ...over,
  }
  return renderHookWithProviders(() => useHomeViewModel(input)).result.current
}

const PROFILES = {
  activeProfileId: 'p2',
  profiles: [
    { id: 'p1', name: 'Home' },
    { id: 'p2', name: 'Work' },
  ],
}

function connected(connection: Record<string, unknown> = {}, extra = {}) {
  return {
    profile: PROFILES,
    connection: { status: 'connected', health: 'ready', ...connection },
    ...extra,
  }
}

describe('useHomeViewModel: profiles', () => {
  it('reports no profile on a fresh install (null state)', () => {
    const m = vm()
    expect(m.hasAnyProfile).toBe(false)
    expect(m.hasActiveProfile).toBe(false)
    expect(m.activeProfile).toBeUndefined()
    expect(m.connectionLabel).toBe('Not connected')
    expect(m.connectVisual).toBe('idle')
    expect(m.displayMode).toBe('rule')
    expect(m.displayTraffic).toBe('proxy')
    expect(m.activeNode).toBe('')
    expect(m.activeNodeVisual).toEqual({ iso: '', text: '' })
    expect(m.nodePickerGroup).toBeNull()
    expect(m.homeAlertTooltip).toBe('')
    expect(m.homeUpdateTooltip).toBe('')
  })

  it('finds the active profile', () => {
    const m = vm({ state: { profile: PROFILES } })
    expect(m.hasAnyProfile).toBe(true)
    expect(m.hasActiveProfile).toBe(true)
    expect(m.activeProfile).toEqual({ id: 'p2', name: 'Work' })
  })

  it('has profiles but no active one', () => {
    const m = vm({
      state: { profile: { activeProfileId: '', profiles: PROFILES.profiles } },
    })
    expect(m.hasAnyProfile).toBe(true)
    expect(m.hasActiveProfile).toBe(false)
    expect(m.activeProfile).toBeUndefined()
  })
})

describe('useHomeViewModel: connection status', () => {
  it('disconnected', () => {
    const m = vm({
      state: { profile: PROFILES, connection: { status: 'disconnected' } },
    })
    expect(m.connectionLabel).toBe('Not connected')
    expect(m.connectVisual).toBe('idle')
    expect(m.showProtectedBadge).toBe(false)
    expect(m.homeTrafficHealthSubtitle).toBe('')
  })

  it('connecting, from the backend or from a local click', () => {
    const fromBackend = vm({
      state: { profile: PROFILES, connection: { status: 'connecting' } },
    })
    expect(fromBackend.connectionLabel).toBe('Connecting')
    // Only the local busy flag drives the button visual.
    expect(fromBackend.connectVisual).toBe('idle')
    const fromClick = vm({
      state: { profile: PROFILES, connection: { status: 'disconnected' } },
      connectBusy: true,
    })
    expect(fromClick.connectionLabel).toBe('Connecting')
    expect(fromClick.connectVisual).toBe('connecting')
    expect(fromClick.showProtectedBadge).toBe(false)
  })

  it('connected and healthy', () => {
    const m = vm({ state: connected() })
    expect(m.connectionLabel).toBe('Protected')
    expect(m.connectVisual).toBe('connected')
    expect(m.showProtectedBadge).toBe(true)
    expect(m.homeTrafficHealthSubtitle).toBe('Proxy · Ready')
    expect(m.homeAlertTooltip).toBe('')
  })

  it('connected over TUN', () => {
    const m = vm({ state: connected({}, { traffic: 'tun' }) })
    expect(m.displayTraffic).toBe('tun')
    expect(m.homeTrafficHealthSubtitle).toBe('TUN · Ready')
  })

  it('connected but broken', () => {
    const m = vm({ state: connected({ health: 'broken' }) })
    expect(m.connectionLabel).toBe('Needs attention')
    expect(m.connectVisual).toBe('broken')
    expect(m.showProtectedBadge).toBe(false)
    expect(m.homeTrafficHealthSubtitle).toBe('Proxy · Broken')
    expect(m.homeAlertTooltip).not.toBe('')
  })

  it('connected and degraded: still protected, hint plus warning in the tooltip', () => {
    const m = vm({
      state: connected({ health: 'degraded', lastWarning: ' DNS slow ' }),
    })
    expect(m.connectionLabel).toBe('Protected')
    expect(m.showProtectedBadge).toBe(true)
    const parts = m.homeAlertTooltip.split('\n\n')
    expect(parts).toHaveLength(2)
    expect(parts[1]).toBe('DNS slow')
  })

  it('error', () => {
    const m = vm({
      state: {
        profile: PROFILES,
        connection: { status: 'error', lastWarning: 'ignored when down' },
      },
    })
    expect(m.connectionLabel).toBe('Problem')
    expect(m.connectVisual).toBe('error')
    expect(m.showProtectedBadge).toBe(false)
    expect(m.homeAlertTooltip).toBe('')
  })

  it('disconnecting', () => {
    const m = vm({
      state: { profile: PROFILES, connection: { status: 'disconnecting' } },
    })
    expect(m.connectionLabel).toBe('Disconnecting')
    expect(m.connectVisual).toBe('idle')
  })
})

describe('useHomeViewModel: mode and traffic', () => {
  it('shows the optimistic pick over the snapshot', () => {
    const state = connected({}, { mode: { current: 'rule' }, traffic: 'proxy' })
    const m = vm({ state, optimisticMode: 'global', optimisticTraffic: 'tun' })
    expect(m.displayMode).toBe('global')
    expect(m.displayTraffic).toBe('tun')
    expect(m.homeTrafficHealthSubtitle).toBe('TUN · Ready')
  })
})

describe('useHomeViewModel: active node', () => {
  const groups = [
    { name: 'GLOBAL', type: 'Selector', selected: 'Auto', proxies: ['Auto'] },
    {
      name: 'Auto',
      type: 'Selector',
      selected: '🇩🇪 Frankfurt',
      proxies: ['🇩🇪 Frankfurt', '🇳🇱 Amsterdam'],
    },
    { name: 'Chain', type: 'Chain', selected: 'hop1', proxies: ['hop1'] },
  ]

  it('follows GLOBAL into the nested group', () => {
    const m = vm({
      state: connected({}, { proxy: { activeGroup: 'GLOBAL', groups } }),
    })
    expect(m.activeNode).toBe('Auto -> 🇩🇪 Frankfurt')
    expect(m.activeNodeVisual.text).toBe('🇩🇪 Frankfurt')
    expect(m.activeNodeVisual.iso).toBe('DE')
    expect(m.nodePickerGroup?.name).toBe('Auto')
  })

  it('uses a plain group directly', () => {
    const m = vm({
      state: connected({}, { proxy: { activeGroup: 'Auto', groups } }),
    })
    expect(m.activeNode).toBe('🇩🇪 Frankfurt')
    expect(m.nodePickerGroup?.name).toBe('Auto')
  })

  it('never offers a chain as a node picker', () => {
    const m = vm({
      state: connected({}, { proxy: { activeGroup: 'Chain', groups } }),
    })
    expect(m.activeNode).toBe('hop1')
    expect(m.nodePickerGroup).toBeNull()
  })

  it('is empty for an unknown active group', () => {
    const m = vm({
      state: connected({}, { proxy: { activeGroup: 'Gone', groups } }),
    })
    expect(m.activeNode).toBe('')
    expect(m.nodePickerGroup).toBeNull()
  })
})

describe('useHomeViewModel: update tooltip', () => {
  it('names the version when known', () => {
    expect(
      vm({ updateSnap: { hasUpdate: true, latestVersion: ' 1.0.0 ' } })
        .homeUpdateTooltip,
    ).toContain('1.0.0')
    expect(vm({ updateSnap: { hasUpdate: true } }).homeUpdateTooltip).not.toBe(
      '',
    )
    expect(
      vm({ updateSnap: { hasUpdate: false, latestVersion: '1.0.0' } })
        .homeUpdateTooltip,
    ).toBe('')
  })
})

describe('useHomeViewModel: Desync chip', () => {
  const rule = (extra = {}) =>
    connected({}, { mode: { current: 'rule' }, traffic: 'tun', ...extra })

  it('is hidden without a snapshot, with Desync off, or with no service on', () => {
    expect(vm({ state: rule() }).desyncChip).toBeNull()
    expect(
      vm({
        state: rule(),
        desync: desyncSnapshot(desyncStatus({ settings: { enabled: false } })),
      }).desyncChip,
    ).toBeNull()
    expect(
      vm({
        state: rule(),
        desync: desyncSnapshot(
          desyncStatus({
            settings: { youtube: false, discord: false, discordUDP: false },
          }),
        ),
      }).desyncChip,
    ).toBeNull()
  })

  it('lists enabled services with their tone and the full status as tooltip', () => {
    const chip = vm({
      state: rule(),
      desync: desyncSnapshot(
        desyncStatus(),
        desyncAuto({
          youtube: desyncService('ready'),
          discord: desyncService('testing', { attempt: 1, total: 3 }),
        }),
      ),
    }).desyncChip
    expect(chip?.services).toEqual([
      { key: 'youtube', name: 'YouTube', tone: 'ok' },
      { key: 'discord', name: 'Discord', tone: 'pending' },
    ])
    expect(chip?.tooltip).toBe(
      [
        'YouTube: HTTPS available · automatic',
        'Discord: Finding a strategy · 1/3',
        'Discord voice: UDP exchange detected',
      ].join('\n'),
    )
  })

  it('skips a service that is switched off', () => {
    const chip = vm({
      state: rule(),
      desync: desyncSnapshot(desyncStatus({ settings: { youtube: false } })),
    }).desyncChip
    expect(chip?.services.map((s) => s.key)).toEqual(['discord'])
  })

  it('an interrupted voice line raises a working Discord to pending', () => {
    // Interrupted voice is still recovering: yellow, not red.
    const chip = vm({
      state: rule(),
      desync: desyncSnapshot(
        desyncStatus(),
        desyncAuto({ voice: desyncService('interrupted') }),
      ),
    }).desyncChip
    expect(chip?.services[1]).toMatchObject({ key: 'discord', tone: 'pending' })
  })

  it('a reserve whose check failed is pending, not a problem', () => {
    const chip = vm({
      state: rule(),
      desync: desyncSnapshot(
        desyncStatus(),
        desyncAuto({ youtube: desyncService('profile_failed') }),
      ),
    }).desyncChip
    expect(chip?.services[0]).toMatchObject({ key: 'youtube', tone: 'pending' })
  })

  it('a voice line without a working strategy raises Discord to a problem', () => {
    const chip = vm({
      state: rule(),
      desync: desyncSnapshot(
        desyncStatus({ desyncOnly: true }),
        desyncAuto({ voice: desyncService('no_strategy') }),
      ),
    }).desyncChip
    expect(chip?.services[1]).toMatchObject({ key: 'discord', tone: 'problem' })
  })

  it('voice needing TUN does not mark a working Discord as a problem', () => {
    const chip = vm({
      state: rule({ traffic: 'proxy' }),
      desync: desyncSnapshot(),
    }).desyncChip
    expect(chip?.services[1]).toMatchObject({ key: 'discord', tone: 'ok' })
    expect(chip?.tooltip).toContain('Discord voice: Enable TUN for voice')
  })

  it('gates on connection and Rule mode like the Settings card', () => {
    const off = vm({
      state: { ...rule(), connection: { status: 'disconnected' } },
      desync: desyncSnapshot(),
    }).desyncChip
    expect(off?.services.map((s) => s.tone)).toEqual(['off', 'off'])
    expect(off?.tooltip).toContain('YouTube: Connect on Home')

    const global = vm({
      state: rule({ mode: { current: 'global' } }),
      desync: desyncSnapshot(),
    }).desyncChip
    expect(global?.tooltip).toContain('YouTube: Select Rule mode')
  })

  describe('custom domains', () => {
    const entries = [
      { id: 'a1', domain: 'example.org', enabled: true },
      { id: 'b2', domain: 'example.net', enabled: true },
      { id: 'c3', domain: 'off.example.com', enabled: false },
    ]
    const chipWith = (custom: Record<string, string>, settings = {}) =>
      vm({
        state: rule(),
        desync: desyncSnapshot(
          desyncStatus({ settings: { custom: entries, ...settings } }),
          desyncAuto({
            custom: Object.fromEntries(
              Object.entries(custom).map(([id, p]) => [id, desyncService(p)]),
            ),
          }),
        ),
      }).desyncChip

    it('adds one item counting the switched-on entries, each in the tooltip', () => {
      const chip = chipWith({ a1: 'ready', b2: 'not_needed' })
      expect(chip?.services.at(-1)).toEqual({
        key: 'custom',
        name: 'Sites 2',
        tone: 'ok',
      })
      expect(chip?.tooltip).toContain(
        'example.org: HTTPS available · automatic',
      )
      expect(chip?.tooltip).toContain(
        'example.net: Not blocked · Desync not needed',
      )
      expect(chip?.tooltip).not.toContain('off.example.com')
    })

    it('a partly unavailable set is pending, not a problem', () => {
      expect(
        chipWith({ a1: 'ready', b2: 'unavailable' })?.services.at(-1)?.tone,
      ).toBe('pending')
      expect(
        chipWith({ a1: 'unavailable', b2: 'unavailable' })?.services.at(-1)
          ?.tone,
      ).toBe('problem')
      expect(
        chipWith({ a1: 'testing', b2: 'ready' })?.services.at(-1)?.tone,
      ).toBe('pending')
    })

    it('shows the chip for custom-only Desync', () => {
      const chip = chipWith(
        { a1: 'ready', b2: 'ready' },
        { youtube: false, discord: false, discordUDP: false },
      )
      expect(chip?.services.map((s) => s.key)).toEqual(['custom'])
    })
  })

  describe('presets', () => {
    // Stored Instagram first: the chip still follows the catalog order.
    const entries = [
      { id: 'ig', domain: '', preset: 'instagram', enabled: true },
      { id: 'a1', domain: 'example.org', enabled: true },
      { id: 'fb', domain: '', preset: 'facebook', enabled: true },
      { id: 'xx', domain: '', preset: 'x', enabled: false },
      { id: 'b2', domain: 'example.net', enabled: true },
    ]
    const chipWith = (custom: Record<string, string>, settings = {}) =>
      vm({
        state: rule(),
        desyncPresets: DESYNC_PRESETS,
        desync: desyncSnapshot(
          desyncStatus({ settings: { custom: entries, ...settings } }),
          desyncAuto({
            custom: Object.fromEntries(
              Object.entries(custom).map(([id, p]) => [id, desyncService(p)]),
            ),
          }),
        ),
      }).desyncChip

    it('names each switched-on preset like a service, before the sites', () => {
      const chip = chipWith({
        fb: 'ready',
        ig: 'unavailable',
        a1: 'ready',
        b2: 'ready',
      })
      expect(chip?.services).toEqual([
        { key: 'youtube', name: 'YouTube', tone: 'ok' },
        { key: 'discord', name: 'Discord', tone: 'ok' },
        { key: 'preset:facebook', name: 'Facebook', tone: 'ok' },
        { key: 'preset:instagram', name: 'Instagram', tone: 'problem' },
        { key: 'custom', name: 'Sites 2', tone: 'ok' },
      ])
      expect(chip?.tooltip).toContain('Facebook: HTTPS available · automatic')
      expect(chip?.tooltip).toContain(
        "Instagram: Desync can't unblock this site",
      )
      expect(chip?.tooltip).not.toContain('X (Twitter)')
    })

    it('shows the chip for preset-only Desync', () => {
      const chip = vm({
        state: rule(),
        desyncPresets: DESYNC_PRESETS,
        desync: desyncSnapshot(
          desyncStatus({
            settings: {
              youtube: false,
              discord: false,
              discordUDP: false,
              custom: [entries[2]],
            },
          }),
        ),
      }).desyncChip
      expect(chip?.services.map((s) => s.name)).toEqual(['Facebook'])
    })
  })
})
