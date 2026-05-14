import { useTranslation } from 'react-i18next'

import type { ConnectionsOverview } from '../types/app'
import { formatBytesSmart } from '../utils/format'

type ConnectionRow = NonNullable<ConnectionsOverview['connections']>[number]

export function ConnectionsPage({
  overview,
  filtered,
  busy,
  search,
  onSearchChange,
  onRefresh,
  onCloseAll,
}: {
  overview: ConnectionsOverview | null
  filtered: ConnectionRow[]
  busy: boolean
  search: string
  onSearchChange: (next: string) => void
  onRefresh: () => void
  onCloseAll: () => void
}) {
  const { t } = useTranslation()
  return (
    <div className="panel rulesPanel">
      <div className="rulesPanelHead">
        <h2 className="rulesPanelTitle">{t('ui.connections.title')}</h2>
        <div className="row">
          <button
            type="button"
            className="btn ghost"
            disabled={busy}
            onClick={onRefresh}
          >
            {busy
              ? t('ui.connections.refreshing')
              : t('ui.connections.refresh')}
          </button>
          <button type="button" className="btn" onClick={onCloseAll}>
            {t('ui.connections.closeAll')}
          </button>
        </div>
      </div>
      {overview?.lastError ? (
        <p className="error tight">{overview.lastError}</p>
      ) : null}
      <div className="rulesSummaryRow">
        <span className="rulesSummaryChip">
          {t('ui.connections.upload')}:{' '}
          {formatBytesSmart(Number(overview?.uploadTotal ?? 0))}
        </span>
        <span className="rulesSummaryChip">
          {t('ui.connections.download')}:{' '}
          {formatBytesSmart(Number(overview?.downloadTotal ?? 0))}
        </span>
        <span className="rulesSummaryChip">
          {t('ui.connections.total')}: {filtered.length}/
          {(overview?.connections ?? []).length}
        </span>
      </div>
      <input
        className="input rulesFilterSearch"
        value={search}
        onChange={(e) => onSearchChange(e.target.value)}
        placeholder={t('ui.connections.searchPlaceholder')}
      />
      <div className="rulesTableWrap rulesTableWrapFull">
        <table className="rulesTable">
          <thead>
            <tr>
              <th>ID</th>
              <th>{t('ui.connections.host')}</th>
              <th>{t('ui.connections.process')}</th>
              <th>{t('ui.connections.rule')}</th>
              <th>{t('ui.connections.traffic')}</th>
            </tr>
          </thead>
          <tbody>
            {filtered.map((c) => {
              const meta = c.metadata ?? {}
              return (
                <tr key={c.id}>
                  <td className="monoTight">{String(c.id).slice(0, 8)}</td>
                  <td className="rulesPayload">
                    {meta.host || meta.destinationIP || '—'}
                    {meta.destinationPort ? `:${meta.destinationPort}` : ''}
                  </td>
                  <td>{meta.process || '—'}</td>
                  <td className="rulesPayload">
                    {c.rulePayload || c.rule || '—'}
                  </td>
                  <td>
                    ↑ {formatBytesSmart(Number(c.upload ?? 0))} / ↓{' '}
                    {formatBytesSmart(Number(c.download ?? 0))}
                  </td>
                </tr>
              )
            })}
          </tbody>
        </table>
      </div>
    </div>
  )
}
