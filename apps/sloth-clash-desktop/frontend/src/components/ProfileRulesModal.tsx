import { useEffect, useMemo, useState } from 'react'

import {
  GetProfileRulesBaseline,
  SetProfileRulesTemplate,
} from '../api/profile'
import { RULE_TYPE_OPTIONS } from '../constants'
import {
  applyRulesBucketsToMerge,
  parseRuleLine,
  rulesBucketsFromAdvancedYaml,
  rulesBucketsFromMerge,
  rulesBucketsToAdvancedYaml,
  rulesTemplateFromProfile,
  type RuleRow,
} from '../mergeTemplate'
import { MonacoYamlEditor } from '../MonacoYamlEditor'
import { yamlValidationError } from '../utils/yaml'

import type { ProfileModalTarget } from './ProfileMergeModal'

type Mode = 'visual' | 'advanced'
type RuleAppendTarget = 'prepend' | 'append'

export function ProfileRulesModal({
  target,
  profiles,
  proxyGroups,
  onClose,
  onSaved,
  onError,
}: {
  target: ProfileModalTarget | null
  profiles: any[] | undefined
  proxyGroups: any[] | undefined
  onClose: () => void
  onSaved: (banner: string) => void
  onError: (msg: string) => void
}) {
  // Drafts are derived from the active target on first mount.
  // App.tsx passes `key={target?.id}` so a new target triggers a remount and
  // these lazy initializers run again.
  const initialRaw = target ? rulesTemplateFromProfile(profiles, target.id) : ''
  const initialBuckets = useMemo(
    () => rulesBucketsFromMerge(initialRaw),
    [initialRaw],
  )

  const [uiMode, setUiMode] = useState<Mode>('visual')
  const [mergeDraft, setMergeDraft] = useState(initialRaw)
  const [advancedDraft, setAdvancedDraft] = useState(() =>
    rulesBucketsToAdvancedYaml(initialBuckets),
  )
  const [rows, setRows] = useState<RuleRow[]>(initialBuckets.prepend)
  const [appendRows, setAppendRows] = useState<RuleRow[]>(initialBuckets.append)
  const [baseline, setBaseline] = useState<string[] | null>(null)
  const [baselineLoading, setBaselineLoading] = useState(target != null)
  const [baselineError, setBaselineError] = useState<string | null>(null)
  const [deletedBaseline, setDeletedBaseline] = useState<string[]>(
    initialBuckets.delete,
  )
  const [appendTarget, setAppendTarget] = useState<RuleAppendTarget>('prepend')
  const [formType, setFormType] = useState('DOMAIN-SUFFIX')
  const [formContent, setFormContent] = useState('')
  const [formPolicy, setFormPolicy] = useState('DIRECT')

  const policyOptions = useMemo(() => {
    const base = ['DIRECT', 'REJECT', 'REJECT-DROP', 'PASS']
    const groups = (proxyGroups ?? [])
      .map((g: any) => String(g?.name ?? '').trim())
      .filter(Boolean)
    const seen = new Set<string>()
    const out: string[] = []
    for (const v of [...base, ...groups]) {
      if (seen.has(v)) continue
      seen.add(v)
      out.push(v)
    }
    return out
  }, [proxyGroups])

  const advancedYamlErr = useMemo(
    () => yamlValidationError(advancedDraft, true),
    [advancedDraft],
  )

  useEffect(() => {
    if (!target) return
    let cancelled = false
    const id = target.id
    void (async () => {
      try {
        const peek = await GetProfileRulesBaseline(id)
        if (cancelled) return
        if (peek?.lastError) {
          setBaselineError(peek.lastError)
          setBaseline([])
        } else {
          setBaseline(Array.isArray(peek?.rules) ? peek.rules : [])
        }
      } catch (e: any) {
        if (!cancelled) {
          setBaselineError(String(e))
          setBaseline([])
        }
      } finally {
        if (!cancelled) setBaselineLoading(false)
      }
    })()
    return () => {
      cancelled = true
    }
  }, [target])

  if (!target) return null

  const onSwitchToVisual = () => {
    if (advancedYamlErr) {
      onError('Fix rules advanced YAML before switching to Visual mode.')
      return
    }
    setUiMode('visual')
    const buckets =
      uiMode === 'advanced'
        ? rulesBucketsFromAdvancedYaml(advancedDraft)
        : rulesBucketsFromMerge(mergeDraft)
    setRows(buckets.prepend)
    setAppendRows(buckets.append)
    setDeletedBaseline(buckets.delete)
  }

  const addRule = () => {
    const content = formContent.trim()
    const needsContent = formType !== 'MATCH'
    if (needsContent && !content) return
    const row: RuleRow = {
      id: `rl-${Date.now()}`,
      ruleType: formType,
      content: needsContent ? content : '',
      policy: formPolicy.trim() || 'DIRECT',
    }
    const prepNext = appendTarget === 'prepend' ? [row, ...rows] : rows
    const appNext =
      appendTarget === 'append' ? [...appendRows, row] : appendRows
    setRows(prepNext)
    setAppendRows(appNext)
    const buckets = {
      prepend: prepNext,
      append: appNext,
      delete: deletedBaseline,
    }
    setMergeDraft((prev) => applyRulesBucketsToMerge(prev, buckets))
    setAdvancedDraft(rulesBucketsToAdvancedYaml(buckets))
    setFormContent('')
  }

  const removeRow = (rowId: string) => {
    const next = rows.filter((x) => x.id !== rowId)
    setRows(next)
    const buckets = {
      prepend: next,
      append: appendRows,
      delete: deletedBaseline,
    }
    setMergeDraft((prev) => applyRulesBucketsToMerge(prev, buckets))
    setAdvancedDraft(rulesBucketsToAdvancedYaml(buckets))
  }

  const removeAppendRow = (rowId: string) => {
    const next = appendRows.filter((x) => x.id !== rowId)
    setAppendRows(next)
    const buckets = {
      prepend: rows,
      append: next,
      delete: deletedBaseline,
    }
    setMergeDraft((prev) => applyRulesBucketsToMerge(prev, buckets))
    setAdvancedDraft(rulesBucketsToAdvancedYaml(buckets))
  }

  const toggleBaselineDelete = (line: string) => {
    const isDel = deletedBaseline.includes(line)
    const nextDel = isDel
      ? deletedBaseline.filter((x) => x !== line)
      : [...deletedBaseline, line]
    setDeletedBaseline(nextDel)
    const buckets = {
      prepend: rows,
      append: appendRows,
      delete: nextDel,
    }
    setMergeDraft((prev) => applyRulesBucketsToMerge(prev, buckets))
    setAdvancedDraft(rulesBucketsToAdvancedYaml(buckets))
  }

  const save = async () => {
    if (uiMode === 'advanced' && advancedYamlErr) return
    const body =
      uiMode === 'visual'
        ? applyRulesBucketsToMerge(mergeDraft, {
            prepend: rows,
            append: appendRows,
            delete: deletedBaseline,
          })
        : applyRulesBucketsToMerge(
            mergeDraft,
            rulesBucketsFromAdvancedYaml(advancedDraft),
          )
    try {
      await SetProfileRulesTemplate(target.id, body)
      onSaved('Rules saved into merge template.')
    } catch (e: any) {
      onError(String(e))
    }
  }

  return (
    <div
      className="modalOverlay"
      role="presentation"
      style={{ zIndex: 70 }}
      onClick={onClose}
    >
      <div
        className={`modalCard modalCardWide yamlModalCard vergeModal ${
          uiMode === 'advanced' ? 'modalCardFullscreen' : 'modalCardVisualTall'
        }`}
        role="dialog"
        aria-modal="true"
        aria-labelledby="rulesEdTitle"
        onClick={(e) => e.stopPropagation()}
      >
        <div className="vergeModalHead">
          <h3 id="rulesEdTitle" className="modalTitle vergeModalTitle">
            Edit rules
          </h3>
          <div className="vergeToggleRow">
            <button
              type="button"
              className={`btn vergeToggle ${uiMode === 'visual' ? 'primary' : 'ghost'}`}
              onClick={onSwitchToVisual}
            >
              Visual
            </button>
            <button
              type="button"
              className={`btn vergeToggle ${uiMode === 'advanced' ? 'primary' : 'ghost'}`}
              onClick={() => setUiMode('advanced')}
            >
              Advanced (YAML)
            </button>
          </div>
        </div>
        <div
          className={`vergeSplit ${
            uiMode === 'advanced' ? 'vergeSplitAdvanced' : ''
          }`}
        >
          <div className="vergePane">
            {uiMode === 'visual' ? (
              <>
                <label className="field modalField">
                  <span className="fieldLab">Rule type</span>
                  <select
                    className="selectModern"
                    value={formType}
                    onChange={(e) => setFormType(e.target.value)}
                  >
                    {RULE_TYPE_OPTIONS.map((type) => (
                      <option key={type} value={type}>
                        {type}
                      </option>
                    ))}
                  </select>
                </label>
                <label className="field modalField">
                  <span className="fieldLab">Rule content</span>
                  <input
                    className="input"
                    value={formContent}
                    onChange={(e) => setFormContent(e.target.value)}
                    placeholder="google.com"
                  />
                </label>
                <label className="field modalField">
                  <span className="fieldLab">Proxy policy</span>
                  <select
                    className="selectModern"
                    value={formPolicy}
                    onChange={(e) => setFormPolicy(e.target.value)}
                  >
                    {policyOptions.map((opt) => (
                      <option key={opt} value={opt}>
                        {opt}
                      </option>
                    ))}
                  </select>
                </label>
                <div className="vergeRuleActions">
                  <div className="segPill">
                    <button
                      type="button"
                      className={`pillOpt ${appendTarget === 'prepend' ? 'active' : ''}`}
                      onClick={() => setAppendTarget('prepend')}
                    >
                      Prepend
                    </button>
                    <button
                      type="button"
                      className={`pillOpt ${appendTarget === 'append' ? 'active' : ''}`}
                      onClick={() => setAppendTarget('append')}
                    >
                      Append
                    </button>
                  </div>
                  <button
                    type="button"
                    className="btn primary vergeStackBtn"
                    onClick={addRule}
                  >
                    Add rule
                  </button>
                </div>
              </>
            ) : (
              <label className="field modalField">
                <span className="fieldLab">Advanced YAML</span>
                <MonacoYamlEditor
                  className="vergePaneYaml modalMonacoWrap"
                  value={advancedDraft}
                  onChange={setAdvancedDraft}
                  height="52vh"
                />
                {advancedYamlErr ? (
                  <span className="muted small" style={{ color: '#ff6b6b' }}>
                    YAML error: {advancedYamlErr}
                  </span>
                ) : null}
              </label>
            )}
          </div>
          {uiMode === 'visual' ? (
            <div className="vergePane vergePaneList">
              <p className="eyebrow">subscription.rules</p>
              <div className="vergeScrollList">
                {baselineLoading ? (
                  <p className="muted small">Loading subscription rules…</p>
                ) : baselineError ? (
                  <p className="muted small" style={{ color: '#ff6b6b' }}>
                    {baselineError}
                  </p>
                ) : !baseline || baseline.length === 0 ? (
                  <p className="muted small">No subscription rules detected.</p>
                ) : (
                  baseline.map((line, idx) => {
                    const parsed = parseRuleLine(line, idx)
                    const isDeleted = deletedBaseline.includes(line)
                    return (
                      <div
                        key={parsed.id}
                        className={`vergeCard vergeCardReadOnly ${
                          isDeleted ? 'vergeCardDeleted' : ''
                        }`}
                      >
                        <div>
                          <div className="vergeCardTitle">
                            {parsed.ruleType}
                          </div>
                          <div className="muted small">{parsed.content}</div>
                          <div className="muted small">→ {parsed.policy}</div>
                        </div>
                        <button
                          type="button"
                          className="btn ghost vergeTrash"
                          aria-label={isDeleted ? 'Restore' : 'Delete'}
                          title={isDeleted ? 'Restore' : 'Delete'}
                          onClick={() => toggleBaselineDelete(line)}
                        >
                          {isDeleted ? '↺' : '×'}
                        </button>
                      </div>
                    )
                  })
                )}
              </div>
              <p className="eyebrow" style={{ marginTop: 10 }}>
                prepend.rules
              </p>
              <div className="vergeScrollList">
                {rows.length === 0 ? (
                  <p className="muted small">No custom prepend rules.</p>
                ) : (
                  rows.map((r) => (
                    <div key={r.id} className="vergeCard">
                      <div>
                        <div className="vergeCardTitle">{r.ruleType}</div>
                        <div className="muted small">{r.content}</div>
                        <div className="muted small">→ {r.policy}</div>
                      </div>
                      <button
                        type="button"
                        className="btn ghost vergeTrash"
                        aria-label="Remove"
                        onClick={() => removeRow(r.id)}
                      >
                        ×
                      </button>
                    </div>
                  ))
                )}
              </div>
              <p className="eyebrow" style={{ marginTop: 10 }}>
                append.rules
              </p>
              <div className="vergeScrollList">
                {appendRows.length === 0 ? (
                  <p className="muted small">No custom append rules.</p>
                ) : (
                  appendRows.map((r) => (
                    <div key={r.id} className="vergeCard">
                      <div>
                        <div className="vergeCardTitle">{r.ruleType}</div>
                        <div className="muted small">{r.content}</div>
                        <div className="muted small">→ {r.policy}</div>
                      </div>
                      <button
                        type="button"
                        className="btn ghost vergeTrash"
                        aria-label="Remove"
                        onClick={() => removeAppendRow(r.id)}
                      >
                        ×
                      </button>
                    </div>
                  ))
                )}
              </div>
            </div>
          ) : null}
        </div>
        <div className="modalFooter">
          <button type="button" className="btn ghost" onClick={onClose}>
            Cancel
          </button>
          <div className="modalFooterRight">
            <button
              type="button"
              className="btn primary"
              disabled={uiMode === 'advanced' && Boolean(advancedYamlErr)}
              onClick={() => void save()}
            >
              Save
            </button>
          </div>
        </div>
      </div>
    </div>
  )
}
