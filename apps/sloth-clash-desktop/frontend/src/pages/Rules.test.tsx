import { fireEvent, screen, waitFor, within } from '@testing-library/react'
import { useMemo, useState } from 'react'
import { describe, expect, it } from 'vitest'

import { useRulesOverview } from '../hooks/queries/useRulesOverview'
import { parseMihomoRulesJson } from '../rulesTable'
import { mihomoRulesBody, RULE_POLICIES, RULE_TYPES } from '../test/fixtures'
import { renderHookWithProviders, renderWithProviders } from '../test/render'
import { wailsApp } from '../test/wailsMock'

import { type RuleToggleApi, RulesPage } from './Rules'

// Regression: on profiles with tens of thousands of rules the Rules screen
// rendered nothing. These tests drive the real data path (FetchRulesOverview
// binding -> useRulesOverview -> parseMihomoRulesJson -> RulesPage) with a
// large body and assert the rows, counters and filters.
//
// jsdom cost note (measured): inserting/removing children of a connected node
// is O(n) per operation in jsdom, so narrowing a mounted 20k-row table via the
// filter controls takes 30s+ there (a browser does it in milliseconds). The
// 20k tests therefore mount once (cheap) with the filter state they assert,
// and the control interactions run on a smaller, still multi-thousand list.

const RULE_COUNT = 20_000
const TOTAL_ROWS = RULE_COUNT + 1 // + trailing MATCH

const noProfileToggle: RuleToggleApi = {
  hasActiveProfile: false,
  baselineLoading: false,
  baselineError: null,
  busyLines: new Set(),
  isToggleable: () => false,
  matchLine: () => null,
  onDisable: () => {},
  onEnable: () => {},
}

type Filters = { search?: string; type?: string; policy?: string }

function sortedUnique(values: string[]): string[] {
  return [...new Set(values.filter(Boolean))].sort((a, b) => a.localeCompare(b))
}

// Mirrors the Rules wiring in App.tsx (rows, filter options, filtered rows,
// type summary) without the rule-toggle baseline, which needs an active
// profile and is covered by useRuleToggles.test.ts.
const NO_FILTERS: Filters = {}

function RulesScreen({ initial = NO_FILTERS }: { initial?: Filters }) {
  const { overview, busy, refresh } = useRulesOverview(true)
  const [search, setSearch] = useState(initial.search ?? '')
  const [typeFilter, setTypeFilter] = useState(initial.type ?? 'all')
  const [policyFilter, setPolicyFilter] = useState(initial.policy ?? 'all')
  const rows = useMemo(
    () => parseMihomoRulesJson(overview?.rulesBody),
    [overview?.rulesBody],
  )
  const typeOptions = useMemo(
    () => sortedUnique(rows.map((r) => r.type)),
    [rows],
  )
  const policyOptions = useMemo(
    () => sortedUnique(rows.map((r) => r.proxy)),
    [rows],
  )
  const filtered = useMemo(() => {
    const q = search.trim().toLowerCase()
    return rows
      .map((r) => ({ ...r, disabled: false as const }))
      .filter((r) => {
        if (typeFilter !== 'all' && r.type !== typeFilter) return false
        if (policyFilter !== 'all' && r.proxy !== policyFilter) return false
        if (!q) return true
        return `${r.type} ${r.payload} ${r.proxy}`.toLowerCase().includes(q)
      })
  }, [rows, search, typeFilter, policyFilter])
  const typeTop = useMemo(() => {
    const counts = new Map<string, number>()
    for (const r of filtered) counts.set(r.type, (counts.get(r.type) ?? 0) + 1)
    return [...counts.entries()].sort((a, b) => b[1] - a[1]).slice(0, 6)
  }, [filtered])
  return (
    <RulesPage
      rulesOverview={overview}
      connectionStatus="connected"
      rulesBusy={busy}
      providers={[]}
      providerBusyMap={{}}
      providerErrMap={{}}
      bulkBusy={false}
      rulesRows={rows}
      filteredRulesRows={filtered}
      rulesTypeTop={typeTop}
      ruleSearch={search}
      ruleTypeFilter={typeFilter}
      rulePolicyFilter={policyFilter}
      ruleTypeOptions={typeOptions}
      rulePolicyOptions={policyOptions}
      error={null}
      onRefresh={refresh}
      onRefreshAll={() => {}}
      onRefreshOne={() => {}}
      onSearchChange={setSearch}
      onTypeFilterChange={setTypeFilter}
      onPolicyFilterChange={setPolicyFilter}
      ruleToggle={noProfileToggle}
    />
  )
}

