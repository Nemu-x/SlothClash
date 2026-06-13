import { useTranslation } from 'react-i18next'

import { MonacoYamlEditor } from '../MonacoYamlEditor'

export type ProfileModalTarget = { id: string; name: string }

export function ProfileMergeModal({
  target,
  value,
  yamlError,
  onChange,
  onResetScaffold,
  onClose,
  onSave,
}: {
  target: ProfileModalTarget | null
  value: string
  yamlError: string | null
  onChange: (next: string) => void
  onResetScaffold: () => void
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
        aria-labelledby="mergeTplTitle"
        onClick={(e) => e.stopPropagation()}
      >
        <div className="vergeModalHead">
          <h3 id="mergeTplTitle" className="modalTitle vergeModalTitle">
            {t('ui.profiles.mergeModal.title')}
          </h3>
        </div>
        <p className="muted small yamlModalBlurb">
          {t('ui.profiles.mergeModal.blurbLead')}{' '}
          <code className="code">prepend</code> /{' '}
          <code className="code">append</code> /{' '}
          <code className="code">delete</code>{' '}
          {t('ui.profiles.mergeModal.blurbTail')}{' '}
          <code className="code">config.yaml</code>.
        </p>
        <label className="field modalField">
          <span className="fieldLab">
            {target.name}{' '}
            <span className="optional">
              {t('ui.profiles.mergeModal.mergeYaml')}
            </span>
          </span>
          <MonacoYamlEditor
            className="modalMonacoWrap"
            value={value}
            onChange={onChange}
            height="48vh"
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
            onClick={onResetScaffold}
          >
            {t('ui.profiles.mergeModal.resetScaffold')}
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
