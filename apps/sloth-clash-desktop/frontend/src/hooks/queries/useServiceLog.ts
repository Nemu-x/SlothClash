import { useQuery } from '@tanstack/react-query'
import { useCallback } from 'react'

import { ReadServiceLatestLog } from '../../api/diagnostics'
import type { main } from '../../api/models'

const TAIL_BYTES = 200_000

/** Last 200 KB of the privileged service log. `refresh` memoized (audit C1-1). */
export function useServiceLog(enabled: boolean) {
  const { data, isFetching, refetch } = useQuery({
    queryKey: ['service-log'],
    queryFn: () =>
      ReadServiceLatestLog(TAIL_BYTES) as Promise<main.ServiceLogPeek>,
    enabled,
  })
  const refresh = useCallback(() => void refetch(), [refetch])
  return { log: data ?? null, busy: isFetching, refresh }
}
