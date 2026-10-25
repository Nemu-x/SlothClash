import { describe, expect, it } from 'vitest'

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
