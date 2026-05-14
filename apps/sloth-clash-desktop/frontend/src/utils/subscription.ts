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

/** `used / total` from Subscription-Userinfo (total missing ⇒ `0 B`). */
export function profileTrafficPair(profile: any): string {
  const raw = String(profile?.subscriptionInfo ?? '').trim()
  if (!raw) return ''
  const parseNum = (v: unknown): number => {
    const n = Number(v)
    return Number.isFinite(n) ? n : 0
  }
  const pair = (usedBytes: number, totalBytes: number): string => {
    const u = Math.max(0, usedBytes)
    const t = totalBytes > 0 ? totalBytes : 0
    return `${formatBytesSmart(u)} / ${t > 0 ? formatBytesSmart(t) : formatBytesSmart(0)}`
  }
  const fromObj = (obj: any): string => {
    if (!obj || typeof obj !== 'object' || Array.isArray(obj)) return ''
    const u =
      obj.usage && typeof obj.usage === 'object' && !Array.isArray(obj.usage)
        ? obj.usage
        : obj
    const up = parseNum(
      u.upload ?? u.u ?? u.used_upload ?? obj.upload ?? obj.used_upload,
    )
    const down = parseNum(
      u.download ?? u.d ?? u.used_download ?? obj.download ?? obj.used_download,
    )
    const total = parseNum(u.total ?? u.t ?? u.size ?? obj.total ?? obj.t)
    const usedOnce = parseNum(u.used ?? obj.used)
    const usedBytes = up + down > 0 ? up + down : usedOnce
    return pair(usedBytes, total)
  }
  try {
    const parsed = JSON.parse(raw)
    const s = fromObj(parsed)
    if (s) return s
  } catch {
    // fall through
  }
  const flat: Record<string, string> = {}
  for (const part of raw.split(/[;&,\n]/)) {
    const seg = part.trim()
    if (!seg.includes('=')) continue
    const i = seg.indexOf('=')
    flat[seg.slice(0, i).trim().toLowerCase()] = seg.slice(i + 1).trim()
  }
  const up = Number(flat.upload ?? flat.u ?? 0)
  const down = Number(flat.download ?? flat.d ?? 0)
  const total = Number(flat.total ?? flat.t ?? flat.size ?? 0)
  const usedFlat = Number(flat.used ?? 0)
  const usedSum =
    (Number.isFinite(up) ? up : 0) + (Number.isFinite(down) ? down : 0)
  const usedBytes =
    usedSum > 0 ? usedSum : Number.isFinite(usedFlat) ? usedFlat : 0
  if (
    usedSum > 0 ||
    (Number.isFinite(usedFlat) && usedFlat > 0) ||
    (Number.isFinite(total) && total > 0)
  ) {
    return pair(usedBytes, Number.isFinite(total) ? total : 0)
  }
  return ''
}

export function profileTrafficLine(profile: any): string {
  const p = profileTrafficPair(profile)
  return p ? `Traffic: ${p}` : ''
}
