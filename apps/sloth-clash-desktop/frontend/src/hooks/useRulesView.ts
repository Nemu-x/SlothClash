import { useCallback, useMemo, useState } from 'react'

import type { main } from '../api/models'
import { UpdateRuleProvider } from '../api/rules'
import {
  parseMihomoRulesJson,
  parseRuleProvidersJson,
  type RuleProviderRow,
  type RuleRow,
} from '../rulesTable'

import { useRulesOverview } from './queries/useRulesOverview'
import { type DisabledRuleRow, useRuleToggles } from './useRuleToggles'

// A rule row as rendered on the dashboard: a live rule (disabled=false) or a
// user-disabled baseline rule (disabled=true, carrying its exact `line`).
export type DashboardRuleRow = RuleRow & { disabled?: boolean; line?: string }

export type RuleToggleApi = {
  hasActiveProfile: boolean
  baselineLoading: boolean
  baselineError: string | null
  busyLines: Set<string>
  isToggleable: (row: RuleRow) => boolean
  matchLine: (row: RuleRow) => string | null
  onDisable: (row: RuleRow) => void
  onEnable: (line: string) => void
}

export type RuleFilters = { search: string; type: string; policy: string }

/** Distinct non-empty values of `key` across `rows`, sorted for a filter <select>. */
export function ruleFilterOptions(
  rows: readonly RuleRow[],
  key: 'type' | 'proxy',
): string[] {
  const set = new Set<string>()
  for (const r of rows) {
    const v = String(r[key] ?? '').trim()
    if (v) set.add(v)
  }
  return [...set].sort((a, b) => a.localeCompare(b))
}

/**
 * Live rows minus the ones whose exact baseline line is (optimistically)
 * disabled, tagged enabled, followed by the disabled rows so they can be
 * re-enabled from the table.
 */
export function combineRuleRows(
  rows: readonly RuleRow[],
  matchLine: (row: RuleRow) => string | null,
  deleteLines: ReadonlySet<string>,
  disabledRows: readonly DisabledRuleRow[],
): DashboardRuleRow[] {
  const live = rows
    .filter((r) => {
      const line = matchLine(r)
      return !(line && deleteLines.has(line))
    })
    .map((r) => ({ ...r, disabled: false as const }))
  return [...live, ...disabledRows]
}

/** Type / policy filters are exact; the search is case-insensitive over type, match and policy. */
export function filterRuleRows<T extends RuleRow>(
  rows: readonly T[],
  { search, type, policy }: RuleFilters,
): T[] {
  const q = search.trim().toLowerCase()
  return rows.filter((r) => {
    if (type !== 'all' && r.type !== type) return false
    if (policy !== 'all' && r.proxy !== policy) return false
    if (!q) return true
    const hay = `${r.type} ${r.payload} ${r.proxy}`.toLowerCase()
    return hay.includes(q)
  })
}

/** The six most frequent rule types among `rows` (summary chips). */
export function ruleTypeTop(rows: readonly RuleRow[]): Array<[string, number]> {
  const counts = new Map<string, number>()
  for (const r of rows) {
    const key = String(r.type || '—')
    counts.set(key, (counts.get(key) ?? 0) + 1)
  }
  return [...counts.entries()].sort((a, b) => b[1] - a[1]).slice(0, 6)
}

export type RulesView = {
  rulesOverview: main.RulesOverview | null
  rulesBusy: boolean
  providers: RuleProviderRow[]
  providerBusyMap: Record<string, boolean>
  providerErrMap: Record<string, string>
  bulkBusy: boolean
  rulesRows: RuleRow[]
  filteredRulesRows: DashboardRuleRow[]
  rulesTypeTop: Array<[string, number]>
  ruleSearch: string
  ruleTypeFilter: string
  rulePolicyFilter: string
  ruleTypeOptions: string[]
  rulePolicyOptions: string[]
  onRefresh: () => void
  onRefreshAll: () => void
  onRefreshOne: (name: string) => void
  onSearchChange: (next: string) => void
  onTypeFilterChange: (next: string) => void
  onPolicyFilterChange: (next: string) => void
  ruleToggle: RuleToggleApi
}

/**
 * Everything the Rules screen shows: the /rules snapshot, rule-provider
 * updates, the dashboard enable/disable toggles and the filter state.
 *
 * Called from App, not from RulesPage: the page unmounts on every screen
 * switch, and the filters, provider busy/error flags and toggle state must
 * survive leaving and re-opening the screen.
 */
