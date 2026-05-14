import { useQuery, useQueryClient } from '@tanstack/react-query'

import { CheckForUpdates, GetUpdateState } from '../../api/update'

/**
 * Cached snapshot of update-state. The backend pushes 'app:update' when fresh
 * data is available; we invalidate on that event. Manual checks call
 * `runCheck()` which uses `CheckForUpdates` and refreshes the cache.
 */
export function useUpdateState() {
  const qc = useQueryClient()
  const q = useQuery({
    queryKey: ['update-state'],
    queryFn: () => GetUpdateState(),
    // The backend refreshes on its own schedule; we only need the latest copy.
    refetchInterval: false,
    refetchOnMount: 'always',
  })
  const runCheck = async () => {
    const next = await CheckForUpdates()
    qc.setQueryData(['update-state'], next)
    return next
  }
  return {
    snap: q.data ?? null,
    busy: q.isFetching,
    invalidate: () => void qc.invalidateQueries({ queryKey: ['update-state'] }),
    runCheck,
  }
}
