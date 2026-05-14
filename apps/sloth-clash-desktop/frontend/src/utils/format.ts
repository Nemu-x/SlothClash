/** mihomo /traffic reports speeds in kbps */
export function formatSpeedKbps(kbps: number | undefined): string {
  if (kbps == null || kbps < 0) return '—'
  if (kbps === 0) return '0 KB/s'
  if (kbps >= 1024) {
    const mb = kbps / 1024
    return `${mb >= 10 ? Math.round(mb) : mb.toFixed(1)} MB/s`
  }
  return `${kbps} KB/s`
}

export function formatBytesSmart(n: number): string {
  if (!Number.isFinite(n) || n < 0) return '—'
  const units = ['B', 'KB', 'MB', 'GB', 'TB']
  let v = n
  let i = 0
  while (v >= 1024 && i < units.length - 1) {
    v /= 1024
    i++
  }
  const d = v >= 10 || i === 0 ? 0 : 1
  return `${v.toFixed(d)} ${units[i]}`
}

export function formatProfileAgo(lastUpdatedUnix: number): string {
  if (!Number.isFinite(lastUpdatedUnix) || lastUpdatedUnix <= 0) return ''
  const ms = lastUpdatedUnix < 1e12 ? lastUpdatedUnix * 1000 : lastUpdatedUnix
  const diff = Date.now() - ms
  if (diff < 0) return ''
  const s = Math.floor(diff / 1000)
  if (s < 60) return `${s}s ago`
  const m = Math.floor(s / 60)
  if (m < 60) return `${m}m ago`
  const h = Math.floor(m / 60)
  if (h < 48) return `${h}h ago`
  const d = Math.floor(h / 24)
  return `${d}d ago`
}

export function formatProviderUpdatedAt(raw: string): string {
  const s = String(raw ?? '').trim()
  if (!s) return '—'
  const ts = Date.parse(s)
  if (!Number.isFinite(ts)) return s
  const ago = formatProfileAgo(Math.floor(ts / 1000))
  return ago || new Date(ts).toLocaleString()
}
