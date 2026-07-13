import { useQuery, useQueryClient } from '@tanstack/react-query'
import { useCallback } from 'react'

import {
  CloseAllConnections,
  FetchConnectionsOverview,
} from '../../api/connections'
import type { ConnectionsOverview } from '../../types/app'

/**
 * Connections snapshot poll — auto-refetches every 3.5s while `enabled`.
 * `refresh`/`closeAll` are memoized: a fresh identity each render is the footgun
 * that caused the useUpdateState render-loop (audit C1-1).
 */
export function useConnectionsOverview(enabled: boolean) {
  const qc = useQueryClient()
  const { data, isFetching, refetch } = useQuery({
    queryKey: ['connections-overview'],
    queryFn: () => FetchConnectionsOverview() as Promise<ConnectionsOverview>,
    enabled,
    refetchInterval: enabled ? 3500 : false,
  })
  const refresh = useCallback(() => void refetch(), [refetch])
  const closeAll = useCallback(async () => {
    await CloseAllConnections()
    void qc.invalidateQueries({ queryKey: ['connections-overview'] })
  }, [qc])
  return { overview: data ?? null, busy: isFetching, refresh, closeAll }
}
