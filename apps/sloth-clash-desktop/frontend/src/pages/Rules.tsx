import { useState } from 'react'
import { useTranslation } from 'react-i18next'

import type { main } from '../api/models'
import { RuleProvidersModal } from '../components/RuleProvidersModal'
import type { RuleRow } from '../rulesTable'
import { friendlyErrorMessage } from '../utils/yaml'

// A rule row as rendered on the dashboard: a live rule (disabled=false) or a
// user-disabled baseline rule (disabled=true, carrying its exact `line`).
type DashboardRuleRow = RuleRow & { disabled?: boolean; line?: string }

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

export function RulesPage({
  rulesOverview,
  connectionStatus,
  rulesBusy,
  providers,
  providerBusyMap,
  providerErrMap,
  bulkBusy,
  rulesRows,
  filteredRulesRows,
  rulesTypeTop,
  ruleSearch,
  ruleTypeFilter,
  rulePolicyFilter,
  ruleTypeOptions,
  rulePolicyOptions,
  error,
  onRefresh,
  onRefreshAll,
  onRefreshOne,
  onSearchChange,
  onTypeFilterChange,
  onPolicyFilterChange,
  ruleToggle,
}: {
  rulesOverview: main.RulesOverview | null
  connectionStatus: string
  rulesBusy: boolean
  providers: any[]
  providerBusyMap: Record<string, boolean>
  providerErrMap: Record<string, string>
  bulkBusy: boolean
  rulesRows: any[]
  filteredRulesRows: DashboardRuleRow[]
  rulesTypeTop: Array<[string, number]>
  ruleSearch: string
  ruleTypeFilter: string
  rulePolicyFilter: string
  ruleTypeOptions: string[]
  rulePolicyOptions: string[]
  error: string | null
  onRefresh: () => void
  onRefreshAll: () => void
  onRefreshOne: (name: string) => void
  onSearchChange: (next: string) => void
  onTypeFilterChange: (next: string) => void
  onPolicyFilterChange: (next: string) => void
  ruleToggle: RuleToggleApi
}) {
  const { t } = useTranslation()
  const [providersModalOpen, setProvidersModalOpen] = useState(false)
  // Toggling is offered only with an active profile whose baseline loaded.
  const togglesReady = ruleToggle.hasActiveProfile && !ruleToggle.baselineError
  // Why a given live row cannot be toggled (custom / unmatched / ruleset-only).
  const rowHint = (row: DashboardRuleRow): string => {
    if (!ruleToggle.hasActiveProfile) return t('ui.rules.toggleNoProfile')
    if (String(row.type).toLowerCase().replace(/[-_]/g, '') === 'ruleset')
      return t('ui.rules.toggleRulesetHint')
    return t('ui.rules.toggleReadonlyHint')
  }
  const failedProviders = providers.filter((p) =>
    Boolean(providerErrMap[p.name]),
  ).length
  return (
    <div className="panel rulesPanel">
      <div className="rulesPanelHead">
        <h2 className="rulesPanelTitle">{t('ui.rules.title')}</h2>
        <div className="row">
          <button
            type="button"
            className="btn ghost"
            disabled={rulesBusy}
            onClick={onRefresh}
          >
            {rulesBusy ? t('ui.rules.refreshing') : t('ui.rules.refresh')}
          </button>
          <button
            type="button"
            className={`btn${failedProviders > 0 ? ' rulesProvidersTriggerWarn' : ''}`}
            disabled={providers.length === 0}
            onClick={() => setProvidersModalOpen(true)}
            title={t('ui.rules.providersManage')}
          >
            {t('ui.rules.providersHeading')} ({providers.length})
            {failedProviders > 0 ? (
              <span
                className="rulesProvidersFailBadge"
                aria-label={t('ui.rules.providerUpdateFailed')}
              >
                {failedProviders}
              </span>
            ) : null}
          </button>
        </div>
      </div>
      {connectionStatus === 'connected' && rulesOverview?.lastError ? (
        <p className="error tight">{rulesOverview.lastError}</p>
      ) : null}
      {rulesRows.length > 0 ? (
        <>
          <div className="rulesFilterRow">
            <input
              className="input rulesFilterSearch"
              value={ruleSearch}
              onChange={(e) => onSearchChange(e.target.value)}
              placeholder={t('ui.rules.searchPlaceholder')}
            />
            <select
              className="selectModern rulesFilterSelect"
              value={ruleTypeFilter}
              onChange={(e) => onTypeFilterChange(e.target.value)}
            >
              <option value="all">{t('ui.rules.allTypes')}</option>
              {ruleTypeOptions.map((opt) => (
                <option key={opt} value={opt}>
                  {opt}
                </option>
              ))}
            </select>
            <select
              className="selectModern rulesFilterSelect"
              value={rulePolicyFilter}
              onChange={(e) => onPolicyFilterChange(e.target.value)}
            >
              <option value="all">{t('ui.rules.allPolicies')}</option>
              {rulePolicyOptions.map((opt) => (
                <option key={opt} value={opt}>
                  {opt}
                </option>
              ))}
            </select>
          </div>
          <div className="rulesSummaryRow">
            <span className="rulesSummaryChip">
              {t('ui.rules.total')}: {filteredRulesRows.length}/
              {rulesRows.length}
            </span>
            {rulesTypeTop.map(([typeKey, count]) => (
              <span key={typeKey} className="rulesSummaryChip">
                {typeKey}: {count}
              </span>
            ))}
          </div>
          {!ruleToggle.hasActiveProfile ? (
            <p className="muted small tight">{t('ui.rules.toggleNoProfile')}</p>
          ) : ruleToggle.baselineError ? (
            <p className="muted small tight rulesYamlErr">
              {t('ui.rules.toggleBaselineError', {
                error: ruleToggle.baselineError,
              })}
            </p>
          ) : null}
          <div className="rulesTableWrap rulesTableWrapFull">
            <table className="rulesTable">
              <thead>
                <tr>
                  <th
                    className="rulesToggleCol"
                    title={t('ui.rules.toggleCol')}
                  >
                    {t('ui.rules.toggleCol')}
                  </th>
                  <th>#</th>
                  <th>{t('ui.rules.type')}</th>
                  <th>{t('ui.rules.match')}</th>
                  <th>{t('ui.rules.policy')}</th>
                </tr>
              </thead>
              <tbody>
                {filteredRulesRows.map((r, i) => {
                  const isDisabled = r.disabled === true
                  const line = isDisabled ? r.line : ruleToggle.matchLine(r)
                  const canToggle = togglesReady && Boolean(line)
                  const busy = line ? ruleToggle.busyLines.has(line) : false
                  const key = isDisabled
                    ? `off-${r.line}`
                    : `${r.idx}-${r.type}-${r.payload}-${i}`
                  return (
                    <tr
                      key={key}
                      className={isDisabled ? 'ruleRowDisabled' : undefined}
                    >
                      <td className="rulesToggleCell">
                        <input
                          type="checkbox"
                          className="ruleToggleBox"
                          checked={!isDisabled}
                          disabled={!canToggle || busy}
                          title={
                            canToggle
                              ? isDisabled
                                ? t('ui.rules.toggleEnable')
                                : t('ui.rules.toggleDisable')
                              : rowHint(r)
                          }
                          aria-label={
                            isDisabled
                              ? t('ui.rules.toggleEnable')
                              : t('ui.rules.toggleDisable')
                          }
                          onChange={() => {
                            if (!canToggle || busy) return
                            if (isDisabled) {
                              if (r.line) ruleToggle.onEnable(r.line)
                            } else {
                              ruleToggle.onDisable(r)
                            }
                          }}
                        />
                      </td>
                      <td>{isDisabled ? '—' : r.idx}</td>
                      <td>{r.type}</td>
                      <td className="rulesPayload">{r.payload || '—'}</td>
                      <td
                        className={
                          r.proxy && r.proxy !== 'DIRECT'
                            ? 'rulesPolicy rulesPolicyProxy'
                            : 'rulesPolicy'
                        }
                      >
                        {r.proxy}
                      </td>
                    </tr>
                  )
                })}
              </tbody>
            </table>
          </div>
        </>
      ) : (
        <p className="muted small tight">{t('ui.rules.noParsed')}</p>
      )}
      {error ? <p className="error">{friendlyErrorMessage(error)}</p> : null}

      <RuleProvidersModal
        open={providersModalOpen}
        providers={providers}
        busyMap={providerBusyMap}
        errMap={providerErrMap}
        bulkBusy={bulkBusy}
        onRefreshOne={onRefreshOne}
        onRefreshAll={onRefreshAll}
        onClose={() => setProvidersModalOpen(false)}
      />
    </div>
  )
}
