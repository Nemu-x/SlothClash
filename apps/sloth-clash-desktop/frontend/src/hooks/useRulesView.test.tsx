import { act, waitFor } from '@testing-library/react'
import { describe, expect, it } from 'vitest'

import type { RuleRow } from '../rulesTable'
import { renderHookWithProviders } from '../test/render'
import { wailsApp } from '../test/wailsMock'

import {
  combineRuleRows,
  filterRuleRows,
  ruleFilterOptions,
  ruleTypeTop,
  useRulesView,
} from './useRulesView'
import type { DisabledRuleRow } from './useRuleToggles'

function row(idx: number, type: string, payload: string, proxy: string) {
  return { idx, type, payload, proxy } satisfies RuleRow
}

const ROWS: RuleRow[] = [
  row(0, 'DomainSuffix', 'example.com', 'Proxy'),
  row(1, 'Domain', 'api.Example.org', 'DIRECT'),
  row(2, 'DomainSuffix', 'ads.net', 'REJECT'),
  row(3, 'GeoIP', 'CN', 'DIRECT'),
  row(4, 'Match', '', 'Proxy'),
]

const ALL = { search: '', type: 'all', policy: 'all' }

describe('ruleFilterOptions', () => {
  it('returns distinct, trimmed, sorted values without blanks', () => {
    const rows = [...ROWS, row(5, ' GeoIP ', 'x', ''), row(6, '', 'y', '  ')]
    expect(ruleFilterOptions(rows, 'type')).toEqual([
      'Domain',
      'DomainSuffix',
      'GeoIP',
      'Match',
    ])
    expect(ruleFilterOptions(rows, 'proxy')).toEqual([
      'DIRECT',
      'Proxy',
      'REJECT',
    ])
  })

  it('is empty for no rows', () => {
    expect(ruleFilterOptions([], 'type')).toEqual([])
  })
})

describe('filterRuleRows', () => {
  it('keeps everything with no filters', () => {
    expect(filterRuleRows(ROWS, ALL)).toEqual(ROWS)
  })

  it('matches type and policy exactly', () => {
    expect(
      filterRuleRows(ROWS, { ...ALL, type: 'DomainSuffix' }).map((r) => r.idx),
    ).toEqual([0, 2])
    expect(
      filterRuleRows(ROWS, { ...ALL, policy: 'DIRECT' }).map((r) => r.idx),
    ).toEqual([1, 3])
    expect(
      filterRuleRows(ROWS, { ...ALL, type: 'Domain', policy: 'DIRECT' }).map(
        (r) => r.idx,
      ),
    ).toEqual([1])
    // Exact, not substring / case-insensitive.
    expect(filterRuleRows(ROWS, { ...ALL, type: 'domain' })).toEqual([])
  })

  it('searches type, match and policy case-insensitively, trimmed', () => {
    const ids = (search: string) =>
      filterRuleRows(ROWS, { ...ALL, search }).map((r) => r.idx)
    expect(ids('  EXAMPLE.')).toEqual([0, 1])
    expect(ids('geoip')).toEqual([3])
    expect(ids('reject')).toEqual([2])
    expect(ids('   ')).toEqual([0, 1, 2, 3, 4])
    expect(ids('no-such-rule')).toEqual([])
  })

  it('combines the search with the filters', () => {
    expect(
      filterRuleRows(ROWS, { search: 'example', type: 'all', policy: 'Proxy' }),
    ).toEqual([ROWS[0]])
  })
})

describe('ruleTypeTop', () => {
  it('counts types, most frequent first, at most six', () => {
    const rows = [
      ...['A', 'B', 'B', 'C', 'C', 'C', 'D', 'E', 'F', 'G'].map((t, i) =>
        row(i, t, '', 'DIRECT'),
      ),
    ]
    const top = ruleTypeTop(rows)
    expect(top).toHaveLength(6)
    expect(top.slice(0, 2)).toEqual([
      ['C', 3],
      ['B', 2],
    ])
    expect(top.every(([, n]) => n >= 1)).toBe(true)
  })

  it('buckets a blank type under a dash', () => {
    expect(ruleTypeTop([row(0, '', 'x', 'DIRECT')])).toEqual([['—', 1]])
  })

  it('is empty for no rows', () => {
    expect(ruleTypeTop([])).toEqual([])
  })
})

