import { useEffect, useMemo, useState } from 'react'
import { useTranslation } from 'react-i18next'

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

// One-line read/edit row used in both columns.
function RuleLine({
  ruleType,
  content,
  policy,
  pos,
  deleted,
  actionLabel,
  actionGlyph,
  onAction,
}: {
  ruleType: string
  content: string
  policy: string
  pos?: RuleAppendTarget
  deleted?: boolean
  actionLabel: string
  actionGlyph: string
  onAction: () => void
}) {
  return (
    <div className={`ruleRow${deleted ? ' ruleRowDeleted' : ''}`}>
      <span className="ruleTypeChip">{ruleType}</span>
      <span className="ruleContent" title={content}>
        {content || '—'}
      </span>
      <span className="ruleArrow" aria-hidden>
        →
      </span>
      <span className="rulePolicy" title={policy}>
        {policy}
      </span>
      {pos ? <span className="rulePos">{pos}</span> : null}
      <button
        type="button"
        className="btn ghost ruleRemove"
        aria-label={actionLabel}
        title={actionLabel}
        onClick={onAction}
      >
        {actionGlyph}
      </button>
    </div>
  )
}

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
  const { t } = useTranslation()
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
      onError(t('ui.profiles.rulesModal.fixYamlBeforeVisual'))
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
    const buckets = { prepend: rows, append: next, delete: deletedBaseline }
    setMergeDraft((prev) => applyRulesBucketsToMerge(prev, buckets))
    setAdvancedDraft(rulesBucketsToAdvancedYaml(buckets))
  }

  const toggleBaselineDelete = (line: string) => {
    const isDel = deletedBaseline.includes(line)
    const nextDel = isDel
      ? deletedBaseline.filter((x) => x !== line)
      : [...deletedBaseline, line]
    setDeletedBaseline(nextDel)
    const buckets = { prepend: rows, append: appendRows, delete: nextDel }
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
      onSaved(t('ui.profiles.rulesModal.savedBanner'))
    } catch (e: any) {
      onError(String(e))
    }
  }

  const hasCustom = rows.length > 0 || appendRows.length > 0

  return (
    <div
      className="modalOverlay"
      role="presentation"
      style={{ zIndex: 70 }}
      onClick={onClose}
    >
      <div
        className={`modalCard modalCardWide rulesEditorModal ${
          uiMode === 'advanced' ? 'modalCardFullscreen' : 'modalCardVisualTall'
        }`}
        role="dialog"
        aria-modal="true"
        aria-labelledby="rulesEdTitle"
        onClick={(e) => e.stopPropagation()}
      >
        <div className="vergeModalHead">
          <h3 id="rulesEdTitle" className="modalTitle vergeModalTitle">
            {t('ui.profiles.rulesModal.title')}
          </h3>
          <div className="vergeToggleRow">
            <button
              type="button"
              className={`btn vergeToggle ${uiMode === 'visual' ? 'primary' : 'ghost'}`}
              onClick={onSwitchToVisual}
            >
              {t('common.visual')}
            </button>
            <button
              type="button"
              className={`btn vergeToggle ${uiMode === 'advanced' ? 'primary' : 'ghost'}`}
              onClick={() => setUiMode('advanced')}
            >
              {t('common.advancedYamlTab')}
            </button>
          </div>
        </div>

        {uiMode === 'advanced' ? (
          <label className="field modalField rulesAdvancedField">
            <span className="fieldLab">{t('common.advancedYaml')}</span>
            <MonacoYamlEditor
              className="vergePaneYaml modalMonacoWrap"
              value={advancedDraft}
              onChange={setAdvancedDraft}
              height="56vh"
            />
            {advancedYamlErr ? (
              <span className="rulesYamlErr small">
                {t('common.yamlError', { error: advancedYamlErr })}
              </span>
            ) : null}
          </label>
        ) : (
          <div className="rulesEditorBody">
            <div className="rulesAddBar">
              <select
                className="selectModern selectInline rulesAddType"
                value={formType}
                onChange={(e) => setFormType(e.target.value)}
                aria-label={t('ui.profiles.rulesModal.ruleTypeAria')}
              >
                {RULE_TYPE_OPTIONS.map((type) => (
                  <option key={type} value={type}>
                    {type}
                  </option>
                ))}
              </select>
              <input
                className="input rulesAddContent"
                value={formContent}
                onChange={(e) => setFormContent(e.target.value)}
                placeholder={
                  formType === 'MATCH'
                    ? t('ui.profiles.rulesModal.noContentPlaceholder')
                    : 'google.com'
                }
                disabled={formType === 'MATCH'}
                onKeyDown={(e) => {
                  if (e.key === 'Enter') addRule()
                }}
              />
              <span className="rulesAddArrow" aria-hidden>
                →
              </span>
              <select
                className="selectModern selectInline rulesAddPolicy"
                value={formPolicy}
                onChange={(e) => setFormPolicy(e.target.value)}
                aria-label={t('ui.profiles.rulesModal.policyAria')}
              >
                {policyOptions.map((opt) => (
                  <option key={opt} value={opt}>
                    {opt}
                  </option>
                ))}
              </select>
              <div className="segPill rulesAddPos">
                <button
                  type="button"
                  className={`pillOpt ${appendTarget === 'prepend' ? 'active' : ''}`}
                  onClick={() => setAppendTarget('prepend')}
                >
                  {t('common.prepend')}
                </button>
                <button
                  type="button"
                  className={`pillOpt ${appendTarget === 'append' ? 'active' : ''}`}
                  onClick={() => setAppendTarget('append')}
                >
                  {t('common.append')}
                </button>
              </div>
              <button
                type="button"
                className="btn primary rulesAddBtn"
                onClick={addRule}
              >
                {t('common.addShort')}
              </button>
            </div>

            <div className="rulesCols">
              <div className="rulesCol">
                <p className="eyebrow">
                  {t('ui.profiles.rulesModal.yourRules')}
                </p>
                <div className="rulesList">
                  {!hasCustom ? (
                    <p className="muted small">
                      {t('ui.profiles.rulesModal.noCustomRules')}
                    </p>
                  ) : null}
                  {rows.map((r) => (
                    <RuleLine
                      key={r.id}
                      ruleType={r.ruleType}
                      content={r.content}
                      policy={r.policy}
                      pos="prepend"
                      actionLabel={t('common.remove')}
                      actionGlyph="×"
                      onAction={() => removeRow(r.id)}
                    />
                  ))}
                  {appendRows.map((r) => (
                    <RuleLine
                      key={r.id}
                      ruleType={r.ruleType}
                      content={r.content}
                      policy={r.policy}
                      pos="append"
                      actionLabel={t('common.remove')}
                      actionGlyph="×"
                      onAction={() => removeAppendRow(r.id)}
                    />
                  ))}
                </div>
              </div>

              <div className="rulesCol">
                <p className="eyebrow">
                  {t('ui.profiles.rulesModal.subscriptionReadOnly')}
                </p>
                <div className="rulesList">
                  {baselineLoading ? (
                    <p className="muted small">
                      {t('ui.profiles.rulesModal.loadingRules')}
                    </p>
                  ) : baselineError ? (
                    <p className="muted small rulesYamlErr">{baselineError}</p>
                  ) : !baseline || baseline.length === 0 ? (
                    <p className="muted small">
                      {t('ui.profiles.rulesModal.noRulesDetected')}
                    </p>
                  ) : (
                    baseline.map((line, idx) => {
                      const parsed = parseRuleLine(line, idx)
                      const isDeleted = deletedBaseline.includes(line)
                      return (
                        <RuleLine
                          key={parsed.id}
                          ruleType={parsed.ruleType}
                          content={parsed.content}
                          policy={parsed.policy}
                          deleted={isDeleted}
                          actionLabel={
                            isDeleted ? t('common.restore') : t('common.delete')
                          }
                          actionGlyph={isDeleted ? '↺' : '×'}
                          onAction={() => toggleBaselineDelete(line)}
                        />
                      )
                    })
                  )}
                </div>
              </div>
            </div>
          </div>
        )}

        <div className="modalFooter">
          <button type="button" className="btn ghost" onClick={onClose}>
            {t('common.cancel')}
          </button>
          <div className="modalFooterRight">
            <button
              type="button"
              className="btn primary"
              disabled={uiMode === 'advanced' && Boolean(advancedYamlErr)}
              onClick={() => void save()}
            >
              {t('common.save')}
            </button>
          </div>
        </div>
      </div>
    </div>
  )
}
