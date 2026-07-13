import { useQuery } from '@tanstack/react-query'
import { useCallback } from 'react'

import { GetAdvancedGeoStatus, GetAdvancedPaths } from '../../api/diagnostics'
import type { main } from '../../api/models'

/** Paths + geo-data status (Advanced screen). `refresh` memoized (audit C1-1). */
export function useAdvancedInfo(enabled: boolean) {
  const { data: pathsData, refetch: refetchPaths } = useQuery({
    queryKey: ['advanced-paths'],
    queryFn: () => GetAdvancedPaths() as Promise<main.AdvancedPaths>,
    enabled,
  })
  const { data: geoData, refetch: refetchGeo } = useQuery({
    queryKey: ['advanced-geo'],
    queryFn: () => GetAdvancedGeoStatus() as Promise<main.AdvancedGeoStatus>,
    enabled,
    refetchInterval: enabled ? 8_000 : false,
  })
  const refresh = useCallback(() => {
    void refetchPaths()
    void refetchGeo()
  }, [refetchPaths, refetchGeo])
  return { paths: pathsData ?? null, geo: geoData ?? null, refresh }
}
