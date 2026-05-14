import { useState } from 'react'
import { useTranslation } from 'react-i18next'

import type { main } from '../api/models'
import { RuleProvidersModal } from '../components/RuleProvidersModal'
import { friendlyErrorMessage } from '../utils/yaml'

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
}: {
  rulesOverview: main.RulesOverview | null
  connectionStatus: string
  rulesBusy: boolean
  providers: any[]
  providerBusyMap: Record<string, boolean>
  providerErrMap: Record<string, string>
  bulkBusy: boolean
  rulesRows: any[]
  filteredRulesRows: any[]
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
}) {
  const { t } = useTranslation()
  const [providersModalOpen, setProvidersModalOpen] = useState(false)
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
          <div className="rulesTableWrap rulesTableWrapFull">
            <table className="rulesTable">
              <thead>
                <tr>
                  <th>#</th>
                  <th>{t('ui.rules.type')}</th>
                  <th>{t('ui.rules.match')}</th>
                  <th>{t('ui.rules.policy')}</th>
                </tr>
              </thead>
              <tbody>
                {filteredRulesRows.map((r) => (
                  <tr key={`${r.idx}-${r.type}-${r.payload}`}>
                    <td>{r.idx}</td>
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
                ))}
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
