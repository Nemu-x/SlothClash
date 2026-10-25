import { type Dispatch, type SetStateAction, useEffect, useRef } from 'react'
import { useTranslation } from 'react-i18next'

import { EventsOn } from '../api/runtime'
import type { Screen } from '../types/app'

import type { Toast } from './useToasts'

export type UpdateProgress = {
  downloaded: number
  total: number
  pct: number
}

// Screens the backend (tray menu) may navigate to.
const NAVIGABLE_SCREENS: Screen[] = [
  'home',
  'proxies',
  'profiles',
  'rules',
  'advanced',
  'settings',
]

/**
 * The app-wide Wails event subscriptions (app:state, app:update,
 * app:update:progress, app:navigate, app:install-config).
 *
 * One effect per event, each re-subscribing only when its own inputs change,
 * so a re-render of App never drops or doubles a listener. The deep-link
 * confirmation (app:install-config-request) lives in ProfileModalsHost.
 */
export function useBackendEvents({
  refresh,
  isAnyEditorModalOpen,
  invalidateUpdateState,
  setUpdateProgress,
  setScreen,
  setError,
  pushToast,
}: {
  refresh: () => Promise<void>
  // app:state pushes are ignored while a YAML editor modal is open.
  isAnyEditorModalOpen: boolean
  invalidateUpdateState: () => void
  setUpdateProgress: (next: UpdateProgress) => void
  setScreen: Dispatch<SetStateAction<Screen>>
  setError: (message: string) => void
  pushToast: (toast: Omit<Toast, 'id'>) => number
}) {
  const { t } = useTranslation()
  const stateEventTimerRef = useRef<number | null>(null)

  // Coalesce bursts of state pushes into one refresh.
  useEffect(() => {
    const off = EventsOn('app:state', () => {
      if (isAnyEditorModalOpen) return
      if (stateEventTimerRef.current !== null) return
      stateEventTimerRef.current = window.setTimeout(() => {
        stateEventTimerRef.current = null
        void refresh()
      }, 120)
    })
    return () => {
      off()
      if (stateEventTimerRef.current !== null) {
        window.clearTimeout(stateEventTimerRef.current)
        stateEventTimerRef.current = null
      }
    }
  }, [refresh, isAnyEditorModalOpen])

  useEffect(() => {
    const off = EventsOn('app:update', () => {
      invalidateUpdateState()
    })
    return () => off()
  }, [invalidateUpdateState])

  // Carried over from App.tsx unchanged: subscribed once; the setter is a
  // state setter, stable for the component's lifetime.
  /* eslint-disable @eslint-react/exhaustive-deps, react-hooks/exhaustive-deps */
  useEffect(() => {
    const off = EventsOn('app:update:progress', (payload: unknown) => {
      const p = payload as
        | { downloaded?: number; total?: number; pct?: number }
        | undefined
      if (!p) return
      setUpdateProgress({
        downloaded: Number(p.downloaded ?? 0),
        total: Number(p.total ?? 0),
        pct: Number(p.pct ?? -1),
      })
    })
    return () => off()
  }, [])

  useEffect(() => {
    const off = EventsOn('app:navigate', (payload: unknown) => {
      const p = payload as { screen?: string } | undefined
      const id = String(p?.screen ?? '').trim() as Screen
      if (!NAVIGABLE_SCREENS.includes(id)) return
      setScreen(id)
      void refresh()
    })
    return () => off()
  }, [refresh])

  useEffect(() => {
    const off = EventsOn('app:install-config', (payload: unknown) => {
      void refresh()
      setScreen('home')
      const p = payload as {
        success?: boolean
        message?: string
        profileName?: string
      }
      if (p?.success) {
        setError('')
        const n = p.profileName
          ? t('ui.toasts.profileAddedFromLink', { name: p.profileName })
          : t('ui.toasts.subAddedFromLink')
        pushToast({ kind: 'success', message: n })
      } else {
        pushToast({
          kind: 'error',
          message: String(p?.message ?? t('ui.toasts.subAddFromLinkFailed')),
          durationMs: 0,
        })
      }
    })
    return () => off()
  }, [refresh, pushToast])
  /* eslint-enable @eslint-react/exhaustive-deps, react-hooks/exhaustive-deps */
}
