import {
  type QueryClient,
  useQuery,
  useQueryClient,
} from '@tanstack/react-query'
import { useCallback, useEffect, useRef } from 'react'

import {
  AddDesyncCustomDomain,
  AddDesyncRule,
  AddDesyncService,
  GetDesyncAutoStatus,
  GetDesyncStatus,
  RemoveDesyncCustomDomain,
  RetryDesyncAuto,
  SetDesyncCustomDomainEnabled,
  SetDesyncSettings,
  UpdateDesyncRule,
  type DesyncAutoStatus,
  type DesyncRuleInput,
  type DesyncSettings,
  type DesyncStatus,
} from '../../api/desync'
import type { DesyncContext, DesyncSnapshot } from '../../utils/desyncStatus'

export const DESYNC_STATUS_KEY = ['desync-status'] as const
const REFETCH_INTERVAL = 2_500
// While Desync is off nothing is calibrating; poll just often enough to
// notice it being enabled elsewhere (Home observes this query too).
const REFETCH_INTERVAL_DISABLED = 15_000

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
 * Desync status + automatic calibration, polled every 2.5 s (15 s while
 * Desync is off) while any
 * `enabled` observer is mounted (the Settings card, and App while Home is
 * shown). Re-polls at once when connection / mode / traffic change, since
 * those gate the calibration, and when `profileId` (the active profile,
 * whose settings these are) changes.
 *
 * `save`, the custom-entry writes and `retry` are stable (useCallback) so they are safe effect deps.
 */
export function useDesyncStatus(
  ctx: DesyncContext & { enabled?: boolean; profileId?: string },
) {
  const qc = useQueryClient()
  const enabled = ctx.enabled !== false
  const q = useQuery({
    queryKey: DESYNC_STATUS_KEY,
    queryFn: () => fetchSnapshot(qc),
    enabled,
    refetchInterval: (query) => {
      if (!enabled) return false
      return query.state.data?.status.settings.enabled === false
        ? REFETCH_INTERVAL_DISABLED
        : REFETCH_INTERVAL
    },
    refetchIntervalInBackground: false,
  })

  const gateKey = `${ctx.connected}|${ctx.mode}|${ctx.traffic}|${ctx.profileId ?? ''}`
  const lastGateRef = useRef(gateKey)
  useEffect(() => {
    if (lastGateRef.current === gateKey) return
    lastGateRef.current = gateKey
    if (enabled) void qc.invalidateQueries({ queryKey: DESYNC_STATUS_KEY })
  }, [gateKey, enabled, qc])

  /**
   * Run one settings write and store the status it returns. Returns false
   * without calling the backend when another write is still in flight.
   */
  const write = useCallback(
    async (call: () => Promise<DesyncStatus>): Promise<boolean> => {
      const w = writeState(qc)
      if (w.saving) return false
      w.revision++
      w.saving = true
      try {
        const next = await call()
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

  /** Apply `settings` (the full object; the custom list is not changed). */
  const save = useCallback(
    (settings: DesyncSettings) => write(() => SetDesyncSettings(settings)),
    [write],
  )
  const addCustom = useCallback(
    (domain: string) => write(() => AddDesyncCustomDomain(domain)),
    [write],
  )
  const addRule = useCallback(
    (rule: DesyncRuleInput) => write(() => AddDesyncRule(rule)),
    [write],
  )
  const updateRule = useCallback(
    (id: string, rule: DesyncRuleInput) =>
      write(() => UpdateDesyncRule(id, rule)),
    [write],
  )
  /**
   * Add catalog service `id`. Resolves to the domains of the plain entries it
   * replaced (the backend drops those the service covers), or false when
   * another write is in flight.
   */
  const addService = useCallback(
    async (id: string): Promise<string[] | false> => {
      let replaced: string[] = []
      const ok = await write(async () => {
        const before =
          qc.getQueryData<DesyncSnapshot>(DESYNC_STATUS_KEY)?.status.settings
            .custom ?? []
        const next = await AddDesyncService(id)
        const kept = new Set((next.settings.custom ?? []).map((e) => e.id))
        replaced = before
          .filter((e) => !e.service && !kept.has(e.id))
          .map((e) => e.domain)
        return next
      })
      return ok ? replaced : false
    },
    [qc, write],
  )
  const removeCustom = useCallback(
    (id: string) => write(() => RemoveDesyncCustomDomain(id)),
    [write],
  )
  const setCustomEnabled = useCallback(
    (id: string, enabled: boolean) =>
      write(() => SetDesyncCustomDomainEnabled(id, enabled)),
    [write],
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
    addCustom,
    addRule,
    updateRule,
    addService,
    removeCustom,
    setCustomEnabled,
    retry,
  }
}
