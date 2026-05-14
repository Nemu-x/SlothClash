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
  if (toasts.length === 0) return null
  return (
    <div className="toastHub" role="status" aria-live="polite">
      {toasts.map((t) => (
        <div key={t.id} className={`toast toast-${t.kind}`}>
          <span className="toastMsg">{t.message}</span>
          {t.actionLabel ? (
            <button
              type="button"
              className="toastAction"
              onClick={() => {
                t.onAction?.()
                onDismiss(t.id)
              }}
            >
              {t.actionLabel}
            </button>
          ) : null}
          <button
            type="button"
            className="toastClose"
            onClick={() => onDismiss(t.id)}
            aria-label="Dismiss"
          >
            ×
          </button>
        </div>
      ))}
    </div>
  )
}
