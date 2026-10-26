import { useCallback, useState } from 'react'

import type { DesyncPanelVariant } from '../components/DesyncPanel'

export type DesyncModalState = {
  /** null = closed; otherwise which flavor of the dialog is shown. */
  view: DesyncPanelVariant | null
  /** The Desync controls (Home chip, Settings, a Desync-only profile card). */
  openManage: () => void
  /** "Unblock without VPN": pick services, then create a Desync-only profile. */
  openOnboarding: () => void
  close: () => void
}

/**
 * Whether the Desync modal is open, and in which flavor. Held by App: Home,
 * Profiles, Settings and the import dialog all open it.
 */
export function useDesyncModal(): DesyncModalState {
  const [view, setView] = useState<DesyncPanelVariant | null>(null)
  const openManage = useCallback(() => setView('manage'), [])
  const openOnboarding = useCallback(() => setView('onboarding'), [])
  const close = useCallback(() => setView(null), [])
  return { view, openManage, openOnboarding, close }
}