function stubRules(rulesBody: string) {
  return wailsApp.stub('FetchRulesOverview', () => ({
    controller: '127.0.0.1:9097',
    reachable: true,
    rulesBody,
    ruleProvidersBody: '{"providers":{}}',
  }))
}

function bodyRows(container: HTMLElement) {
  return container.querySelectorAll<HTMLTableRowElement>('tbody > tr')
}

// Text queries scoped to the summary chips: screen-wide getByText walks every
// cell of a 20k-row table.
function summary(container: HTMLElement) {
  const row = container.querySelector<HTMLElement>('.rulesSummaryRow')
  if (!row) throw new Error('summary row not rendered')
  return within(row)
}

// Compare plain numbers: a failing toHaveLength on a 20k-node NodeList
// pretty-prints every node on each waitFor retry and stalls the run.
function rowCount(container: HTMLElement): number {
  return bodyRows(container).length
}

function cellTexts(row: HTMLTableRowElement | undefined): string[] {
  // [toggle, #, type, match, policy] -> drop the checkbox cell.
  return [...(row?.cells ?? [])].slice(1).map((c) => c.textContent ?? '')
}

// How many of the first `count` fixture rules have the given type / policy.
function fixtureCount(count: number, type?: string, policy?: string): number {
  let n = 0
  for (let i = 0; i < count; i++) {
    if (type && RULE_TYPES[i % RULE_TYPES.length] !== type) continue
    if (policy && RULE_POLICIES[i % RULE_POLICIES.length] !== policy) continue
    n++
  }
  return n
}

function filterSelects(container: HTMLElement) {
  const [type, policy] = container.querySelectorAll('select')
  if (!type || !policy) throw new Error('rule filter selects not rendered')
  return { type, policy }
}

describe('useRulesOverview', () => {
  it('hands the full large rules body through to the parser', async () => {
    const body = mihomoRulesBody(RULE_COUNT)
    const fetch = stubRules(body)
    const { result } = renderHookWithProviders(() => useRulesOverview(true))
    await waitFor(() => expect(result.current.overview).not.toBeNull())
    expect(fetch).toHaveBeenCalledTimes(1)
    expect(result.current.overview?.rulesBody?.length).toBe(body.length)
    expect(
      parseMihomoRulesJson(result.current.overview?.rulesBody).length,
    ).toBe(TOTAL_ROWS)
  })

  it('does not fetch while the Rules screen is closed', () => {
    const fetch = stubRules(mihomoRulesBody(1))
    renderHookWithProviders(() => useRulesOverview(false))
    expect(fetch).not.toHaveBeenCalled()
  })
})

