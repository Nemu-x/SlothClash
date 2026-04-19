import YAML from 'yaml'

export const DEFAULT_MERGE_TEMPLATE =
  '# Profile Enhancement Merge Template for SlothClash\n'
export const DEFAULT_EDITOR_TEMPLATE = 'prepend: []\nappend: []\ndelete: []\n'

export function mergeTemplateFromProfile(
  profiles: unknown[] | undefined,
  profileId: string,
): string {
  const p = profiles?.find((x: any) => String(x?.id ?? '') === profileId) as
    | { mergeTemplate?: string }
    | undefined
  return String(p?.mergeTemplate ?? '')
}

export function proxyTemplateFromProfile(
  profiles: unknown[] | undefined,
  profileId: string,
): string {
  const p = profiles?.find((x: any) => String(x?.id ?? '') === profileId) as
    | { proxyTemplate?: string }
    | undefined
  const raw = String(p?.proxyTemplate ?? '')
  return raw.trim() ? raw : DEFAULT_EDITOR_TEMPLATE
}

export function rulesTemplateFromProfile(
  profiles: unknown[] | undefined,
  profileId: string,
): string {
  const p = profiles?.find((x: any) => String(x?.id ?? '') === profileId) as
    | { rulesTemplate?: string }
    | undefined
  const raw = String(p?.rulesTemplate ?? '')
  return raw.trim() ? raw : DEFAULT_EDITOR_TEMPLATE
}

export function parseMergeDoc(raw: string): Record<string, unknown> {
  try {
    const o = YAML.parse(raw) as Record<string, unknown>
    return o && typeof o === 'object' ? o : {}
  } catch {
    return {}
  }
}

export function stringifyMerge(doc: Record<string, unknown>): string {
  return YAML.stringify(doc, { lineWidth: 120, indent: 2 })
}

export type ProxyGroupRow = {
  id: string
  name: string
  type: string
  use: string
  url: string
  interval: number
  timeout: number
  maxFailedTimes: number
  lazy: boolean
}

export type ProxyBuckets = {
  prepend: ProxyGroupRow[]
  append: ProxyGroupRow[]
  delete: string[]
}

function rowToProxyGroupObj(r: ProxyGroupRow): Record<string, unknown> {
  const use = r.use
    .split(',')
    .map((s) => s.trim())
    .filter(Boolean)
  const out: Record<string, unknown> = {
    type: (r.type || 'url-test').trim(),
    name: r.name.trim(),
    interval: Number(r.interval || 300),
    timeout: Number(r.timeout || 3000),
    'max-failed-times': Number(r.maxFailedTimes || 5),
    lazy: Boolean(r.lazy),
    url: (r.url || 'http://www.gstatic.com/generate_204').trim(),
  }
  if (use.length) out.use = use
  return out
}

function proxyGroupObjToRow(
  gm: Record<string, unknown>,
  idx: number,
): ProxyGroupRow | null {
  const name = String(gm.name ?? '').trim()
  if (!name) return null
  const useArr = gm.use as unknown[] | undefined
  const proxiesArr = gm.proxies as unknown[] | undefined
  let use: string[] = []
  if (Array.isArray(useArr)) {
    use = useArr.map((x) => String(x)).filter(Boolean)
  } else if (Array.isArray(proxiesArr)) {
    use = proxiesArr.map((x) => String(x)).filter(Boolean)
  }
  return {
    id: `pg-${idx}-${name}`,
    name,
    type: String(gm.type ?? 'url-test').trim(),
    use: use.join(', '),
    url: String(gm.url ?? 'http://www.gstatic.com/generate_204').trim(),
    interval: Number(gm.interval ?? 300),
    timeout: Number(gm.timeout ?? 3000),
    maxFailedTimes: Number(gm['max-failed-times'] ?? 5),
    lazy: Boolean(gm.lazy ?? true),
  }
}

export function proxyBucketsFromMerge(raw: string): ProxyBuckets {
  const doc = parseMergeDoc(raw)
  const fromObjKey = (obj: unknown, key: string): unknown => {
    if (!obj || typeof obj !== 'object' || Array.isArray(obj)) return undefined
    return (obj as Record<string, unknown>)[key]
  }
  const prep =
    fromObjKey(doc.prepend, 'proxy-groups') ??
    doc.prepend ??
    doc['proxy-groups']
  const app = fromObjKey(doc.append, 'proxy-groups') ?? doc.append
  const del = fromObjKey(doc.delete, 'proxy-groups') ?? doc.delete

  const mapRows = (arr: unknown): ProxyGroupRow[] => {
    if (!Array.isArray(arr)) return []
    const out: ProxyGroupRow[] = []
    for (let i = 0; i < arr.length; i++) {
      const g = arr[i]
      if (!g || typeof g !== 'object') continue
      const row = proxyGroupObjToRow(g as Record<string, unknown>, i)
      if (row) out.push(row)
    }
    return out
  }

  return {
    prepend: mapRows(prep),
    append: mapRows(app),
    delete: Array.isArray(del)
      ? del.map((x) => String(x).trim()).filter(Boolean)
      : [],
  }
}

export function proxyBucketsToAdvancedYaml(b: ProxyBuckets): string {
  return YAML.stringify(
    {
      prepend: b.prepend.map(rowToProxyGroupObj),
      append: b.append.map(rowToProxyGroupObj),
      delete: b.delete,
    },
    { lineWidth: 120, indent: 2 },
  )
}

