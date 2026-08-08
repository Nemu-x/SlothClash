import { Component, type ErrorInfo, type ReactNode } from 'react'
import { useTranslation } from 'react-i18next'

// The screen-content error boundary. A render crash in one page (bad data from
// the core, a null deref in a rarely-hit branch) used to blank the whole app —
// this catches it, shows a recoverable fallback in the content area, and leaves
// the sidebar alive so the user can switch screens. In App.tsx it is keyed by
// the active screen, so navigating away and back remounts it and clears the
// error. Error boundaries must be class components (no hook equivalent).
type Props = { children: ReactNode }
type State = { error: Error | null }

export class ErrorBoundary extends Component<Props, State> {
  state: State = { error: null }

  static getDerivedStateFromError(error: Error): State {
    return { error }
  }

  componentDidCatch(error: Error, info: ErrorInfo) {
    // Surface to the console / Wails devtools so a crash is diagnosable.
    console.error(
      'ErrorBoundary caught a render error:',
      error,
      info?.componentStack,
    )
  }

  render() {
    if (this.state.error) {
      return (
        <ErrorFallback
          error={this.state.error}
          onReload={() => window.location.reload()}
        />
      )
    }
    return this.props.children
  }
}

// Functional fallback so it can use i18n (the boundary class cannot use hooks).
function ErrorFallback({
  error,
  onReload,
}: {
  error: Error
  onReload: () => void
}) {
  const { t } = useTranslation()
  const details = [
    `${error?.name ?? 'Error'}: ${error?.message ?? ''}`,
    error?.stack ?? '',
  ]
    .join('\n')
    .trim()
  return (
    <div className="panel errorBoundary" role="alert">
      <h2 className="errorBoundaryTitle">⚠️ {t('ui.errorBoundary.title')}</h2>
      <p className="muted">{t('ui.errorBoundary.body')}</p>
      <pre className="errorBoundaryDetails">{details}</pre>
      <div className="row">
        <button
          type="button"
          className="btn"
          onClick={() => void navigator.clipboard.writeText(details)}
        >
          {t('ui.errorBoundary.copy')}
        </button>
        <button type="button" className="btn primary" onClick={onReload}>
          {t('ui.errorBoundary.reload')}
        </button>
      </div>
    </div>
  )
}
