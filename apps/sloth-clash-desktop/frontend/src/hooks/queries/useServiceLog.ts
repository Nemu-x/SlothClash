import { useQuery } from '@tanstack/react-query'

import { ReadServiceLatestLog } from '../../api/diagnostics'
import type { main } from '../../api/models'

const TAIL_BYTES = 200_000

/** Last 200 KB of the privileged service log (Logs screen). */
export function useServiceLog(enabled: boolean) {
  const q = useQuery({
    queryKey: ['service-log'],
    queryFn: () =>
      ReadServiceLatestLog(TAIL_BYTES) as Promise<main.ServiceLogPeek>,
    enabled,
  })
  return {
    log: q.data ?? null,
    busy: q.isFetching,
    refresh: () => void q.refetch(),
  }
}
