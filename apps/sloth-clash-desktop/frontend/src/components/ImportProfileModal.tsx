export function ImportProfileModal({
  open,
  title,
  blurb,
  url,
  name,
  busy,
  onUrlChange,
  onNameChange,
  onPasteFromClipboard,
  onClose,
  onSubmit,
}: {
  open: boolean
  title: string
  blurb: string
  url: string
  name: string
  busy: boolean
  onUrlChange: (next: string) => void
  onNameChange: (next: string) => void
  onPasteFromClipboard: () => void
  onClose: () => void
  onSubmit: () => void
}) {
  if (!open) return null
  return (
    <div className="modalOverlay" role="presentation" onClick={onClose}>
      <div
        className="modalCard"
        role="dialog"
        aria-modal="true"
        aria-labelledby="importTitle"
        onClick={(e) => e.stopPropagation()}
      >
        <h3 id="importTitle" className="modalTitle">
          {title}
        </h3>
        <p className="muted small">{blurb}</p>

        <label className="field modalField">
          <span className="fieldLab">Subscription URL</span>
          <input
            className="input"
            value={url}
            onChange={(e) => onUrlChange(e.target.value)}
            placeholder="https://…"
          />
        </label>
        <label className="field modalField">
          <span className="fieldLab">
            Display name <span className="optional">(optional)</span>
          </span>
          <input
            className="input"
            value={name}
            onChange={(e) => onNameChange(e.target.value)}
            placeholder="Empty = use Profile-Title from server"
          />
        </label>

        <div className="modalFooter">
          <button
            type="button"
            className="btn btnModalSecondary"
            onClick={onPasteFromClipboard}
          >
            Paste URL from clipboard
          </button>
          <div className="modalFooterRight">
            <button
              type="button"
              className="btn ghost"
              disabled={busy}
              onClick={onClose}
            >
              Cancel
            </button>
            <button
              type="button"
              className="btn primary"
              disabled={busy || !url.trim()}
              onClick={onSubmit}
            >
              Import
            </button>
          </div>
        </div>
      </div>
    </div>
  )
}
