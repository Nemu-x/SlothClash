import { useQuery } from '@tanstack/react-query'

import { GetCoreBuild } from '../../api/core'
import type { main } from '../../api/models'

/** What the embedded core is built from (upstream tag + patch series). Fixed per build. */
export function useCoreBuild() {
  const { data } = useQuery({
    queryKey: ['core-build'],
    queryFn: () => GetCoreBuild() as Promise<main.CoreBuildInfo>,
    staleTime: Infinity,
  })
  return data ?? null
}
