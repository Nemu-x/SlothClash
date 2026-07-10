import { useTranslation } from 'react-i18next'

export type InstallConfigRequest = {
  name: string
  url: string
  host: string
}

// A web page can trigger the slothclash:// scheme, so an import that arrives via
// a deep link is always confirmed by the user before anything is fetched.
export function InstallConfigConfirmModal({
  request,
  busy,
  onCancel,
  onConfirm,
}: {
  request: InstallConfigRequest | null
  busy: boolean
  onCancel: () => void
  onConfirm: (request: InstallConfigRequest) => void
}) {
  const { t } = useTranslation()
  if (!request) return null
  return (
    <div
      className="modalOverlay"
      role="presentation"
      style={{ zIndex: 74 }}
      onClick={busy ? undefined : onCancel}
    >
      <div
        className="modalCard"
        role="dialog"
        aria-modal="true"
        aria-label={t('installConfig.title')}
        onClick={(e) => e.stopPropagation()}
      >
        <h3 className="modalTitle">{t('installConfig.title')}</h3>
        <p className="muted">{t('installConfig.lead')}</p>

        <div className="installConfigDetails">
          <div className="installConfigRow">
            <span className="fieldLab">{t('installConfig.source')}</span>
            <strong className="installConfigHost">{request.host}</strong>
          </div>
          {request.name ? (
            <div className="installConfigRow">
              <span className="fieldLab">{t('installConfig.name')}</span>
              <span>{request.name}</span>
            </div>
          ) : null}
          <div className="installConfigRow">
            <span className="fieldLab">{t('installConfig.url')}</span>
            <code className="installConfigUrl">{request.url}</code>
          </div>
        </div>

        <p className="installConfigWarn">{t('installConfig.warning')}</p>

        <div className="modalActions">
          <button
            type="button"
            className="btn ghost"
            disabled={busy}
            onClick={onCancel}
          >
            {t('common.cancel')}
          </button>
          <button
            type="button"
            className="btn primary"
            disabled={busy}
            onClick={() => onConfirm(request)}
          >
            {busy ? t('installConfig.adding') : t('installConfig.confirm')}
          </button>
        </div>
      </div>
    </div>
  )
}
