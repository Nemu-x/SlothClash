import { QueryClientProvider } from '@tanstack/react-query'
import React from 'react'
import { createRoot } from 'react-dom/client'
import { I18nextProvider } from 'react-i18next'

import './style.css'
import App from './App'
import i18n from './i18n'
import { queryClient } from './queryClient'
import { applyUiScale, loadCompactSettings } from './utils/settings'

// Apply the persisted UI zoom before first paint so there's no resize flash.
applyUiScale(loadCompactSettings().uiScale)

const container = document.getElementById('root')

const root = createRoot(container!)

root.render(
  <React.StrictMode>
    <QueryClientProvider client={queryClient}>
      <I18nextProvider i18n={i18n}>
        <App />
      </I18nextProvider>
    </QueryClientProvider>
  </React.StrictMode>,
)
