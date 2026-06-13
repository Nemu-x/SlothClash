import { useTranslation } from 'react-i18next'

export type SettingsResetMode = 'keep_profiles' | 'with_profiles'

export function SettingsResetModal({
  mode,
  onClose,
  onConfirm,
}: {
  mode: SettingsResetMode | null
  onClose: () => void
  onConfirm: (withProfiles: boolean) => void
}) {
  const { t } = useTranslation()
  if (!mode) return null
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
        aria-labelledby="resetSettingsTitle"
        onClick={(e) => e.stopPropagation()}
      >
        <h3 id="resetSettingsTitle" className="modalTitle">
          {t('settings.resetModal.title')}
        </h3>
        <p className="muted small">
          {mode === 'with_profiles'
            ? t('settings.resetModal.withProfilesBody')
            : t('settings.resetModal.keepProfilesBody')}
        </p>
        <div className="modalFooter">
          <div className="modalFooterRight" style={{ width: '100%' }}>
            <button type="button" className="btn ghost" onClick={onClose}>
              {t('common.cancel')}
            </button>
            <button
              type="button"
              className="btn primary"
              onClick={() => onConfirm(mode === 'with_profiles')}
            >
              {t('settings.resetModal.reset')}
            </button>
          </div>
        </div>
      </div>
    </div>
  )
}
