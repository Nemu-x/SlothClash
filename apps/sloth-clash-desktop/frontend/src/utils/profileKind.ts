/** Mirrors profileKindDesyncOnly (profile_desync_only.go). */
export const PROFILE_KIND_DESYNC_ONLY = 'desync-only'

/**
 * A Desync-only profile (no VPN): no subscription and no nodes, only the
 * Desync routes with everything else direct. Profiles saved before the kind
 * existed carry none and are subscription / local ones.
 */
export function isDesyncOnlyProfile(p: unknown): boolean {
  return (
    typeof p === 'object' &&
    p !== null &&
    (p as { kind?: unknown }).kind === PROFILE_KIND_DESYNC_ONLY
  )
}
