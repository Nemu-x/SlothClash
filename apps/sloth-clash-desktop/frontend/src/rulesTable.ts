export type RuleRow = {
  idx: number
  type: string
  payload: string
  proxy: string
}

export type RuleProviderRow = {
  name: string
  behavior: string
  ruleCount: number
  vehicleType: string
  updatedAt: string
}

/** mihomo GET /rules returns `rules` as string lines: TYPE,PAYLOAD,POLICY[,opts…] */
function parseMihomoRuleString(line: string, idx: number): RuleRow {
  const parts = line
    .split(',')
    .map((s) => s.trim())
    .filter(Boolean)
  if (parts.length === 0) {
    return { idx, type: '—', payload: '', proxy: '—' }
  }
  const type = parts[0] ?? '—'
  if (parts.length === 1) {
    return { idx, type, payload: '', proxy: '—' }
  }
  // Strip trailing option tokens like no-resolve / src=… from "policy" column
  let end = parts.length - 1
  const last = parts[end]?.toLowerCase() ?? ''
  if (last === 'no-resolve' || last.startsWith('src=')) {
    end--
  }
  const proxy = end >= 1 ? (parts[end] ?? '—') : '—'
  const payload = end > 1 ? parts.slice(1, end).join(', ') : ''
  return { idx, type, payload, proxy }
}

export function parseMihomoRulesJson(
  raw: string | undefined | null,
): RuleRow[] {
  if (!raw?.trim()) return []
  try {
    const data = JSON.parse(raw) as { rules?: unknown[] }
    const rules = data?.rules
    if (!Array.isArray(rules)) return []
    const out: RuleRow[] = []
    for (let i = 0; i < rules.length; i++) {
      const r = rules[i]
      if (typeof r === 'string') {
        out.push(parseMihomoRuleString(r, i))
      } else if (r && typeof r === 'object') {
        const o = r as Record<string, unknown>
        out.push({
          idx: typeof o.index === 'number' ? o.index : i,
          type: String(o.type ?? o.ruleType ?? '—'),
          payload: String(
            o.payload ??
              o.domain ??
              o.domain_suffix ??
              o.ip_cidr ??
              o.rule ??
              '',
          ),
          proxy: String(o.proxy ?? o.outbound ?? o.policy ?? '—'),
        })
      }
    }
    return out
  } catch {
    return []
  }
}

export function parseRuleProvidersJson(
  raw: string | undefined | null,
): RuleProviderRow[] {
  if (!raw?.trim()) return []
  try {
    const data = JSON.parse(raw) as Record<string, unknown>
    const root = (data?.providers ?? data) as Record<string, unknown>
    if (!root || typeof root !== 'object') return []
    const out: RuleProviderRow[] = []
    for (const [name, value] of Object.entries(root)) {
      if (!value || typeof value !== 'object') continue
      const o = value as Record<string, unknown>
      out.push({
        name,
        behavior: String(o.behavior ?? '—'),
        ruleCount: Number(o.ruleCount ?? o.rule_count ?? 0) || 0,
        vehicleType: String(o.vehicleType ?? o.vehicle_type ?? '—'),
        updatedAt: String(o.updatedAt ?? o.updated_at ?? ''),
      })
    }
    out.sort((a, b) => a.name.localeCompare(b.name))
    return out
  } catch {
    return []
  }
}
