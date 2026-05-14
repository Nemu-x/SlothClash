import { useQuery, useQueryClient } from '@tanstack/react-query'

import {
  CloseAllConnections,
  FetchConnectionsOverview,
} from '../../api/connections'
import type { ConnectionsOverview } from '../../types/app'

/**
 * Connections snapshot poll — auto-refetches every 3.5s while `enabled`,
 * mirroring the prior `setInterval` loop in App.tsx that ran whenever the
 * Connections screen was visible.
 */
export function useConnectionsOverview(enabled: boolean) {
  const qc = useQueryClient()
  const q = useQuery({
    queryKey: ['connections-overview'],
    queryFn: () => FetchConnectionsOverview() as Promise<ConnectionsOverview>,
    enabled,
    refetchInterval: enabled ? 3500 : false,
  })
  const closeAll = async () => {
    await CloseAllConnections()
    void qc.invalidateQueries({ queryKey: ['connections-overview'] })
  }
  return {
    overview: q.data ?? null,
    busy: q.isFetching,
    refresh: () => void q.refetch(),
    closeAll,
  }
}