export function proxyBucketsFromAdvancedYaml(raw: string): ProxyBuckets {
  try {
    const doc = (YAML.parse(raw) ?? {}) as Record<string, unknown>
    const fromObjKey = (obj: unknown, key: string): unknown => {
      if (!obj || typeof obj !== 'object') return undefined
      return (obj as Record<string, unknown>)[key]
    }
    const parseRows = (arr: unknown): ProxyGroupRow[] => {
      if (!Array.isArray(arr)) return []
      const out: ProxyGroupRow[] = []
      for (let i = 0; i < arr.length; i++) {
        const g = arr[i]
        if (!g || typeof g !== 'object') continue
        const row = proxyGroupObjToRow(g as Record<string, unknown>, i)
        if (row) out.push(row)
      }
      return out
    }
    const prependRaw =
      fromObjKey(doc.prepend, 'proxy-groups') ??
      doc.prepend ??
      doc['proxy-groups']
    const appendRaw = fromObjKey(doc.append, 'proxy-groups') ?? doc.append
    const deleteRaw = fromObjKey(doc.delete, 'proxy-groups') ?? doc.delete
    return {
      prepend: parseRows(prependRaw),
      append: parseRows(appendRaw),
      delete: Array.isArray(deleteRaw)
        ? deleteRaw.map((x) => String(x).trim()).filter(Boolean)
        : [],
    }
  } catch {
    return { prepend: [], append: [], delete: [] }
  }
}

export function applyProxyBucketsToMerge(raw: string, b: ProxyBuckets): string {
  const doc = parseMergeDoc(raw)
  if (
    !doc.prepend ||
    typeof doc.prepend !== 'object' ||
    Array.isArray(doc.prepend)
  )
    doc.prepend = {}
  if (
    !doc.append ||
    typeof doc.append !== 'object' ||
    Array.isArray(doc.append)
  )
    doc.append = {}
  if (
    !doc.delete ||
    typeof doc.delete !== 'object' ||
    Array.isArray(doc.delete)
  )
    doc.delete = {}
  ;(doc.prepend as Record<string, unknown>)['proxy-groups'] =
    b.prepend.map(rowToProxyGroupObj)
  ;(doc.append as Record<string, unknown>)['proxy-groups'] =
    b.append.map(rowToProxyGroupObj)
  ;(doc.delete as Record<string, unknown>)['proxy-groups'] = b.delete
  return stringifyMerge(doc)
}

export type RuleRow = {
  id: string
  ruleType: string
  content: string
  policy: string
}

export type RuleBuckets = {
  prepend: RuleRow[]
  append: RuleRow[]
  delete: string[]
}

function ruleRowsFromAny(raw: unknown): RuleRow[] {
  const arr = raw as unknown[] | undefined
  if (!Array.isArray(arr)) return []
  const out: RuleRow[] = []
  for (const line of arr) {
    if (typeof line !== 'string') continue
    const s = line.trim()
    if (!s) continue
    const parts = s.split(',').map((x) => x.trim())
    out.push({
      id: `r-${out.length}-${s.slice(0, 24)}`,
      ruleType: parts[0] ?? 'DOMAIN-SUFFIX',
      content: parts[1] ?? '',
      policy: parts[2] ?? 'DIRECT',
    })
  }
  return out
}

function rowToRuleLine(r: RuleRow): string {
  return `${r.ruleType.trim()},${r.content.trim()},${r.policy.trim()}`
}

export function rulesBucketsFromMerge(raw: string): RuleBuckets {
  const doc = parseMergeDoc(raw)
  const prep = (doc.prepend as Record<string, unknown> | undefined)?.rules
  const app = (doc.append as Record<string, unknown> | undefined)?.rules
  const del = (doc.delete as Record<string, unknown> | undefined)?.rules
  return {
    prepend: ruleRowsFromAny(prep),
    append: ruleRowsFromAny(app),
    delete: Array.isArray(del)
      ? del.map((x) => String(x).trim()).filter(Boolean)
      : [],
  }
}

export function rulesBucketsToAdvancedYaml(b: RuleBuckets): string {
  return YAML.stringify(
    {
      prepend: b.prepend.map(rowToRuleLine),
      append: b.append.map(rowToRuleLine),
      delete: b.delete,
    },
    { lineWidth: 120, indent: 2 },
  )
}

export function rulesBucketsFromAdvancedYaml(raw: string): RuleBuckets {
  try {
    const doc = (YAML.parse(raw) ?? {}) as Record<string, unknown>
    return {
      prepend: ruleRowsFromAny(doc.prepend),
      append: ruleRowsFromAny(doc.append),
      delete: Array.isArray(doc.delete)
        ? doc.delete.map((x) => String(x).trim()).filter(Boolean)
        : [],
    }
  } catch {
    return { prepend: [], append: [], delete: [] }
  }
}

export function applyRulesBucketsToMerge(
  raw: string,
  buckets: RuleBuckets,
): string {
  const doc = parseMergeDoc(raw)
  if (!doc.prepend || typeof doc.prepend !== 'object') doc.prepend = {}
  if (!doc.append || typeof doc.append !== 'object') doc.append = {}
  if (!doc.delete || typeof doc.delete !== 'object') doc.delete = {}
  ;(doc.prepend as Record<string, unknown>).rules =
    buckets.prepend.map(rowToRuleLine)
  ;(doc.append as Record<string, unknown>).rules =
    buckets.append.map(rowToRuleLine)
  ;(doc.delete as Record<string, unknown>).rules = buckets.delete
  return stringifyMerge(doc)
}
