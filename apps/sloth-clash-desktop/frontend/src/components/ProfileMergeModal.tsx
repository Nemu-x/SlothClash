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
            Profile merge template
          </h3>
        </div>
        <p className="muted small yamlModalBlurb">
          Top-level keys merge into the fetched profile;{' '}
          <code className="code">prepend</code> /{' '}
          <code className="code">append</code> /{' '}
          <code className="code">delete</code> for rules, proxy-groups, and
          provider maps. Applied whenever Sloth writes{' '}
          <code className="code">config.yaml</code>.
        </p>
        <label className="field modalField">
          <span className="fieldLab">
            {target.name} <span className="optional">· merge YAML</span>
          </span>
          <MonacoYamlEditor
            className="modalMonacoWrap"
            value={value}
            onChange={onChange}
            height="48vh"
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
            onClick={onResetScaffold}
          >
            Reset scaffold
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