export function useRulesView(args: {
  enabled: boolean
  activeProfileId: string
  profiles: unknown[] | undefined
  onError: (msg: string) => void
}): RulesView {
  const { enabled, activeProfileId, profiles, onError } = args
  // Polls itself while the screen is open; `refresh` also backs the
  // rule-providers bulk update and the header "Refresh" button.
  const {
    overview: rulesOverview,
    busy: rulesBusy,
    refresh: refreshRules,
  } = useRulesOverview(enabled)
  const [ruleProviderBusyMap, setRuleProviderBusyMap] = useState<
    Record<string, boolean>
  >({})
  const [ruleProviderErrMap, setRuleProviderErrMap] = useState<
    Record<string, string>
  >({})
  const [ruleProvidersBulkBusy, setRuleProvidersBulkBusy] = useState(false)
  const [ruleSearch, setRuleSearch] = useState('')
  const [ruleTypeFilter, setRuleTypeFilter] = useState('all')
  const [rulePolicyFilter, setRulePolicyFilter] = useState('all')

  const rulesRows = useMemo(
    () => parseMihomoRulesJson(rulesOverview?.rulesBody),
    [rulesOverview?.rulesBody],
  )
  const ruleProvidersRows = useMemo(
    () => parseRuleProvidersJson(rulesOverview?.ruleProvidersBody),
    [rulesOverview?.ruleProvidersBody],
  )
  const ruleTypeFilterOptions = useMemo(
    () => ruleFilterOptions(rulesRows, 'type'),
    [rulesRows],
  )
  const rulePolicyFilterOptions = useMemo(
    () => ruleFilterOptions(rulesRows, 'proxy'),
    [rulesRows],
  )
  // Dashboard rule enable/disable. The hook loads the active profile's rule
  // baseline, tracks which lines are disabled (rules-template `delete` bucket),
  // and toggles them via SetProfileRulesTemplate (which auto-reconnects). See
  // useRuleToggles for the safety invariant (only unambiguous matches toggle).
  const ruleToggles = useRuleToggles({
    activeProfileId,
    profiles,
    enabled,
    onError,
    // The reconnect reloads the core; refetch /rules shortly after so the live
    // list reconciles with the optimistic view.
    onApplied: () => {
      window.setTimeout(() => void refreshRules(), 1500)
    },
  })
  const combinedRulesRows = useMemo(
    () =>
      combineRuleRows(
        rulesRows,
        ruleToggles.matchLine,
        ruleToggles.deleteLines,
        ruleToggles.disabledRows,
      ),
    [
      rulesRows,
      ruleToggles.matchLine,
      ruleToggles.deleteLines,
      ruleToggles.disabledRows,
    ],
  )
  const filteredRulesRows = useMemo(
    () =>
      filterRuleRows(combinedRulesRows, {
        search: ruleSearch,
        type: ruleTypeFilter,
        policy: rulePolicyFilter,
      }),
    [combinedRulesRows, ruleSearch, ruleTypeFilter, rulePolicyFilter],
  )
  const rulesTypeTop = useMemo(
    () => ruleTypeTop(filteredRulesRows),
    [filteredRulesRows],
  )

  const refreshRuleProviderOne = useCallback(
    async (name: string) => {
      const id = String(name ?? '').trim()
      if (!id) return
      setRuleProviderErrMap((prev) => {
        if (!prev[id]) return prev
        const next = { ...prev }
        delete next[id]
        return next
      })
      setRuleProviderBusyMap((prev) => ({ ...prev, [id]: true }))
      try {
        await UpdateRuleProvider(id)
        refreshRules()
      } catch (e: any) {
        setRuleProviderErrMap((prev) => ({ ...prev, [id]: String(e) }))
      } finally {
        setRuleProviderBusyMap((prev) => ({ ...prev, [id]: false }))
      }
    },
    [refreshRules],
  )

  const refreshRuleProvidersAll = useCallback(async () => {
    if (ruleProvidersRows.length === 0) return
    setRuleProvidersBulkBusy(true)
    setRuleProviderErrMap({})
    const busySeed: Record<string, boolean> = {}
    for (const p of ruleProvidersRows) busySeed[p.name] = true
    setRuleProviderBusyMap(busySeed)
    try {
      for (const p of ruleProvidersRows) {
        try {
          await UpdateRuleProvider(p.name)
          setRuleProviderBusyMap((prev) => ({ ...prev, [p.name]: false }))
        } catch (e: any) {
          setRuleProviderErrMap((prev) => ({ ...prev, [p.name]: String(e) }))
          setRuleProviderBusyMap((prev) => ({ ...prev, [p.name]: false }))
        }
      }
      refreshRules()
    } finally {
      setRuleProvidersBulkBusy(false)
    }
  }, [refreshRules, ruleProvidersRows])

  return {
    rulesOverview,
    rulesBusy,
    providers: ruleProvidersRows,
    providerBusyMap: ruleProviderBusyMap,
    providerErrMap: ruleProviderErrMap,
    bulkBusy: ruleProvidersBulkBusy,
    rulesRows,
    filteredRulesRows,
    rulesTypeTop,
    ruleSearch,
    ruleTypeFilter,
    rulePolicyFilter,
    ruleTypeOptions: ruleTypeFilterOptions,
    rulePolicyOptions: rulePolicyFilterOptions,
    onRefresh: () => refreshRules(),
    onRefreshAll: () => void refreshRuleProvidersAll(),
    onRefreshOne: (name) => void refreshRuleProviderOne(name),
    onSearchChange: setRuleSearch,
    onTypeFilterChange: setRuleTypeFilter,
    onPolicyFilterChange: setRulePolicyFilter,
    ruleToggle: {
      hasActiveProfile: ruleToggles.hasActiveProfile,
      baselineLoading: ruleToggles.baselineLoading,
      baselineError: ruleToggles.baselineError,
      busyLines: ruleToggles.busyLines,
      isToggleable: ruleToggles.isToggleable,
      matchLine: ruleToggles.matchLine,
      onDisable: ruleToggles.disableRow,
      onEnable: ruleToggles.enableLine,
    },
  }
}