describe('RulesPage with a large rule list', () => {
  it(`renders all ${TOTAL_ROWS} rows with counts and filter options`, async () => {
    stubRules(mihomoRulesBody(RULE_COUNT))
    const { container } = renderWithProviders(<RulesScreen />)

    await waitFor(() => expect(rowCount(container)).toBe(TOTAL_ROWS))
    expect(
      summary(container).getByText(`Total: ${TOTAL_ROWS}/${TOTAL_ROWS}`),
    ).toBeVisible()
    for (const type of RULE_TYPES) {
      expect(
        summary(container).getByText(
          `${type}: ${fixtureCount(RULE_COUNT, type)}`,
        ),
      ).toBeVisible()
    }

    const rows = bodyRows(container)
    expect(cellTexts(rows[0])).toEqual([
      '0',
      'DomainSuffix',
      'host-0.example.com',
      'DIRECT',
    ])
    expect(cellTexts(rows[RULE_COUNT - 1])).toEqual([
      String(RULE_COUNT - 1),
      'Domain',
      `host-${RULE_COUNT - 1}.example.com`,
      'Proxy',
    ])
    expect(cellTexts(rows[RULE_COUNT])).toEqual([
      String(RULE_COUNT),
      'Match',
      '—',
      'Proxy',
    ])

    // No active profile: toggles are shown but read-only.
    expect(screen.getByText(/Activate a profile/)).toBeVisible()
    expect(rows[0]?.querySelector('input[type="checkbox"]')).toBeDisabled()

    const { type, policy } = filterSelects(container)
    // "All …" + every distinct value, sorted.
    expect([...type.options].map((o) => o.value)).toEqual([
      'all',
      ...[...RULE_TYPES, 'Match'].sort((a, b) => a.localeCompare(b)),
    ])
    expect([...policy.options].map((o) => o.value)).toEqual([
      'all',
      ...[...RULE_POLICIES].sort((a, b) => a.localeCompare(b)),
    ])
  }, 60_000)

  it('renders a filtered window of the large list', async () => {
    stubRules(mihomoRulesBody(RULE_COUNT))
    const { container } = renderWithProviders(
      <RulesScreen initial={{ type: 'DomainSuffix', policy: 'DIRECT' }} />,
    )
    const want = fixtureCount(RULE_COUNT, 'DomainSuffix', 'DIRECT')
    expect(want).toBeGreaterThan(3000)

    await waitFor(() => expect(rowCount(container)).toBe(want))
    expect(
      summary(container).getByText(`Total: ${want}/${TOTAL_ROWS}`),
    ).toBeVisible()
    expect(summary(container).getByText(`DomainSuffix: ${want}`)).toBeVisible()
    const { type, policy } = filterSelects(container)
    expect(type).toHaveValue('DomainSuffix')
    expect(policy).toHaveValue('DIRECT')
    const rows = bodyRows(container)
    expect(cellTexts(rows[want - 1])).toEqual([
      '19998',
      'DomainSuffix',
      'host-19998.example.com',
      'DIRECT',
    ])
    expect(
      [...rows].every((r) => {
        const [, t, , p] = cellTexts(r)
        return t === 'DomainSuffix' && p === 'DIRECT'
      }),
    ).toBe(true)
  }, 60_000)

  it('narrows and restores rows through the filter controls', async () => {
    const count = 3_000
    const total = count + 1
    stubRules(mihomoRulesBody(count))
    const { container } = renderWithProviders(<RulesScreen />)
    await waitFor(() => expect(rowCount(container)).toBe(total))
    const { type, policy } = filterSelects(container)

    const geoip = fixtureCount(count, 'GeoIP')
    fireEvent.change(type, { target: { value: 'GeoIP' } })
    await waitFor(() => expect(rowCount(container)).toBe(geoip))
    expect(
      summary(container).getByText(`Total: ${geoip}/${total}`),
    ).toBeVisible()

    // GeoIP (i % 6 == 3) and REJECT (i % 3 == 2) never overlap.
    fireEvent.change(policy, { target: { value: 'REJECT' } })
    await waitFor(() => expect(rowCount(container)).toBe(0))
    expect(summary(container).getByText(`Total: 0/${total}`)).toBeVisible()

    fireEvent.change(type, { target: { value: 'all' } })
    await waitFor(() =>
      expect(rowCount(container)).toBe(
        fixtureCount(count, undefined, 'REJECT'),
      ),
    )
    fireEvent.change(policy, { target: { value: 'all' } })
    await waitFor(() => expect(rowCount(container)).toBe(total))

    // Search is case-insensitive over type / match / policy.
    fireEvent.change(
      screen.getByPlaceholderText('Search rule / match / policy'),
      {
        target: { value: 'HOST-2999.example' },
      },
    )
    await waitFor(() => expect(rowCount(container)).toBe(1))
    expect(cellTexts(bodyRows(container)[0])).toEqual([
      '2999',
      'ProcessName',
      'host-2999.example.com',
      'REJECT',
    ])
  }, 60_000)

  it('shows the empty-state hint when the core returns no rules', async () => {
    const fetch = stubRules('{"rules":[]}')
    const { container } = renderWithProviders(<RulesScreen />)
    await waitFor(() => expect(fetch).toHaveBeenCalled())
    expect(
      await screen.findByText(
        'The core returned no rules. Refresh to check again.',
      ),
    ).toBeVisible()
    expect(rowCount(container)).toBe(0)
  })
})
