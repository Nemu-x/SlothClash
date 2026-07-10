import { useTranslation } from 'react-i18next'

import type { ConnectionsOverview } from '../types/app'
import { formatBytesSmart } from '../utils/format'

type ConnectionRow = NonNullable<ConnectionsOverview['connections']>[number]

// mihomo orders `chains` from the exit node (index 0) outward to the entry
// group. The exit node — what the user actually connected THROUGH — is chains[0].
function exitNode(chains: string[] | undefined): string {
  return chains && chains.length > 0 ? chains[0] : ''
}

// Full path read entry-group → … → exit node, for the hover tooltip.
function chainPath(chains: string[] | undefined): string {
  if (!chains || chains.length === 0) return ''
  return [...chains].reverse().join(' → ')
}

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
              <th>{t('ui.connections.node')}</th>
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
                    {exitNode(c.chains) ? (
                      <span
                        className="connNodeChip"
                        title={chainPath(c.chains)}
                      >
                        {exitNode(c.chains)}
                      </span>
                    ) : (
                      <span className="connNodeDirect">
                        {t('ui.connections.direct')}
                      </span>
                    )}
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
