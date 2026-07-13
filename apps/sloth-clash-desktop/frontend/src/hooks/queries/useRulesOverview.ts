import { useQuery } from '@tanstack/react-query'
import { useCallback } from 'react'

import type { main } from '../../api/models'
import { FetchRulesOverview } from '../../api/rules'

/** Mihomo rules snapshot (Rules screen). `refresh` memoized (audit C1-1). */
export function useRulesOverview(enabled: boolean) {
  const { data, isFetching, refetch } = useQuery({
    queryKey: ['rules-overview'],
    queryFn: () => FetchRulesOverview() as Promise<main.RulesOverview>,
    enabled,
  })
  const refresh = useCallback(() => void refetch(), [refetch])
  return { overview: data ?? null, busy: isFetching, refresh }
}
