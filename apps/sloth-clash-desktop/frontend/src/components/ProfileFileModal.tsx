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
            Edit configuration file
          </h3>
        </div>
        <p className="muted small mono tight">{path}</p>
        {loadError ? (
          <p className="muted small tight">
            Read: {loadError} — you can still paste YAML and save to create the
            file.
          </p>
        ) : null}
        <label className="field modalField">
          <span className="fieldLab">
            {target.name} <span className="optional">· loaded config.yaml</span>
          </span>
          <MonacoYamlEditor
            className="modalMonacoWrap"
            value={value}
            onChange={onChange}
            height="50vh"
          />
          {yamlError ? (
            <span className="muted small" style={{ color: '#ff6b6b' }}>
              YAML error: {yamlError}
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
            Copy path
          </button>
          <div className="modalFooterRight">
            <button type="button" className="btn ghost" onClick={onClose}>
              Cancel
            </button>
            <button
              type="button"
              className="btn primary"
              disabled={Boolean(yamlError)}
              onClick={() => onSave(target.id)}
            >
              Save
            </button>
          </div>
        </div>
      </div>
    </div>
  )
}
