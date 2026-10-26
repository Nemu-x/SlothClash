import { useQuery } from '@tanstack/react-query'

import { type DesyncPreset, ListDesyncPresets } from '../../api/desync'

export const DESYNC_PRESETS_KEY = ['desync-presets'] as const

const NONE: DesyncPreset[] = []

/**
 * The Desync preset catalog (fixed per build, fetched once). Empty while it
 * loads or when the backend has none; callers fall back to the preset ID for
 * a stored entry the catalog does not list.
 */
export function useDesyncPresets(): DesyncPreset[] {
  const { data } = useQuery({
    queryKey: DESYNC_PRESETS_KEY,
    queryFn: ListDesyncPresets,
    staleTime: Infinity,
    gcTime: Infinity,
  })
  return data ?? NONE
}