describe('combineRuleRows', () => {
  const disabled: DisabledRuleRow = {
    ...row(-1, 'DOMAIN', 'off.example', 'DIRECT'),
    disabled: true,
    line: 'DOMAIN,off.example,DIRECT',
  }
  const lineOf = (r: RuleRow) =>
    r.payload ? `${r.type},${r.payload},${r.proxy}` : null

  it('tags live rows enabled and appends the disabled ones', () => {
    const out = combineRuleRows(ROWS, lineOf, new Set(), [disabled])
    expect(out).toHaveLength(ROWS.length + 1)
    expect(out.slice(0, ROWS.length).every((r) => r.disabled === false)).toBe(
      true,
    )
    expect(out.at(-1)).toBe(disabled)
  })

  it('hides live rows whose exact line is optimistically disabled', () => {
    const out = combineRuleRows(
      ROWS,
      lineOf,
      new Set(['DomainSuffix,ads.net,REJECT']),
      [],
    )
    expect(out.map((r) => r.idx)).toEqual([0, 1, 3, 4])
  })

  it('keeps rows that match no baseline line', () => {
    const out = combineRuleRows(ROWS, () => null, new Set(['anything']), [])
    expect(out).toHaveLength(ROWS.length)
  })
})

function stubOverview(rules: unknown[], providers: Record<string, unknown>) {
  return wailsApp.stub('FetchRulesOverview', () => ({
    controller: '127.0.0.1:9097',
    reachable: true,
    rulesBody: JSON.stringify({ rules }),
    ruleProvidersBody: JSON.stringify({ providers }),
  }))
}

const LIVE_RULES = [
  { type: 'DomainSuffix', payload: 'example.com', proxy: 'Proxy' },
  { type: 'DomainSuffix', payload: 'ads.net', proxy: 'REJECT' },
  { type: 'GeoIP', payload: 'CN', proxy: 'DIRECT' },
  { type: 'Match', payload: '', proxy: 'Proxy' },
]

const PROVIDERS = {
  zeta: { behavior: 'domain', ruleCount: 10, vehicleType: 'HTTP' },
  alpha: { behavior: 'ipcidr', ruleCount: 5, vehicleType: 'HTTP' },
}

function renderRulesView(enabled = true) {
  return renderHookWithProviders(() =>
    useRulesView({
      enabled,
      activeProfileId: '',
      profiles: undefined,
      onError: () => {},
    }),
  )
}

