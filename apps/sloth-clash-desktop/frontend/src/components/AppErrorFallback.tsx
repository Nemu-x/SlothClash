// Last-resort UI shown by the top-level ErrorBoundary when a render throws.
// Deliberately dependency-free (plain strings, no i18n/hooks) — it must render
// even if the thing that broke is a provider/translation. The backend + tunnel
// keep running (only the WebView UI process is affected), so a reload recovers.
import type { FallbackProps } from 'react-error-boundary'

export function AppErrorFallback({ error, resetErrorBoundary }: FallbackProps) {
  const message =
    error instanceof Error ? error.message : String(error ?? 'Unknown error')
  return (
    <div className="appErrorFallback" role="alert">
      <div className="appErrorIcon" aria-hidden>
        ⚠️
      </div>
      <h2 className="appErrorTitle">Something went wrong</h2>
      <p className="appErrorLead">
        The interface hit an unexpected error. Your connection is unaffected —
        reload the window to recover.
      </p>
      <pre className="appErrorDetail">{message}</pre>
      <div className="appErrorActions">
        <button
          type="button"
          className="btn primary"
          onClick={resetErrorBoundary}
        >
          Try again
        </button>
        <button
          type="button"
          className="btn ghost"
          onClick={() => window.location.reload()}
        >
          Reload window
        </button>
      </div>
    </div>
  )
}
