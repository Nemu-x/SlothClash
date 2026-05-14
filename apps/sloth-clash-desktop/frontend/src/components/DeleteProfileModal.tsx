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
          Delete profile
        </h3>
        <p className="muted small">
          Remove <strong>{target.name}</strong> from this device? This does not
          cancel a remote subscription.
        </p>
        <div className="modalFooter">
          <div className="modalFooterRight" style={{ width: '100%' }}>
            <button type="button" className="btn ghost" onClick={onClose}>
              Cancel
            </button>
            <button
              type="button"
              className="btn primary"
              onClick={() => onConfirm(target.id)}
            >
              Delete
            </button>
          </div>
        </div>
      </div>
    </div>
  )
}
