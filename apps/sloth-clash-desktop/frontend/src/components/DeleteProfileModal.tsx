import { useTranslation } from 'react-i18next'

export type DeleteProfileTarget = { id: string; name: string }

export function DeleteProfileModal({
  target,
  onClose,
  onConfirm,
}: {
  target: DeleteProfileTarget | null
  onClose: () => void
  onConfirm: (id: string) => void
}) {
  const { t } = useTranslation()
  if (!target) return null
  return (
    <div
      className="modalOverlay"
      role="presentation"
      style={{ zIndex: 72 }}
      onClick={onClose}
    >
      <div
        className="modalCard"
        role="dialog"
        aria-modal="true"
        aria-labelledby="deleteProfileTitle"
        onClick={(e) => e.stopPropagation()}
      >
        <h3 id="deleteProfileTitle" className="modalTitle">
          {t('ui.profiles.deleteModal.title')}
        </h3>
        <p className="muted small">
          {t('ui.profiles.deleteModal.bodyPrefix')}{' '}
          <strong>{target.name}</strong>{' '}
          {t('ui.profiles.deleteModal.bodySuffix')}
        </p>
        <div className="modalFooter">
          <div className="modalFooterRight" style={{ width: '100%' }}>
            <button type="button" className="btn ghost" onClick={onClose}>
              {t('common.cancel')}
            </button>
            <button
              type="button"
              className="btn primary"
              onClick={() => onConfirm(target.id)}
            >
              {t('common.delete')}
            </button>
          </div>
        </div>
      </div>
    </div>
  )
}
