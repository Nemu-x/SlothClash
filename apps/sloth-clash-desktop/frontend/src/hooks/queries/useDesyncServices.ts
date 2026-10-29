import { useQuery } from '@tanstack/react-query'

import { type DesyncService, ListDesyncServices } from '../../api/desync'

export const DESYNC_SERVICES_KEY = ['desync-services'] as const

const NONE: DesyncService[] = []

/**
 * The Desync service catalog (fixed per build, fetched once). Empty while it
 * loads or when the backend has none; callers fall back to the service ID for
 * a stored entry the catalog does not list.
 */
export function useDesyncServices(): DesyncService[] {
  const { data } = useQuery({
    queryKey: DESYNC_SERVICES_KEY,
    queryFn: ListDesyncServices,
    staleTime: Infinity,
    gcTime: Infinity,
  })
  return data ?? NONE
}
