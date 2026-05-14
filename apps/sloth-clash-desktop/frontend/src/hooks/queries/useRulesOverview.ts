import { useQuery } from '@tanstack/react-query'

import type { main } from '../../api/models'
import { FetchRulesOverview } from '../../api/rules'

/**
 * Mihomo rules snapshot. Only fetches when `enabled` (Rules screen is open).
 * Manual refresh via the returned `refresh()` (used by the bulk
 * "Update all providers" button after it cycles through the providers).
 */
export function useRulesOverview(enabled: boolean) {
  const q = useQuery({
    queryKey: ['rules-overview'],
    queryFn: () => FetchRulesOverview() as Promise<main.RulesOverview>,
    enabled,
  })
  return {
    overview: q.data ?? null,
    busy: q.isFetching,
    refresh: () => void q.refetch(),
  }
}
