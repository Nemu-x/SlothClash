import { useQuery } from '@tanstack/react-query'

import { GetRuntimeDiagEvents } from '../../api/diagnostics'
import type { main } from '../../api/models'

const REFETCH_INTERVAL = 3_000

/**
 * Runtime trace ring (Advanced screen). Polls while the Advanced screen is
 * visible so the user sees fresh events without manual refresh, but stays
 * idle everywhere else — the ring is bounded server-side, dropping events on
 * write is fine.
 */
export function useRuntimeDiag(enabled: boolean) {
  const q = useQuery({
    queryKey: ['runtime-diag'],
    queryFn: () =>
      GetRuntimeDiagEvents() as Promise<main.RuntimeDiagEvent[] | null>,
    enabled,
    refetchInterval: enabled ? REFETCH_INTERVAL : false,
    refetchIntervalInBackground: false,
  })
  return {
    events: (q.data ?? []) as main.RuntimeDiagEvent[],
    busy: q.isFetching,
    refresh: () => void q.refetch(),
  }
}
