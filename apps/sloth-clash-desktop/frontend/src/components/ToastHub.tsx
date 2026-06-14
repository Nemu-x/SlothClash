import { useTranslation } from 'react-i18next'

import type { Toast } from '../hooks/useToasts'

type Props = {
  toasts: Toast[]
  onDismiss: (id: number) => void
}

/**
 * Stacked toast renderer. The stack lives in a fixed pane in the bottom-right
 * of the window so it does not occlude the primary content. The first item
 * shown is the most recent.
 */
export function ToastHub({ toasts, onDismiss }: Props) {
  const { t } = useTranslation()
  if (toasts.length === 0) return null
  return (
    <div className="toastHub" role="status" aria-live="polite">
      {toasts.map((toast) => (
        <div key={toast.id} className={`toast toast-${toast.kind}`}>
          <span className="toastMsg">{toast.message}</span>
          {toast.actionLabel ? (
            <button
              type="button"
              className="toastAction"
              onClick={() => {
                toast.onAction?.()
                onDismiss(toast.id)
              }}
            >
              {toast.actionLabel}
            </button>
          ) : null}
          <button
            type="button"
            className="toastClose"
            onClick={() => onDismiss(toast.id)}
            aria-label={t('common.dismiss')}
          >
            ×
          </button>
        </div>
      ))}
    </div>
  )
}
