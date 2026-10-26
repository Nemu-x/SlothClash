import {
  type QueryClient,
  useQuery,
  useQueryClient,
} from '@tanstack/react-query'
import { useCallback, useEffect, useRef } from 'react'

import {
  GetDesyncAutoStatus,
  GetDesyncStatus,
  RetryDesyncAuto,
  SetDesyncSettings,
  type DesyncAutoStatus,
  type DesyncSettings,
  type DesyncStatus,
} from '../../api/desync'
import type { DesyncContext, DesyncSnapshot } from '../../utils/desyncStatus'

export const DESYNC_STATUS_KEY = ['desync-status'] as const
const REFETCH_INTERVAL = 2_500

/**
 * Write bookkeeping per QueryClient. A poll that started before (or runs
 * during) a settings save must not overwrite the saved result with the
 * pre-save snapshot, so a poll keeps the cached copy when a save is in
 * flight or happened while it was awaiting.
 */
type WriteState = { saving: boolean; revision: number }
const writeStates = new WeakMap<QueryClient, WriteState>()
function writeState(qc: QueryClient): WriteState {
  let w = writeStates.get(qc)
  if (!w) {
    w = { saving: false, revision: 0 }
    writeStates.set(qc, w)
  }
  return w
}

async function fetchSnapshot(qc: QueryClient): Promise<DesyncSnapshot> {
  const w = writeState(qc)
  const cached = () => qc.getQueryData<DesyncSnapshot>(DESYNC_STATUS_KEY)
  const before = cached()
  if (w.saving && before) return before
  const revision = w.revision
  const [status, automatic] = await Promise.all([
    GetDesyncStatus(),
    GetDesyncAutoStatus(),
  ])
  const current = cached()
  if ((w.saving || revision !== w.revision) && current) return current
  return { status, automatic }
}

/**
 * Desync status + automatic calibration, polled every 2.5 s while any
 * `enabled` observer is mounted (the Settings card, and App while Home is
 * shown). Re-polls at once when connection / mode / traffic change, since
 * those gate the calibration.
 *
 * `save` / `retry` are stable (useCallback) so they are safe effect deps.
 */
export function useDesyncStatus(ctx: DesyncContext & { enabled?: boolean }) {
  const qc = useQueryClient()
  const enabled = ctx.enabled !== false
  const q = useQuery({
    queryKey: DESYNC_STATUS_KEY,
    queryFn: () => fetchSnapshot(qc),
    enabled,
    refetchInterval: enabled ? REFETCH_INTERVAL : false,
    refetchIntervalInBackground: false,
  })

  const gateKey = `${ctx.connected}|${ctx.mode}|${ctx.traffic}`
  const lastGateRef = useRef(gateKey)
  useEffect(() => {
    if (lastGateRef.current === gateKey) return
    lastGateRef.current = gateKey
    if (enabled) void qc.invalidateQueries({ queryKey: DESYNC_STATUS_KEY })
  }, [gateKey, enabled, qc])

  /**
   * Apply `settings` (the full object). Returns false without calling the
   * backend when another save is still in flight.
   */
  const save = useCallback(
    async (settings: DesyncSettings): Promise<boolean> => {
      const w = writeState(qc)
      if (w.saving) return false
      w.revision++
      w.saving = true
      try {
        const next: DesyncStatus = await SetDesyncSettings(settings)
        qc.setQueryData<DesyncSnapshot>(DESYNC_STATUS_KEY, (old) =>
          old ? { ...old, status: next } : old,
        )
        return true
      } finally {
        w.saving = false
      }
    },
    [qc],
  )

  const retry = useCallback(async (): Promise<DesyncAutoStatus> => {
    const next = await RetryDesyncAuto()
    qc.setQueryData<DesyncSnapshot>(DESYNC_STATUS_KEY, (old) =>
      old ? { ...old, automatic: next } : old,
    )
    return next
  }, [qc])

  return {
    snapshot: q.data ?? null,
    error: q.error ? String(q.error) : '',
    save,
    retry,
  }
}
