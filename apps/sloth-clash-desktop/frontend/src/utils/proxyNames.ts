export function escapeRegExp(s: string): string {
  return s.replace(/[.*+?^${}()|[\]\\]/g, '\\$&')
}

export function isUnsafeGroupName(name: string) {
  const u = name.trim().toUpperCase()
  return u === 'DIRECT' || u === 'REJECT'
}

/** Built-in policy nodes hidden from picker/grid when "show builtin" is off. */
export function isBuiltinProxyNodeName(name: string) {
  const u = String(name ?? '')
    .trim()
    .toUpperCase()
  return u === 'DIRECT' || u === 'REJECT' || u === 'REJECT-DROP' || u === 'PASS'
}

export function filterProxyNodesForDisplay(
  names: string[],
  showBuiltin: boolean,
  selected: string,
): string[] {
  const raw = names.map((x) => String(x))
  if (showBuiltin) return raw
  const filtered = raw.filter((p) => !isBuiltinProxyNodeName(p))
  const sel = String(selected ?? '').trim()
  if (
    sel &&
    isBuiltinProxyNodeName(sel) &&
    raw.includes(sel) &&
    !filtered.includes(sel)
  ) {
    return [sel, ...filtered]
  }
  return filtered
}

export function decodeUnicodeEscapes(input: string): string {
  let s = String(input ?? '')
  // Python-style escapes often found in subscriptions: \U0001F1EA\U0001F1F8
  s = s.replace(/\\U([0-9A-Fa-f]{8})/g, (_m, hex) => {
    const cp = Number.parseInt(hex, 16)
    if (!Number.isFinite(cp) || cp < 0 || cp > 0x10ffff) return _m
    try {
      return String.fromCodePoint(cp)
    } catch {
      return _m
    }
  })
  // Standard JSON-style escapes: \uXXXX
  s = s.replace(/\\u([0-9A-Fa-f]{4})/g, (_m, hex) => {
    const cp = Number.parseInt(hex, 16)
    if (!Number.isFinite(cp) || cp < 0 || cp > 0x10ffff) return _m
    try {
      return String.fromCodePoint(cp)
    } catch {
      return _m
    }
  })
  return s
}

/** Subscription announce header sometimes ships as `base64:…` (UTF-8). */
export function decodeSubscriptionAnnouncementDisplay(raw: string): string {
  let s = String(raw ?? '').trim()
  if (!s) return ''
  if (s.toLowerCase().startsWith('base64:')) {
    const payload = s.slice('base64:'.length).trim().replace(/\s/g, '')
    try {
      const binary = atob(payload)
      const bytes = new Uint8Array(binary.length)
      for (let i = 0; i < binary.length; i++) {
        bytes[i] = binary.charCodeAt(i)
      }
      s = new TextDecoder('utf-8').decode(bytes)
    } catch {
      return decodeUnicodeEscapes(String(raw ?? '').trim())
    }
  }
  return decodeUnicodeEscapes(s)
}

export function extractNodeFlagIso(nodeName: string): string {
  const s = decodeUnicodeEscapes(String(nodeName ?? ''))
    .replace(/[\u200B-\u200D\uFEFF]/g, '')
    .trim()
  if (!s) return ''
  // Support real flag emoji in name (regional indicator pair), e.g. "🇪🇸 Node".
  const chars = [...s]
  for (let i = 0; i < chars.length - 1; i++) {
    const a = chars[i].codePointAt(0) ?? 0
    const b = chars[i + 1].codePointAt(0) ?? 0
    const isRegional = (cp: number) => cp >= 0x1f1e6 && cp <= 0x1f1ff
    if (isRegional(a) && isRegional(b)) {
      const c1 = String.fromCharCode(65 + (a - 0x1f1e6))
      const c2 = String.fromCharCode(65 + (b - 0x1f1e6))
      return `${c1}${c2}`
    }
  }
  // Common pattern: "ES Node", but also support any standalone 2-letter token.
  const m0 = /^([A-Za-z]{2})\b/.exec(s)
  if (m0) {
    const up = m0[1].toUpperCase()
    return up === 'UK' ? 'GB' : up
  }
  const skip = new Set(['WS', 'GR', 'TCP', 'UDP', 'UP', 'IP'])
  const all = s.match(/\b([A-Za-z]{2})\b/g) ?? []
  for (const t of all) {
    const up = t.toUpperCase()
    if (!skip.has(up)) return up === 'UK' ? 'GB' : up
  }
  return ''
}

export function nodeDisplayName(nodeName: string): string {
  const s = decodeUnicodeEscapes(String(nodeName ?? '')).trim()
  if (!s) return '—'
  // Normalize noisy provider prefixes: repeated flag emojis / ISO tokens.
  let out = s
  out = out.replace(/^(?:[\u{1F1E6}-\u{1F1FF}]{2}\s*)+/gu, '')
  out = out.replace(/^(?:[A-Za-z]{2}\s+){1,4}/, '')
  // Some providers add duplicate ISO pair like "ES es Name".
  out = out.replace(/^([A-Za-z]{2})\s+\1\s+/i, '')
  out = out.trim()
  return out || s
}

export function isoToFlagEmoji(iso2: string): string {
  const up = String(iso2 ?? '').toUpperCase()
  if (!/^[A-Z]{2}$/.test(up)) return ''
  return String.fromCodePoint(
    ...[...up].map((c) => 0x1f1e6 + c.charCodeAt(0) - 65),
  )
}

export function nodeFeatureTags(nodeName: string): string[] {
  const s = decodeUnicodeEscapes(String(nodeName ?? '')).toLowerCase()
  const out: string[] = []
  if (s.includes('vless')) out.push('VLESS')
  if (s.includes('vmess')) out.push('VMESS')
  if (s.includes('trojan')) out.push('TROJAN')
  if (s.includes('tuic')) out.push('TUIC')
  if (s.includes('hysteria')) out.push('HYSTERIA')
  if (s.includes('reality')) out.push('REALITY')
  if (s.includes('udp')) out.push('UDP')
  if (s.includes('xudp')) out.push('XUDP')
  if (s.includes('grpc')) out.push('gRPC')
  if (s.includes('xhttp')) out.push('xHTTP')
  if (s.includes('ws')) out.push('WS')
  return [...new Set(out)].slice(0, 4)
}

export function selectedNodeEmoji(value: unknown): string {
  return isoToFlagEmoji(
    extractNodeFlagIso(decodeUnicodeEscapes(String(value ?? ''))),
  )
}

/**
 * Label text next to FlagMark: no leading ISO/country token or duplicate flag emoji
 * when the country is already shown as an image flag (Home Active node menu).
 */
export function proxyNodeLabelBesideFlag(nodeName: string): string {
  const raw = decodeUnicodeEscapes(String(nodeName ?? '')).trim()
  if (!raw) return '—'
  const iso = extractNodeFlagIso(raw)
  let rest = nodeDisplayName(raw)
  if (!rest || rest === '—') rest = raw

  if (iso) {
    rest = rest.replace(new RegExp(`^${escapeRegExp(iso)}\\s+`, 'i'), '').trim()
    const flagEmoji = isoToFlagEmoji(iso)
    if (flagEmoji && rest.startsWith(flagEmoji)) {
      rest = rest.slice(flagEmoji.length).trim()
    }
    rest = rest.replace(/^(?:[\u{1F1E6}-\u{1F1FF}]{2}\s*)+/gu, '').trim()
  }

  return rest || raw
}
