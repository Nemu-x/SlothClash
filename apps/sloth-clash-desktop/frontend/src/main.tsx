import { QueryClientProvider } from '@tanstack/react-query'
import React from 'react'
import { createRoot } from 'react-dom/client'
import { ErrorBoundary } from 'react-error-boundary'
import { I18nextProvider } from 'react-i18next'

import './style.css'
import App from './App'
import { AppErrorFallback } from './components/AppErrorFallback'
import i18n from './i18n'
import { queryClient } from './queryClient'
import { applyUiScale, loadCompactSettings } from './utils/settings'

// Global safety net: surface (don't silently swallow) unhandled promise
// rejections and uncaught errors. They no longer vanish into the void — at
// minimum they hit the console for post-mortem, and never crash the app.
window.addEventListener('unhandledrejection', (e) => {
  console.error('[unhandledrejection]', e.reason)
})
window.addEventListener('error', (e) => {
  console.error('[uncaught error]', e.error ?? e.message)
})

// Apply the persisted UI zoom before first paint so there's no resize flash.
applyUiScale(loadCompactSettings().uiScale)

const container = document.getElementById('root')

const root = createRoot(container!)

root.render(
  <React.StrictMode>
    <QueryClientProvider client={queryClient}>
      <I18nextProvider i18n={i18n}>
        <ErrorBoundary FallbackComponent={AppErrorFallback}>
          <App />
        </ErrorBoundary>
      </I18nextProvider>
    </QueryClientProvider>
  </React.StrictMode>,
)
