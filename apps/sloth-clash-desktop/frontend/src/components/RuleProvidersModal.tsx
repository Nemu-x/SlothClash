import { useTranslation } from 'react-i18next'

import { formatProviderUpdatedAt } from '../utils/format'

/**
 * Rule-provider management modal.
 *
 * Previously the Rules screen rendered every provider as an inline card. On
 * profiles with 10+ providers (geo-grouped subscriptions) this was an
 * unscrollable wall of cards above the rules table. The modal extracts the
 * full list into its own scroll area; the Rules screen now just shows a
 * trigger button with the current count.
 */
export function RuleProvidersModal({
  open,
  providers,
  busyMap,
  errMap,
  bulkBusy,
  onRefreshOne,
  onRefreshAll,
  onClose,
}: {
  open: boolean
  providers: any[]
  busyMap: Record<string, boolean>
  errMap: Record<string, string>
  bulkBusy: boolean
  onRefreshOne: (name: string) => void
  onRefreshAll: () => void
  onClose: () => void
}) {
  const { t } = useTranslation()
  if (!open) return null
  return (
    <div
      className="modalOverlay"
      role="presentation"
      style={{ zIndex: 72 }}
      onClick={onClose}
    >
      <div
        className="modalCard modalCardWide ruleProvidersModalCard"
        role="dialog"
        aria-modal="true"
        aria-labelledby="ruleProvidersTitle"
        onClick={(e) => e.stopPropagation()}
      >
        <div className="rulesPanelHead">
          <h3 id="ruleProvidersTitle" className="modalTitle">
            {t('ui.rules.providersHeading')} ({providers.length})
          </h3>
          <div className="row">
            <button
              type="button"
              className="btn"
              disabled={bulkBusy || providers.length === 0}
              onClick={onRefreshAll}
              title={t('ui.rules.providersUpdateAll')}
            >
              {bulkBusy
                ? t('ui.rules.providersUpdating')
                : t('ui.rules.providersUpdateAll')}
            </button>
            <button
              type="button"
              className="btn ghost"
              onClick={onClose}
              aria-label={t('ui.rules.providersClose')}
            >
              {t('ui.rules.providersClose')}
            </button>
          </div>
        </div>

        {providers.length === 0 ? (
          <p className="muted small tight">{t('ui.rules.providersEmpty')}</p>
        ) : (
          <div className="ruleProvidersModalScroll">
            <div className="rulesProvidersGrid">
              {providers.map((p) => {
                const busy = Boolean(busyMap[p.name])
                const err = errMap[p.name]
                return (
                  <div key={p.name} className="ruleProviderCard">
                    <div className="ruleProviderCardInner">
                      <div className="ruleProviderCardTop">
                        <div className="ruleProviderTitle" title={p.name}>
                          {p.name}
                        </div>
                        <button
                          type="button"
                          className={`ruleProviderRefreshIcon${busy ? ' isBusy' : ''}`}
                          aria-label={t('ui.rules.providersUpdate')}
                          title={t('ui.rules.providersUpdate')}
                          disabled={busy || bulkBusy}
                          onClick={() => onRefreshOne(p.name)}
                        >
                          <svg
                            className="profileRefreshSvg"
                            viewBox="0 0 24 24"
                            width="18"
                            height="18"
                            aria-hidden
                          >
                            <path
                              fill="none"
                              stroke="currentColor"
                              strokeWidth="2"
                              strokeLinecap="round"
                              strokeLinejoin="round"
                              d="M21 12a9 9 0 1 1-3-6.7"
                            />
                            <path
                              fill="none"
                              stroke="currentColor"
                              strokeWidth="2"
                              strokeLinecap="round"
                              strokeLinejoin="round"
                              d="M21 3v6h-6"
                            />
                          </svg>
                        </button>
                      </div>
                      <div className="ruleProviderMetaLine">
                        {p.behavior} · {p.vehicleType} · {p.ruleCount}
                      </div>
                      <div className="ruleProviderFoot">
                        <span className="ruleProviderUpdated">
                          {formatProviderUpdatedAt(p.updatedAt)}
                        </span>
                      </div>
                      {err ? (
                        <p className="error small tight ruleProviderErr">
                          {t('ui.rules.providerUpdateFailed')}: {err}
                        </p>
                      ) : null}
                    </div>
                  </div>
                )
              })}
            </div>
          </div>
        )}
      </div>
    </div>
  )
}
