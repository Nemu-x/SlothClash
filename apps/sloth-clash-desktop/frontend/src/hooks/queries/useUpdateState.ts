import { useQuery, useQueryClient } from '@tanstack/react-query'
import { useCallback } from 'react'

import { CheckForUpdates, GetUpdateState } from '../../api/update'

/**
 * Cached snapshot of update-state. The backend pushes 'app:update' when fresh
 * data is available; we invalidate on that event. Manual checks call
 * `runCheck()` which uses `CheckForUpdates` and refreshes the cache.
 *
 * `invalidate`/`runCheck` MUST be stable (useCallback): they are used as effect
 * dependencies in App.tsx. A fresh identity each render made the Settings-screen
 * effect (`if (screen==='settings') invalidateUpdateState()`) re-run on every
 * render → invalidate → refetch → re-render → invalidate … an infinite loop that
 * churned the JS heap into the gigabytes while Settings was open.
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
  const runCheck = useCallback(async () => {
    const next = await CheckForUpdates()
    qc.setQueryData(['update-state'], next)
    return next
  }, [qc])
  const invalidate = useCallback(() => {
    void qc.invalidateQueries({ queryKey: ['update-state'] })
  }, [qc])
  return {
    snap: q.data ?? null,
    busy: q.isFetching,
    invalidate,
    runCheck,
  }
}
