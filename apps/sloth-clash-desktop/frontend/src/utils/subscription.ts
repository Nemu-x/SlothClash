import { formatBytesSmart } from './format'

export function supportSubscriptionUrlKind(url: string): 'telegram' | 'web' {
  const u = String(url ?? '').toLowerCase()
  if (
    u.startsWith('tg:') ||
    u.includes('t.me/') ||
    u.includes('telegram.me/') ||
    u.includes('telegram.org')
  ) {
    return 'telegram'
  }
  return 'web'
}

export function profileSubscriptionHost(url: string): string {
  const s = String(url ?? '').trim()
  if (!s) return ''
  try {
    const u = new URL(s.includes('://') ? s : `https://${s}`)
    return u.hostname || ''
  } catch {
    return ''
  }
}

/** Structured `Subscription-Userinfo`. All fields optional — providers are
 *  inconsistent about which they send, and a missing one must not blank the
 *  rest. `expiresAt` is a unix-seconds timestamp. */
export type SubscriptionUsage = {
  usedBytes: number
  totalBytes: number
  expiresAt: number
}

const parseNum = (v: unknown): number => {
  const n = Number(v)
  return Number.isFinite(n) ? n : 0
}

/**
 * Parses the header in both shapes seen in the wild: the standard flat
 * `upload=…; download=…; total=…; expire=…` and the JSON some panels emit.
 * Returns null when nothing usable is present.
 */
export function parseSubscriptionUsage(profile: any): SubscriptionUsage | null {
  const raw = String(profile?.subscriptionInfo ?? '').trim()
  if (!raw) return null

  const build = (
    up: number,
    down: number,
    usedOnce: number,
    total: number,
    expire: number,
  ): SubscriptionUsage | null => {
    const usedBytes = up + down > 0 ? up + down : Math.max(0, usedOnce)
    const totalBytes = total > 0 ? total : 0
    if (usedBytes <= 0 && totalBytes <= 0 && expire <= 0) return null
    return { usedBytes, totalBytes, expiresAt: expire > 0 ? expire : 0 }
  }

  try {
    const obj = JSON.parse(raw)
    if (obj && typeof obj === 'object' && !Array.isArray(obj)) {
      const u =
        obj.usage && typeof obj.usage === 'object' && !Array.isArray(obj.usage)
          ? obj.usage
          : obj
      const got = build(
        parseNum(u.upload ?? u.u ?? u.used_upload ?? obj.used_upload),
        parseNum(u.download ?? u.d ?? u.used_download ?? obj.used_download),
        parseNum(u.used ?? obj.used),
        parseNum(u.total ?? u.t ?? u.size ?? obj.t),
        parseNum(u.expire ?? u.expiry ?? u.expires_at ?? obj.expire),
      )
      if (got) return got
    }
  } catch {
    // not JSON — fall through to the flat form
  }

  const flat: Record<string, string> = {}
  for (const part of raw.split(/[;&,\n]/)) {
    const seg = part.trim()
    if (!seg.includes('=')) continue
    const i = seg.indexOf('=')
    flat[seg.slice(0, i).trim().toLowerCase()] = seg.slice(i + 1).trim()
  }
  return build(
    parseNum(flat.upload ?? flat.u),
    parseNum(flat.download ?? flat.d),
    parseNum(flat.used),
    parseNum(flat.total ?? flat.t ?? flat.size),
    parseNum(flat.expire ?? flat.expiry ?? flat.expires_at),
  )
}

/** `used / total` from Subscription-Userinfo (total missing ⇒ `0 B`). */
export function profileTrafficPair(profile: any): string {
  const usage = parseSubscriptionUsage(profile)
  if (!usage) return ''
  const { usedBytes, totalBytes } = usage
  return `${formatBytesSmart(Math.max(0, usedBytes))} / ${formatBytesSmart(totalBytes > 0 ? totalBytes : 0)}`
}

export function profileTrafficLine(profile: any): string {
  const p = profileTrafficPair(profile)
  return p ? `Traffic: ${p}` : ''
}

/** Whole days until the subscription expires; null when no expiry is known.
 *  Negative means it already lapsed. */
export function subscriptionDaysLeft(
  usage: SubscriptionUsage | null,
): number | null {
  if (!usage || usage.expiresAt <= 0) return null
  const ms = usage.expiresAt * 1000 - Date.now()
  return Math.ceil(ms / 86_400_000)
}
