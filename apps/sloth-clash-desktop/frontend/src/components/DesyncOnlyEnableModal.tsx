import { useRef } from 'react'
import { useTranslation } from 'react-i18next'

import { useDialogFocus } from '../hooks/useDialogFocus'

/**
 * Asked after a Desync-only profile was activated while Desync is off: such a
 * profile has no VPN servers, so without Desync it routes everything directly.
 */
export function DesyncOnlyEnableModal({
  open,
  busy,
  onClose,
  onEnable,
}: {
  open: boolean
  busy: boolean
  onClose: () => void
  onEnable: () => void
}) {
  const { t } = useTranslation()
  const cardRef = useRef<HTMLDivElement>(null)
  useDialogFocus(cardRef, open, busy ? undefined : onClose)
  if (!open) return null
  return (
    <div
      className="modalOverlay"
      role="presentation"
      style={{ zIndex: 72 }}
      onClick={onClose}
    >
      <div
        ref={cardRef}
        className="modalCard"
        role="dialog"
        aria-modal="true"
        aria-labelledby="desyncOnlyEnableTitle"
        aria-describedby="desyncOnlyEnableBody"
        onClick={(e) => e.stopPropagation()}
      >
        <h3 id="desyncOnlyEnableTitle" className="modalTitle">
          {t('ui.profiles.desyncOnly.enableTitle')}
        </h3>
        <p id="desyncOnlyEnableBody" className="muted small">
          {t('ui.profiles.desyncOnly.enableBody')}
        </p>
        <div className="modalFooter">
          <div className="modalFooterRight" style={{ width: '100%' }}>
            <button
              type="button"
              className="btn ghost"
              disabled={busy}
              onClick={onClose}
            >
              {t('ui.profiles.desyncOnly.notNow')}
            </button>
            <button
              type="button"
              className="btn primary"
              disabled={busy}
              onClick={onEnable}
              data-autofocus
            >
              {t('ui.profiles.desyncOnly.enable')}
            </button>
          </div>
        </div>
      </div>
    </div>
  )
}
