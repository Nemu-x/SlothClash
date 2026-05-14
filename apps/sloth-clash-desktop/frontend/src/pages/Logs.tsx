import { useTranslation } from 'react-i18next'

import type { main } from '../api/models'

export function LogsPage({
  serviceLog,
  onRefresh,
}: {
  serviceLog: main.ServiceLogPeek | null
  onRefresh: () => void
}) {
  const { t } = useTranslation()
  return (
    <div className="panel rulesPanel">
      <div className="rulesPanelHead">
        <h2 className="rulesPanelTitle">{t('ui.logs.title')}</h2>
        <button type="button" className="btn ghost" onClick={onRefresh}>
          {t('ui.logs.refresh')}
        </button>
      </div>
      {serviceLog?.path ? (
        <p className="muted small tight">
          <strong className="monoTight">{serviceLog.path}</strong>
        </p>
      ) : null}
      {serviceLog?.lastError ? (
        <p className="error tight">{serviceLog.lastError}</p>
      ) : null}
      {serviceLog?.text ? (
        <pre className="mono tightPre logPre">{serviceLog.text}</pre>
      ) : (
        <p className="muted small">{t('ui.logs.empty')}</p>
      )}
    </div>
  )
}
