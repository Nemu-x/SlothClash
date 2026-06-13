import { useTranslation } from 'react-i18next'

export type ProfileEditInfoTarget = { id: string; name: string; url: string }

export function ProfileEditInfoModal({
  target,
  name,
  url,
  autoEnabled,
  autoInterval,
  onNameChange,
  onUrlChange,
  onAutoEnabledToggle,
  onAutoIntervalChange,
  onCopyUrl,
  onCopyName,
  onClose,
  onSave,
}: {
  target: ProfileEditInfoTarget | null
  name: string
  url: string
  autoEnabled: boolean
  autoInterval: string
  onNameChange: (next: string) => void
  onUrlChange: (next: string) => void
  onAutoEnabledToggle: () => void
  onAutoIntervalChange: (next: string) => void
  onCopyUrl: (url: string) => void
  onCopyName: (name: string) => void
  onClose: () => void
  onSave: (id: string) => void
}) {
  const { t } = useTranslation()
  if (!target) return null
  return (
    <div
      className="modalOverlay"
      role="presentation"
      style={{ zIndex: 70 }}
      onClick={onClose}
    >
      <div
        className="modalCard"
        role="dialog"
        aria-modal="true"
        aria-labelledby="editInfoTitle"
        onClick={(e) => e.stopPropagation()}
      >
        <h3 id="editInfoTitle" className="modalTitle">
          {t('ui.profiles.editInfo.title')}
        </h3>
        <p className="muted small">{t('ui.profiles.editInfo.blurb')}</p>
        <label className="field modalField">
          <span className="fieldLab">{t('ui.profiles.editInfo.name')}</span>
          <input
            className="input"
            value={name}
            onChange={(e) => onNameChange(e.target.value)}
            autoFocus
          />
        </label>
        <label className="field modalField">
          <span className="fieldLab">
            {t('ui.profiles.editInfo.subscriptionUrl')}
          </span>
          <input
            className="input"
            value={url}
            onChange={(e) => onUrlChange(e.target.value)}
            placeholder={t('ui.profiles.editInfo.urlPlaceholder')}
          />
        </label>
        <div className="fieldGrid">
          <label className="field modalField">
            <span className="fieldLab">
              {t('ui.profiles.editInfo.autoUpdate')}
            </span>
            <button
              type="button"
              className={`trafficKnob ${autoEnabled ? 'on' : ''}`}
              onClick={onAutoEnabledToggle}
            >
              {autoEnabled ? t('common.on') : t('common.off')}
            </button>
          </label>
          <label className="field modalField">
            <span className="fieldLab">
              {t('ui.profiles.editInfo.intervalMinutes')}
            </span>
            <input
              className="input"
              value={autoInterval}
              onChange={(e) => onAutoIntervalChange(e.target.value)}
              placeholder="360"
            />
          </label>
        </div>
        <div className="modalActions">
          <button
            type="button"
            className="btn btnModalSecondary"
            disabled={!url.trim()}
            onClick={() => onCopyUrl(url.trim())}
          >
            {t('ui.profiles.editInfo.copyUrl')}
          </button>
          <button
            type="button"
            className="btn btnModalSecondary"
            disabled={!name.trim()}
            onClick={() => onCopyName(name.trim())}
          >
            {t('ui.profiles.editInfo.copyName')}
          </button>
        </div>
        <div className="modalFooter">
          <div className="modalFooterRight" style={{ width: '100%' }}>
            <button type="button" className="btn ghost" onClick={onClose}>
              {t('common.cancel')}
            </button>
            <button
              type="button"
              className="btn primary"
              disabled={!name.trim()}
              onClick={() => onSave(target.id)}
            >
              {t('common.save')}
            </button>
          </div>
        </div>
      </div>
    </div>
  )
}
