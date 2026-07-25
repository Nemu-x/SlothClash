import { useCallback, useEffect, useMemo, useState } from 'react'

import {
  GetProfileRulesBaseline,
  SetProfileRulesTemplate,
} from '../api/profile'
import {
  applyRulesBucketsToMerge,
  parseRuleLine,
  rowToRuleLine,
  rulesBucketsFromMerge,
  rulesTemplateFromProfile,
} from '../mergeTemplate'
import type { RuleRow as TableRuleRow } from '../rulesTable'

// A dashboard rule that maps to a disabled baseline line (present in the
// rules-template `delete` bucket, so absent from the live /rules snapshot). It
// is shaped like a table row so it can be rendered alongside live rows.
export type DisabledRuleRow = TableRuleRow & { disabled: true; line: string }

// normalizeRuleKey makes a live /rules row (PascalCase types like "DomainSuffix")
// comparable to a config baseline line (UPPER-KEBAB like "DOMAIN-SUFFIX"): drop
// case, separators and whitespace so "DomainSuffix" == "DOMAIN-SUFFIX" and
// "wazzup24.com" survives while ", "-joined payloads collapse to the same shape.
export function normalizeRuleKey(
  type: string,
  content: string,
  policy: string,
): string {
  const strip = (s: string) =>
    String(s ?? '')
      .toLowerCase()
      .replace(/[-_\s]/g, '')
  return `${strip(type)}|${strip(content)}|${strip(policy)}`
}

// buildBaselineIndex maps each normalized rule key to its exact baseline line,
// or to null when >1 baseline line shares the key. A null (ambiguous) entry is
// never toggled, so the dashboard can never delete the wrong rule. Exported for
// unit testing the matching/ambiguity guarantee.
export function buildBaselineIndex(
  lines: readonly string[],
): Map<string, string | null> {
  const byKey = new Map<string, string | null>()
  for (const line of lines) {
    if (typeof line !== 'string' || !line.trim()) continue
    const parsed = parseRuleLine(line)
    const key = normalizeRuleKey(parsed.ruleType, parsed.content, parsed.policy)
    byKey.set(key, byKey.has(key) ? null : line.trim())
  }
  return byKey
}

type BaselineState = {
  // Raw config-format rule lines from the subscription/extend baseline.
  lines: string[]
  error: string | null
  loading: boolean
}

/**
 * useRuleToggles powers the Rules dashboard enable/disable checkbox. It never
 * invents a rule line: a row is toggleable only when it maps to exactly one
 * baseline line, and toggling writes that exact line to the profile's
 * rules-template `delete` bucket (the same mechanism ProfileRulesModal uses),
 * then saves via SetProfileRulesTemplate — which auto-reconnects the active
 * profile, applying the change live. No backend change is needed.
 */
