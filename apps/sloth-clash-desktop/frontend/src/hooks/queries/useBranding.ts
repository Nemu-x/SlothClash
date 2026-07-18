import { useQuery, useQueryClient } from '@tanstack/react-query'
import { useEffect } from 'react'

import { GetActiveBranding } from '../../api/branding'
import type { main } from '../../api/models'
import { EventsOn } from '../../api/runtime'

/**
 * Active profile's brand manifest (X-Brand-Desktop-* subscription headers) +
 * disk-cached logos. Null = stock UI. Reads local cache only (no network), so
 * invalidating on every app:state event — profile switches, refreshes — is
 * cheap and keeps branding in lockstep with the active profile.
 */
export function useBranding(): main.ActiveBranding | null {
  const qc = useQueryClient()
  const { data } = useQuery({
    queryKey: ['active-branding'],
    queryFn: () => GetActiveBranding() as Promise<main.ActiveBranding>,
  })
  useEffect(() => {
    const off = EventsOn('app:state', () => {
      void qc.invalidateQueries({ queryKey: ['active-branding'] })
    })
    return () => off()
  }, [qc])
  return data?.manifest ? data : null
}
