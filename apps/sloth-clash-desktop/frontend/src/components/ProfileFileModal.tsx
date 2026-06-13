import { useTranslation } from 'react-i18next'

import { MonacoYamlEditor } from '../MonacoYamlEditor'

import type { ProfileModalTarget } from './ProfileMergeModal'

export function ProfileFileModal({
  target,
  path,
  loadError,
  value,
  yamlError,
  onChange,
  onCopyPath,
  onClose,
  onSave,
}: {
  target: ProfileModalTarget | null
  path: string
  loadError: string | null
  value: string
  yamlError: string | null
  onChange: (next: string) => void
  onCopyPath: (path: string) => void
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
        className="modalCard modalCardWide yamlModalCard modalCardFullscreen vergeModal"
        role="dialog"
        aria-modal="true"
        aria-labelledby="editFileTitle"
        onClick={(e) => e.stopPropagation()}
      >
        <div className="vergeModalHead">
          <h3 id="editFileTitle" className="modalTitle vergeModalTitle">
            {t('ui.profiles.fileModal.title')}
          </h3>
        </div>
        <p className="muted small mono tight">{path}</p>
        {loadError ? (
          <p className="muted small tight">
            {t('ui.profiles.fileModal.readErrorLead', { error: loadError })}
          </p>
        ) : null}
        <label className="field modalField">
          <span className="fieldLab">
            {target.name}{' '}
            <span className="optional">
              {t('ui.profiles.fileModal.loadedConfig')}
            </span>
          </span>
          <MonacoYamlEditor
            className="modalMonacoWrap"
            value={value}
            onChange={onChange}
            height="50vh"
          />
          {yamlError ? (
            <span className="muted small" style={{ color: '#ff6b6b' }}>
              {t('common.yamlError', { error: yamlError })}
            </span>
          ) : null}
        </label>
        <div className="modalFooter">
          <button
            type="button"
            className="btn btnModalSecondary"
            disabled={!path}
            onClick={() => onCopyPath(path)}
          >
            {t('ui.profiles.fileModal.copyPath')}
          </button>
          <div className="modalFooterRight">
            <button type="button" className="btn ghost" onClick={onClose}>
              {t('common.cancel')}
            </button>
            <button
              type="button"
              className="btn primary"
              disabled={Boolean(yamlError)}
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
