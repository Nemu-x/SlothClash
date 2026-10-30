import { useQuery } from '@tanstack/react-query'

import {
  type DesyncCapabilityReport,
  GetDesyncCapabilities,
} from '../../api/desync'

export const DESYNC_CAPABILITIES_KEY = ['desync-capabilities'] as const

/**
 * What this build of the desync library can do on this OS: its methods and
 * the strategy catalog (fixed per build, fetched once). Null while it loads
 * or when the backend cannot report it; callers then show strategies as
 * "Automatic" and leave the capabilities section out.
 */
export function useDesyncCapabilities(
  enabled = true,
): DesyncCapabilityReport | null {
  const { data } = useQuery({
    queryKey: DESYNC_CAPABILITIES_KEY,
    queryFn: GetDesyncCapabilities,
    enabled,
    staleTime: Infinity,
    gcTime: Infinity,
  })
  return data ?? null
}
