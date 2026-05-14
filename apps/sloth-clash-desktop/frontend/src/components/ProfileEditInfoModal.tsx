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
          Edit profile info
        </h3>
        <p className="muted small">
          Display name and subscription link. Leave the URL field empty to keep
          the current link unchanged.
        </p>
        <label className="field modalField">
          <span className="fieldLab">Name</span>
          <input
            className="input"
            value={name}
            onChange={(e) => onNameChange(e.target.value)}
            autoFocus
          />
        </label>
        <label className="field modalField">
          <span className="fieldLab">Subscription URL</span>
          <input
            className="input"
            value={url}
            onChange={(e) => onUrlChange(e.target.value)}
            placeholder="Leave empty to keep current"
          />
        </label>
        <div className="fieldGrid">
          <label className="field modalField">
            <span className="fieldLab">Auto-update</span>
            <button
              type="button"
              className={`trafficKnob ${autoEnabled ? 'on' : ''}`}
              onClick={onAutoEnabledToggle}
            >
              {autoEnabled ? 'On' : 'Off'}
            </button>
          </label>
          <label className="field modalField">
            <span className="fieldLab">Interval (minutes)</span>
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
            Copy URL
          </button>
          <button
            type="button"
            className="btn btnModalSecondary"
            disabled={!name.trim()}
            onClick={() => onCopyName(name.trim())}
          >
            Copy name
          </button>
        </div>
        <div className="modalFooter">
          <div className="modalFooterRight" style={{ width: '100%' }}>
            <button type="button" className="btn ghost" onClick={onClose}>
              Cancel
            </button>
            <button
              type="button"
              className="btn primary"
              disabled={!name.trim()}
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
