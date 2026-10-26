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

/**
 * Activates a profile and, for a Desync-only one, offers to turn Desync on.
 * `activate` resolves to whether the activation succeeded (it shows the
 * error itself); a failed one keeps the previous profile, so nothing is
 * offered.
 */
export async function activateProfileThenOfferDesync(
  target: unknown,
  activate: () => Promise<boolean>,
  offerDesync: () => Promise<void>,
): Promise<void> {
  if (!(await activate())) return
  if (isDesyncOnlyProfile(target)) await offerDesync()
}
