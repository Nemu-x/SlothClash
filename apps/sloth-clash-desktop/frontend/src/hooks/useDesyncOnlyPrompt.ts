import { useCallback, useState } from 'react'

import {
  type DesyncSettings,
  type DesyncStatus,
  GetDesyncStatus,
} from '../api/desync'
import { desyncEnablePatch } from '../utils/desyncStatus'

/**
 * "Turn on Desync?" after a Desync-only profile was activated while Desync is
 * off (without it such a profile routes everything directly).
 *
 * `enable` switches Desync on exactly as the master switch and onboarding
 * "Create profile" do (desyncEnablePatch: a selected Discord gets voice).
 */
export function useDesyncOnlyPrompt({
  save,
  onFailed,
}: {
  /** useDesyncStatus().save: false when another Desync write is in flight. */
  save: (settings: DesyncSettings) => Promise<boolean>
  /** The save was rejected (typically nothing selected). */
  onFailed: (err: unknown) => void
}) {
  const [prompt, setPrompt] = useState<DesyncStatus | null>(null)
  const [busy, setBusy] = useState(false)

  /** Ask, if Desync is off. */
  const offer = useCallback(async () => {
    try {
      const status = await GetDesyncStatus()
      if (status.settings.enabled !== true) setPrompt(status)
    } catch {
      // No status: Home and the card still show that Desync is off.
    }
  }, [])

  const enable = async () => {
    if (!prompt || busy) return
    setBusy(true)
    try {
      // false: another Desync write is in flight; the dialog stays for a
      // retry.
      if (await save({ ...prompt.settings, ...desyncEnablePatch(prompt) }))
        setPrompt(null)
    } catch (err: unknown) {
      setPrompt(null)
      onFailed(err)
    } finally {
      setBusy(false)
    }
  }

  return {
    open: prompt !== null,
    busy,
    offer,
    enable,
    dismiss: () => setPrompt(null),
  }
}
