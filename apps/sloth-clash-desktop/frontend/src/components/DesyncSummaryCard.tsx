import { useId } from 'react'
import { useTranslation } from 'react-i18next'

import { useDesyncPresets } from '../hooks/queries/useDesyncPresets'
import { useDesyncStatus } from '../hooks/queries/useDesyncStatus'
import { desyncSelectionNames } from '../utils/desyncStatus'

/**
 * Settings: a compact summary of Desync (on / off, what it covers) with an
 * Open button; the controls themselves live in the Desync modal.
 */
export function DesyncSummaryCard({
  connected,
  mode,
  traffic,
  onOpen,
}: {
  connected: boolean
  mode: string
  traffic: string
  onOpen: () => void
}) {
  const { t } = useTranslation()
  const titleId = useId()
  const { snapshot } = useDesyncStatus({ connected, mode, traffic })
  const settings = snapshot?.status.settings
  const catalog = useDesyncPresets()
  const names = settings ? desyncSelectionNames(t, settings, catalog) : []
  return (
    <div
      className="homeCard settingsCardCompact desyncSummaryCard"
      role="group"
      aria-labelledby={titleId}
    >
      <h3 id={titleId} className="homeCardTitle">
        <span className="settingsCardIcon" aria-hidden>
          🔓
        </span>
        {t('desync.title')}
      </h3>
      <p className="muted settingsMicroHint">{t('desync.lead')}</p>
      <div className="settingsTunSummary">
        <span className="settingsTunSummaryItem">
          <span className="settingsTunSummaryLabel">
            {t('desync.summary.status')}:
          </span>
          <span
            className="settingsTunSummaryValue desyncSummaryState"
            data-on={settings ? settings.enabled === true : undefined}
          >
            {!settings
              ? '…'
              : settings.enabled === true
                ? t('common.on')
                : t('common.off')}
          </span>
        </span>
        <span className="settingsTunSummaryItem">
          <span className="settingsTunSummaryLabel">
            {t('desync.summary.covers')}:
          </span>
          <span className="settingsTunSummaryValue">
            {!settings
              ? '…'
              : names.length > 0
                ? names.join(', ')
                : t('desync.summary.nothing')}
          </span>
        </span>
      </div>
      <div className="settingsInlineActions">
        <button type="button" className="btn" onClick={onOpen}>
          {t('desync.summary.open')}
        </button>
      </div>
    </div>
  )
}