export function useRuleToggles(args: {
  activeProfileId: string
  profiles: unknown[] | undefined
  enabled: boolean
  onError?: (msg: string) => void
  onApplied?: () => void
}) {
  const { activeProfileId, profiles, enabled, onError, onApplied } = args

  const template = useMemo(
    () =>
      activeProfileId
        ? rulesTemplateFromProfile(profiles, activeProfileId)
        : '',
    [profiles, activeProfileId],
  )

  const deleteLines = useMemo(() => {
    if (!activeProfileId) return new Set<string>()
    return new Set(rulesBucketsFromMerge(template).delete)
  }, [template, activeProfileId])

  const [baseline, setBaseline] = useState<BaselineState>(() => ({
    lines: [],
    error: null,
    loading: false,
  }))
  const [busyLines, setBusyLines] = useState<Set<string>>(() => new Set())

  // Fetch the subscription/extend rule baseline for the active profile.
  useEffect(() => {
    if (!enabled || !activeProfileId) {
      // eslint-disable-next-line @eslint-react/set-state-in-effect
      setBaseline({ lines: [], error: null, loading: false })
      return
    }
    let cancelled = false
    // eslint-disable-next-line @eslint-react/set-state-in-effect
    setBaseline((prev) => ({ ...prev, loading: true, error: null }))
    void (async () => {
      try {
        const peek = await GetProfileRulesBaseline(activeProfileId)
        if (cancelled) return
        if (peek?.lastError) {
          setBaseline({ lines: [], error: peek.lastError, loading: false })
          return
        }
        const lines = Array.isArray(peek?.rules) ? peek.rules : []
        setBaseline({ lines, error: null, loading: false })
      } catch (e: any) {
        if (!cancelled) {
          setBaseline({ lines: [], error: String(e), loading: false })
        }
      }
    })()
    return () => {
      cancelled = true
    }
  }, [enabled, activeProfileId])

  // Toggleable index = subscription/extend baseline lines PLUS the profile's own
  // custom prepend/append rules. Custom rules toggle two-way as well: mihomo's
  // merge applies `delete` AFTER prepend/append, so adding a custom rule's exact
  // line to the delete bucket removes it from the generated config without
  // touching its prepend/append entry — re-enabling just drops it from delete.
  const byKey = useMemo(() => {
    if (!activeProfileId) return new Map<string, string | null>()
    const buckets = rulesBucketsFromMerge(template)
    const customLines = [...buckets.prepend, ...buckets.append].map(
      rowToRuleLine,
    )
    return buildBaselineIndex([...baseline.lines, ...customLines])
  }, [activeProfileId, template, baseline.lines])

  // The exact config line a live row maps to, or null if unmatched/ambiguous.
  const matchLine = useCallback(
    (row: TableRuleRow): string | null => {
      const key = normalizeRuleKey(row.type, row.payload, row.proxy)
      return byKey.get(key) ?? null
    },
    [byKey],
  )

  const isToggleable = useCallback(
    (row: TableRuleRow) => Boolean(activeProfileId) && matchLine(row) !== null,
    [matchLine, activeProfileId],
  )

  // Disabled rules to render alongside live rows so they can be re-enabled:
  // every line in the delete bucket, parsed for display.
  const disabledRows = useMemo<DisabledRuleRow[]>(() => {
    const out: DisabledRuleRow[] = []
    let i = 0
    for (const line of deleteLines) {
      const parsed = parseRuleLine(line, i)
      out.push({
        idx: -1,
        type: parsed.ruleType,
        payload: parsed.content,
        proxy: parsed.policy,
        disabled: true,
        line: line.trim(),
      })
      i++
    }
    return out
  }, [deleteLines])

  const writeTemplate = useCallback(
    async (nextDelete: Set<string>, touchedLine: string) => {
      const buckets = rulesBucketsFromMerge(template)
      const next = applyRulesBucketsToMerge(template, {
        prepend: buckets.prepend,
        append: buckets.append,
        delete: [...nextDelete],
      })
      setBusyLines((prev) => new Set(prev).add(touchedLine))
      try {
        await SetProfileRulesTemplate(activeProfileId, next)
        onApplied?.()
      } catch (e: any) {
        onError?.(String(e))
      } finally {
        setBusyLines((prev) => {
          const s = new Set(prev)
          s.delete(touchedLine)
          return s
        })
      }
    },
    [template, activeProfileId, onApplied, onError],
  )

  // Disable a live rule: add its exact baseline line to the delete bucket.
  const disableRow = useCallback(
    (row: TableRuleRow) => {
      if (!activeProfileId) return
      const line = matchLine(row)
      if (!line) return
      const next = new Set(deleteLines)
      next.add(line)
      void writeTemplate(next, line)
    },
    [activeProfileId, matchLine, deleteLines, writeTemplate],
  )

  // Re-enable a disabled rule: drop its line from the delete bucket.
  const enableLine = useCallback(
    (line: string) => {
      if (!activeProfileId) return
      const next = new Set(deleteLines)
      next.delete(line)
      void writeTemplate(next, line)
    },
    [activeProfileId, deleteLines, writeTemplate],
  )

  return {
    baselineLoading: baseline.loading,
    baselineError: baseline.error,
    hasActiveProfile: Boolean(activeProfileId),
    isToggleable,
    matchLine,
    // Exact baseline lines currently disabled — used to optimistically hide the
    // matching live rows the moment a toggle is applied, before /rules refetches.
    deleteLines,
    disabledRows,
    busyLines,
    disableRow,
    enableLine,
  }
}
