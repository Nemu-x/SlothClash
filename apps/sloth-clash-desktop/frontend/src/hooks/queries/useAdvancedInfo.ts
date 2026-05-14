import { useQuery } from '@tanstack/react-query'

import { GetAdvancedGeoStatus, GetAdvancedPaths } from '../../api/diagnostics'
import type { main } from '../../api/models'

/** Paths + geo-data status for the Advanced screen. Polled lazily so a
 *  re-extract / restart-core action surfaces a fresh size/mtime without the
 *  user having to navigate away. */
export function useAdvancedInfo(enabled: boolean) {
  const paths = useQuery({
    queryKey: ['advanced-paths'],
    queryFn: () => GetAdvancedPaths() as Promise<main.AdvancedPaths>,
    enabled,
  })
  const geo = useQuery({
    queryKey: ['advanced-geo'],
    queryFn: () => GetAdvancedGeoStatus() as Promise<main.AdvancedGeoStatus>,
    enabled,
    refetchInterval: enabled ? 8_000 : false,
  })
  return {
    paths: paths.data ?? null,
    geo: geo.data ?? null,
    refresh: () => {
      void paths.refetch()
      void geo.refetch()
    },
  }
}
