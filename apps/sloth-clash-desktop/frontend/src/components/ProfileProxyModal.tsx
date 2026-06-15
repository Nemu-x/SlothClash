import { useEffect, useMemo, useState } from 'react'
import { useTranslation } from 'react-i18next'

import {
  GetProfileProxyGroupsBaseline,
  SetProfileProxyTemplate,
} from '../api/profile'
import {
  applyProxyBucketsToMerge,
  proxyBucketsFromAdvancedYaml,
  proxyBucketsFromMerge,
  proxyBucketsToAdvancedYaml,
  proxyGroupObjToRow,
  proxyTemplateFromProfile,
  type ProxyGroupRow,
} from '../mergeTemplate'
import { MonacoYamlEditor } from '../MonacoYamlEditor'
import { yamlValidationError } from '../utils/yaml'

import type { ProfileModalTarget } from './ProfileMergeModal'

type Mode = 'visual' | 'advanced'
type AppendTarget = 'prepend' | 'append'

const GROUP_TYPE_OPTIONS = [
  'select',
  'url-test',
  'fallback',
  'load-balance',
] as const

// One-line read/edit row used in baseline and custom group lists.
function GroupLine({
  name,
  groupType,
  use,
  pos,
  deleted,
  actionLabel,
  actionGlyph,
  onAction,
}: {
  name: string
  groupType: string
  use: string
  pos?: AppendTarget
  deleted?: boolean
  actionLabel: string
  actionGlyph: string
  onAction: () => void
}) {
  const { t } = useTranslation()
  const useLabel = use.trim() || '—'

  return (
    <div className={`ruleRow${deleted ? ' ruleRowDeleted' : ''}`}>
      <span className="ruleTypeChip">{groupType}</span>
      <span className="ruleContent" title={name}>
        {name || '—'}
      </span>
      <span className="rulePolicy" title={useLabel}>
        {t('ui.profiles.proxyModal.useLabel', { value: useLabel })}
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

export function ProfileProxyModal({
  target,
  profiles,
  onClose,
  onSaved,
  onError,
}: {
  target: ProfileModalTarget | null
  profiles: any[] | undefined
  onClose: () => void
  onSaved: (banner: string) => void
  onError: (msg: string) => void
}) {
  const { t } = useTranslation()
  const initialRaw = target ? proxyTemplateFromProfile(profiles, target.id) : ''
  const initialBuckets = useMemo(
    () => proxyBucketsFromMerge(initialRaw),
    [initialRaw],
  )

  const [uiMode, setUiMode] = useState<Mode>('visual')
  const [mergeDraft, setMergeDraft] = useState(initialRaw)
  const [advancedDraft, setAdvancedDraft] = useState(() =>
    proxyBucketsToAdvancedYaml(initialBuckets),
  )
  const [rows, setRows] = useState<ProxyGroupRow[]>(initialBuckets.prepend)
  const [appendRows, setAppendRows] = useState<ProxyGroupRow[]>(
    initialBuckets.append,
  )
  const [deleted, setDeleted] = useState<string[]>(initialBuckets.delete)
  const [baseline, setBaseline] = useState<any[] | null>(null)
  const [baselineLoading, setBaselineLoading] = useState(target != null)
  const [baselineError, setBaselineError] = useState<string | null>(null)
  const [appendTarget, setAppendTarget] = useState<AppendTarget>('prepend')

  const [pgName, setPgName] = useState('')
  const [pgType, setPgType] = useState('url-test')
  const [pgUse, setPgUse] = useState('')
  const [pgUrl, setPgUrl] = useState('http://www.gstatic.com/generate_204')
  const [pgInterval, setPgInterval] = useState('300')
  const [pgTimeout, setPgTimeout] = useState('3000')
  const [pgMaxFailed, setPgMaxFailed] = useState('5')
  const [pgLazy, setPgLazy] = useState(true)

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
        const peek = await GetProfileProxyGroupsBaseline(id)
        if (cancelled) return
        if (peek?.lastError) {
          setBaselineError(peek.lastError)
          setBaseline([])
        } else {
          setBaseline(Array.isArray(peek?.groups) ? (peek.groups as any) : [])
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

  const addGroup = () => {
    const name = pgName.trim()
    if (!name) return
    const row: ProxyGroupRow = {
      id: `pg-${Date.now()}`,
      name,
      type: pgType,
      use: pgUse,
      url: pgUrl.trim() || 'http://www.gstatic.com/generate_204',
      interval: Number(pgInterval || '300'),
      timeout: Number(pgTimeout || '3000'),
      maxFailedTimes: Number(pgMaxFailed || '5'),
      lazy: pgLazy,
    }
    const prepNext = appendTarget === 'prepend' ? [...rows, row] : rows
    const appNext =
      appendTarget === 'append' ? [...appendRows, row] : appendRows
    setRows(prepNext)
    setAppendRows(appNext)
    setMergeDraft((prev) =>
      applyProxyBucketsToMerge(prev, {
        prepend: prepNext,
        append: appNext,
        delete: deleted,
      }),
    )
    setAdvancedDraft(
      proxyBucketsToAdvancedYaml({
        prepend: prepNext,
        append: appNext,
        delete: deleted,
      }),
    )
    setPgName('')
    setPgUse('')
  }

  const removeRow = (rowId: string) => {
    const next = rows.filter((x) => x.id !== rowId)
    setRows(next)
    const buckets = {
      prepend: next,
      append: appendRows,
      delete: deleted,
    }
    setMergeDraft((prev) => applyProxyBucketsToMerge(prev, buckets))
    setAdvancedDraft(proxyBucketsToAdvancedYaml(buckets))
  }

  const removeAppendRow = (rowId: string) => {
    const next = appendRows.filter((x) => x.id !== rowId)
    setAppendRows(next)
    const buckets = {
      prepend: rows,
      append: next,
      delete: deleted,
    }
    setMergeDraft((prev) => applyProxyBucketsToMerge(prev, buckets))
    setAdvancedDraft(proxyBucketsToAdvancedYaml(buckets))
  }

  const toggleBaselineDelete = (name: string) => {
    const isDeleted = deleted.includes(name)
    const nextDel = isDeleted
      ? deleted.filter((x) => x !== name)
      : [...deleted, name]
    setDeleted(nextDel)
    const buckets = {
      prepend: rows,
      append: appendRows,
      delete: nextDel,
    }
    setMergeDraft((prev) => applyProxyBucketsToMerge(prev, buckets))
    setAdvancedDraft(proxyBucketsToAdvancedYaml(buckets))
  }

  const onSwitchToVisual = () => {
    if (advancedYamlErr) {
      onError(t('ui.profiles.proxyModal.fixYamlBeforeVisual'))
      return
    }
    setUiMode('visual')
    const buckets =
      uiMode === 'advanced'
        ? proxyBucketsFromAdvancedYaml(advancedDraft)
        : proxyBucketsFromMerge(mergeDraft)
    setRows(buckets.prepend)
    setAppendRows(buckets.append)
    setDeleted(buckets.delete)
  }

  const save = async () => {
    if (uiMode === 'advanced' && advancedYamlErr) return
    const body =
      uiMode === 'visual'
        ? applyProxyBucketsToMerge(mergeDraft, {
            prepend: rows,
            append: appendRows,
            delete: deleted,
          })
        : applyProxyBucketsToMerge(
            mergeDraft,
            proxyBucketsFromAdvancedYaml(advancedDraft),
          )
    if (body == null) return
    try {
      await SetProfileProxyTemplate(target.id, body)
      onSaved(t('ui.profiles.proxyModal.savedBanner'))
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
        className={`modalCard modalCardWide rulesEditorModal ${
          uiMode === 'advanced' ? 'modalCardFullscreen' : 'modalCardVisualTall'
        }`}
        role="dialog"
        aria-modal="true"
        aria-labelledby="pgTitle"
        onClick={(e) => e.stopPropagation()}
      >
        <div className="vergeModalHead">
          <h3 id="pgTitle" className="modalTitle vergeModalTitle">
            {t('ui.profiles.proxyModal.title')}
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
            <div className="rulesCols proxyEditorCols">
              <div className="rulesCol proxyFormCol">
                <p className="eyebrow">
                  {t('ui.profiles.proxyModal.addGroup')}
                </p>
                <label className="field modalField">
                  <span className="fieldLab">
                    {t('ui.profiles.proxyModal.groupName')}
                  </span>
                  <input
                    className="input"
                    value={pgName}
                    onChange={(e) => setPgName(e.target.value)}
                    placeholder={t(
                      'ui.profiles.proxyModal.groupNamePlaceholder',
                    )}
                    aria-label={t('ui.profiles.proxyModal.groupNameAria')}
                  />
                </label>
                <label className="field modalField">
                  <span className="fieldLab">
                    {t('ui.profiles.proxyModal.groupType')}
                  </span>
                  <select
                    className="selectModern"
                    value={pgType}
                    onChange={(e) => setPgType(e.target.value)}
                    aria-label={t('ui.profiles.proxyModal.groupTypeAria')}
                  >
                    {GROUP_TYPE_OPTIONS.map((type) => (
                      <option key={type} value={type}>
                        {type}
                      </option>
                    ))}
                  </select>
                </label>
                <label className="field modalField">
                  <span className="fieldLab">
                    {t('ui.profiles.proxyModal.useProviders')}
                  </span>
                  <input
                    className="input"
                    value={pgUse}
                    onChange={(e) => setPgUse(e.target.value)}
                    placeholder={t(
                      'ui.profiles.proxyModal.useProvidersPlaceholder',
                    )}
                  />
                </label>
                <label className="field modalField">
                  <span className="fieldLab">
                    {t('ui.profiles.proxyModal.healthcheckUrl')}
                  </span>
                  <input
                    className="input"
                    value={pgUrl}
                    onChange={(e) => setPgUrl(e.target.value)}
                    placeholder={t(
                      'ui.profiles.proxyModal.healthcheckUrlPlaceholder',
                    )}
                  />
                </label>
                <div className="fieldGrid">
                  <label className="field modalField">
                    <span className="fieldLab">
                      {t('ui.profiles.proxyModal.interval')}
                    </span>
                    <input
                      className="input"
                      value={pgInterval}
                      onChange={(e) => setPgInterval(e.target.value)}
                    />
                  </label>
                  <label className="field modalField">
                    <span className="fieldLab">
                      {t('ui.profiles.proxyModal.timeout')}
                    </span>
                    <input
                      className="input"
                      value={pgTimeout}
                      onChange={(e) => setPgTimeout(e.target.value)}
                    />
                  </label>
                </div>
                <div className="fieldGrid">
                  <label className="field modalField">
                    <span className="fieldLab">
                      {t('ui.profiles.proxyModal.maxFailedTimes')}
                    </span>
                    <input
                      className="input"
                      value={pgMaxFailed}
                      onChange={(e) => setPgMaxFailed(e.target.value)}
                    />
                  </label>
                  <label className="field modalField">
                    <span className="fieldLab">
                      {t('ui.profiles.proxyModal.lazy')}
                    </span>
                    <button
                      type="button"
                      className={`trafficKnob ${pgLazy ? 'on' : ''}`}
                      onClick={() => setPgLazy((v) => !v)}
                      aria-label={t('ui.profiles.proxyModal.lazy')}
                    >
                      {pgLazy ? t('common.on') : t('common.off')}
                    </button>
                  </label>
                </div>
                <div className="segPill proxyAddPos">
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
                  className="btn primary proxyAddBtn"
                  onClick={addGroup}
                >
                  {t('common.addShort')}
                </button>
              </div>

              <div className="rulesCol proxyListsCol">
                <div className="proxyListBlock">
                  <p className="eyebrow">
                    {t('ui.profiles.proxyModal.subscriptionSection')}
                  </p>
                  <div className="rulesList">
                    {baselineLoading ? (
                      <p className="muted small">
                        {t('ui.profiles.proxyModal.loadingGroups')}
                      </p>
                    ) : baselineError ? (
                      <p className="muted small rulesYamlErr">
                        {baselineError}
                      </p>
                    ) : !baseline || baseline.length === 0 ? (
                      <p className="muted small">
                        {t('ui.profiles.proxyModal.noGroupsDetected')}
                      </p>
                    ) : (
                      baseline.map((gm, idx) => {
                        const row = proxyGroupObjToRow(gm as any, idx)
                        if (!row) return null
                        const isDeleted = deleted.includes(row.name)
                        return (
                          <GroupLine
                            key={row.id}
                            name={row.name}
                            groupType={row.type}
                            use={row.use}
                            deleted={isDeleted}
                            actionLabel={
                              isDeleted
                                ? t('common.restore')
                                : t('common.delete')
                            }
                            actionGlyph={isDeleted ? '↺' : '×'}
                            onAction={() => toggleBaselineDelete(row.name)}
                          />
                        )
                      })
                    )}
                  </div>
                </div>

                <div className="proxyListBlock">
                  <p className="eyebrow">
                    {t('ui.profiles.proxyModal.prependSection')}
                  </p>
                  <div className="rulesList">
                    {rows.length === 0 ? (
                      <p className="muted small">
                        {t('ui.profiles.proxyModal.noGroupsYet')}
                      </p>
                    ) : (
                      rows.map((r) => (
                        <GroupLine
                          key={r.id}
                          name={r.name}
                          groupType={r.type}
                          use={r.use}
                          pos="prepend"
                          actionLabel={t('common.remove')}
                          actionGlyph="×"
                          onAction={() => removeRow(r.id)}
                        />
                      ))
                    )}
                  </div>
                </div>

                <div className="proxyListBlock">
                  <p className="eyebrow">
                    {t('ui.profiles.proxyModal.appendSection')}
                  </p>
                  <div className="rulesList">
                    {appendRows.length === 0 ? (
                      <p className="muted small">
                        {t('ui.profiles.proxyModal.noAppendGroups')}
                      </p>
                    ) : (
                      appendRows.map((r) => (
                        <GroupLine
                          key={r.id}
                          name={r.name}
                          groupType={r.type}
                          use={r.use}
                          pos="append"
                          actionLabel={t('common.remove')}
                          actionGlyph="×"
                          onAction={() => removeAppendRow(r.id)}
                        />
                      ))
                    )}
                  </div>
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
