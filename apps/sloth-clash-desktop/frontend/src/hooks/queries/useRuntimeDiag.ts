import { useQuery } from '@tanstack/react-query'
import { useCallback } from 'react'

import { GetRuntimeDiagEvents } from '../../api/diagnostics'
import type { main } from '../../api/models'

const REFETCH_INTERVAL = 3_000

/** Runtime trace ring (Advanced screen). `refresh` memoized (audit C1-1). */
export function useRuntimeDiag(enabled: boolean) {
  const { data, isFetching, refetch } = useQuery({
    queryKey: ['runtime-diag'],
    queryFn: () =>
      GetRuntimeDiagEvents() as Promise<main.RuntimeDiagEvent[] | null>,
    enabled,
    refetchInterval: enabled ? REFETCH_INTERVAL : false,
    refetchIntervalInBackground: false,
  })
  const refresh = useCallback(() => void refetch(), [refetch])
  return {
    events: (data ?? []) as main.RuntimeDiagEvent[],
    busy: isFetching,
    refresh,
  }
}