describe('useRulesView', () => {
  it('parses the snapshot into rows, options, providers and counts', async () => {
    stubOverview(LIVE_RULES, PROVIDERS)
    const { result } = renderRulesView()
    await waitFor(() => expect(result.current.rulesRows).toHaveLength(4))
    const v = result.current
    expect(v.filteredRulesRows).toHaveLength(4)
    expect(v.filteredRulesRows.every((r) => r.disabled === false)).toBe(true)
    expect(v.ruleTypeOptions).toEqual(['DomainSuffix', 'GeoIP', 'Match'])
    expect(v.rulePolicyOptions).toEqual(['DIRECT', 'Proxy', 'REJECT'])
    expect(v.rulesTypeTop).toEqual([
      ['DomainSuffix', 2],
      ['GeoIP', 1],
      ['Match', 1],
    ])
    expect(v.providers.map((p) => p.name)).toEqual(['alpha', 'zeta'])
    expect(v.ruleToggle.hasActiveProfile).toBe(false)
  })

  it('narrows rows and the type summary through the filter setters', async () => {
    stubOverview(LIVE_RULES, {})
    const { result } = renderRulesView()
    await waitFor(() => expect(result.current.rulesRows).toHaveLength(4))

    act(() => result.current.onTypeFilterChange('DomainSuffix'))
    expect(result.current.ruleTypeFilter).toBe('DomainSuffix')
    expect(result.current.filteredRulesRows.map((r) => r.payload)).toEqual([
      'example.com',
      'ads.net',
    ])
    expect(result.current.rulesTypeTop).toEqual([['DomainSuffix', 2]])

    act(() => result.current.onPolicyFilterChange('REJECT'))
    expect(result.current.filteredRulesRows.map((r) => r.payload)).toEqual([
      'ads.net',
    ])

    act(() => result.current.onSearchChange('EXAMPLE'))
    expect(result.current.ruleSearch).toBe('EXAMPLE')
    expect(result.current.filteredRulesRows).toEqual([])
    expect(result.current.rulesTypeTop).toEqual([])

    act(() => {
      result.current.onTypeFilterChange('all')
      result.current.onPolicyFilterChange('all')
    })
    expect(result.current.filteredRulesRows.map((r) => r.payload)).toEqual([
      'example.com',
    ])
    // Options and totals always describe the unfiltered snapshot.
    expect(result.current.rulesRows).toHaveLength(4)
    expect(result.current.ruleTypeOptions).toHaveLength(3)
  })

  it('does not fetch while the Rules screen is closed', () => {
    const fetch = stubOverview(LIVE_RULES, {})
    const { result } = renderRulesView(false)
    expect(fetch).not.toHaveBeenCalled()
    expect(result.current.rulesRows).toEqual([])
    expect(result.current.filteredRulesRows).toEqual([])
    expect(result.current.rulesTypeTop).toEqual([])
  })

  it('tracks one provider update: busy, error, cleared on retry', async () => {
    const fetch = stubOverview(LIVE_RULES, PROVIDERS)
    let fail = true
    const update = wailsApp.stub('UpdateRuleProvider', () =>
      fail ? Promise.reject(new Error('timeout')) : undefined,
    )
    const { result } = renderRulesView()
    await waitFor(() => expect(result.current.providers).toHaveLength(2))

    act(() => result.current.onRefreshOne('  alpha '))
    expect(result.current.providerBusyMap).toEqual({ alpha: true })
    await waitFor(() =>
      expect(result.current.providerBusyMap.alpha).toBe(false),
    )
    expect(update).toHaveBeenCalledWith('alpha')
    expect(result.current.providerErrMap.alpha).toContain('timeout')

    fail = false
    const fetchesBefore = fetch.mock.calls.length
    act(() => result.current.onRefreshOne('alpha'))
    // A retry drops the stale error right away.
    expect(result.current.providerErrMap).toEqual({})
    await waitFor(() =>
      expect(result.current.providerBusyMap.alpha).toBe(false),
    )
    expect(result.current.providerErrMap).toEqual({})
    // A successful update refetches the snapshot.
    await waitFor(() =>
      expect(fetch.mock.calls.length).toBeGreaterThan(fetchesBefore),
    )
  })

  it('ignores a blank provider name', () => {
    stubOverview(LIVE_RULES, PROVIDERS)
    const update = wailsApp.stub('UpdateRuleProvider', () => undefined)
    const { result } = renderRulesView()
    act(() => result.current.onRefreshOne('   '))
    expect(update).not.toHaveBeenCalled()
    expect(result.current.providerBusyMap).toEqual({})
  })

  it('updates every provider in order and keeps per-provider errors', async () => {
    stubOverview(LIVE_RULES, PROVIDERS)
    const update = wailsApp.stub('UpdateRuleProvider', (name: string) =>
      name === 'zeta' ? Promise.reject(new Error('404')) : undefined,
    )
    const { result } = renderRulesView()
    await waitFor(() => expect(result.current.providers).toHaveLength(2))

    act(() => result.current.onRefreshAll())
    expect(result.current.bulkBusy).toBe(true)
    expect(result.current.providerBusyMap).toEqual({ alpha: true, zeta: true })
    await waitFor(() => expect(result.current.bulkBusy).toBe(false))
    expect(update.mock.calls.map((c) => c[0])).toEqual(['alpha', 'zeta'])
    expect(result.current.providerBusyMap).toEqual({
      alpha: false,
      zeta: false,
    })
    expect(Object.keys(result.current.providerErrMap)).toEqual(['zeta'])
    expect(result.current.providerErrMap.zeta).toContain('404')
  })

  it('drops stale single-provider errors when a bulk update succeeds', async () => {
    stubOverview(LIVE_RULES, PROVIDERS)
    let fail = true
    wailsApp.stub('UpdateRuleProvider', () =>
      fail ? Promise.reject(new Error('timeout')) : undefined,
    )
    const { result } = renderRulesView()
    await waitFor(() => expect(result.current.providers).toHaveLength(2))

    act(() => result.current.onRefreshOne('alpha'))
    await waitFor(() =>
      expect(result.current.providerErrMap.alpha).toContain('timeout'),
    )

    fail = false
    act(() => result.current.onRefreshAll())
    await waitFor(() => expect(result.current.bulkBusy).toBe(false))
    expect(result.current.providerErrMap).toEqual({})
  })

  it('skips the bulk update when there are no providers', async () => {
    stubOverview(LIVE_RULES, {})
    const update = wailsApp.stub('UpdateRuleProvider', () => undefined)
    const { result } = renderRulesView()
    await waitFor(() => expect(result.current.rulesRows).toHaveLength(4))
    act(() => result.current.onRefreshAll())
    expect(result.current.bulkBusy).toBe(false)
    expect(update).not.toHaveBeenCalled()
  })
})
