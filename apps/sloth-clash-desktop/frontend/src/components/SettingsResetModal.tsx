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
          Reset app settings
        </h3>
        <p className="muted small">
          {mode === 'with_profiles'
            ? 'Reset settings and delete all profiles from this device?'
            : 'Reset UI settings and local defaults, but keep profiles?'}
        </p>
        <div className="modalFooter">
          <div className="modalFooterRight" style={{ width: '100%' }}>
            <button type="button" className="btn ghost" onClick={onClose}>
              Cancel
            </button>
            <button
              type="button"
              className="btn primary"
              onClick={() => onConfirm(mode === 'with_profiles')}
            >
              Reset
            </button>
          </div>
        </div>
      </div>
    </div>
  )
}
