import { type ReactNode, useEffect, useId, useRef, useState } from 'react'
import { useTranslation } from 'react-i18next'

import { ClipboardSetText } from '../../wailsjs/runtime/runtime'
import { GetDesyncDiagnosticReport, type DesyncSettings } from '../api/desync'
import { useDesyncStatus } from '../hooks/queries/useDesyncStatus'
import {
  customDomainErrorText,
  DESYNC_CUSTOM_LIMIT,
  desyncCustomViews,
  desyncEnablePatch,
  desyncServiceViews,
  type DesyncPhaseView,
} from '../utils/desyncStatus'

/** Inline status chip: a toned dot plus the phase text. */
export function DesyncStatusChip({
  view,
  prefix,
  sub,
}: {
  view: DesyncPhaseView
  prefix?: string
  sub?: boolean
}) {
  const { t } = useTranslation()
  const text = t(`desync.phase.${view.phase}`, {
    attempt: view.attempt,
    total: view.total,
  })
  return (
    <span
      className={`desyncChip${sub ? ' desyncChipSub' : ''}`}
      data-tone={view.tone}
      data-phase={view.phase}
      role="status"
    >
      <span className="desyncChipDot" aria-hidden />
      <span className="desyncChipText">
        {prefix ? `${prefix}: ${text}` : text}
      </span>
    </span>
  )
}

/**
 * - manage: the full controls — master switch, service rows and custom
 *   domains with live status, Check again / Copy diagnostics.
 * - onboarding: choosing what a new Desync-only profile unblocks — the same
 *   rows without the master switch, status or diagnostics (Desync is turned
 *   on when the profile is created).
 */
export type DesyncPanelVariant = 'manage' | 'onboarding'

/**
 * The Desync controls, laid out for the Desync modal: a scrolling body and a
 * modal footer. Every switch saves at once (the settings are global prefs).
 */
export function DesyncPanel({
  connected,
  mode,
  traffic,
  variant = 'manage',
  locked = false,
  error: outerError = '',
  actions,
  footerNote,
  children,
  onSavingChange,
}: {
  connected: boolean
  mode: string
  traffic: string
  variant?: DesyncPanelVariant
  /** The dialog is busy (creating a profile): every control is disabled. */
  locked?: boolean
  /** The dialog's own error, shown with the panel's above the footer. */
  error?: string
  /** Right side of the footer: the dialog's own buttons. */
  actions?: ReactNode
  /** Left side of the footer in onboarding (manage has diagnostics there). */
  footerNote?: ReactNode
  /** Extra body content after the custom domains (e.g. a name field). */
  children?: ReactNode
  /** A settings write started / finished. */
  onSavingChange?: (saving: boolean) => void
}) {
  const { t } = useTranslation()
  const baseId = useId()
  const {
    snapshot,
    error: pollError,
    save: saveSettings,
    addCustom,
    removeCustom,
    setCustomEnabled,
    retry: retryAuto,
  } = useDesyncStatus({ connected, mode, traffic })
  const status = snapshot?.status ?? null
  const [saving, setSaving] = useState(false)
  const [domainInput, setDomainInput] = useState('')
  const [customError, setCustomError] = useState('')
  const [copying, setCopying] = useState(false)
  const [copied, setCopied] = useState(false)
  const [error, setError] = useState('')
  const mountedRef = useRef(true)
  useEffect(() => {
    mountedRef.current = true
    return () => {
      mountedRef.current = false
    }
  }, [])
  const savingChangeRef = useRef(onSavingChange)
  useEffect(() => {
    savingChangeRef.current = onSavingChange
  })
  useEffect(() => {
    savingChangeRef.current?.(saving)
  }, [saving])

  const onboarding = variant === 'onboarding'
  const busy = saving || locked

  async function save(patch: Partial<DesyncSettings>) {
    if (!status || busy) return
    setSaving(true)
    setError('')
    try {
      await saveSettings({ ...status.settings, ...patch })
    } catch (err: unknown) {
      if (mountedRef.current) setError(String(err))
    } finally {
      if (mountedRef.current) setSaving(false)
    }
  }

  /** One custom-list write; its validation error shows under the field. */
  async function writeCustom(call: () => Promise<boolean>): Promise<boolean> {
    if (!status || busy) return false
    setSaving(true)
    setCustomError('')
    try {
      return await call()
    } catch (err: unknown) {
      if (mountedRef.current) setCustomError(customDomainErrorText(t, err))
      return false
    } finally {
      if (mountedRef.current) setSaving(false)
    }
  }

  async function submitDomain() {
    const domain = domainInput.trim()
    if (!domain) return
    if ((await writeCustom(() => addCustom(domain))) && mountedRef.current)
      setDomainInput('')
  }

  async function retry() {
    setError('')
    try {
      await retryAuto()
    } catch (err: unknown) {
      if (mountedRef.current) setError(String(err))
    }
  }

  async function copyReport() {
    setCopying(true)
    setCopied(false)
    setError('')
    try {
      const text = await GetDesyncDiagnosticReport()
      if (!(await ClipboardSetText(text)))
        throw new Error(t('desync.copyFailed'))
      if (mountedRef.current) setCopied(true)
    } catch (err: unknown) {
      if (mountedRef.current) setError(String(err))
    } finally {
      if (mountedRef.current) setCopying(false)
    }
  }

  const enabled = status?.settings.enabled === true
  const ruleMode = mode.toLowerCase() === 'rule'
  const supported = status?.coreSupported === true
  const rows = snapshot
    ? desyncServiceViews(snapshot, { connected, mode, traffic })
    : []
  const customRows = snapshot
    ? desyncCustomViews(snapshot, { connected, mode, traffic })
    : []
  const atLimit = customRows.length >= DESYNC_CUSTOM_LIMIT
  const shownError = outerError || error || pollError
  const servicesTitleId = `${baseId}-services`
  const customTitleId = `${baseId}-custom`
  const customErrorId = `${baseId}-custom-error`

  return (
    <div className="desyncPanel" data-variant={variant}>
      <div className="desyncPanelBody">
        {onboarding ? null : (
          <div className="desyncMaster">
            <span className="desyncMasterLabel">{t('desync.enabled')}</span>
            <button
              type="button"
              role="switch"
              aria-checked={enabled}
              aria-label={t('desync.enabled')}
              className={`settingsSwitch${enabled ? ' on' : ''}`}
              disabled={busy || !status || (!supported && !enabled)}
              onClick={() =>
                void save(
                  enabled || !status
                    ? { enabled: false }
                    : desyncEnablePatch(status),
                )
              }
            >
              <span className="settingsSwitchKnob" aria-hidden />
            </button>
          </div>
        )}
        {!supported && status ? (
          <p className="error small">{t('desync.unsupportedCore')}</p>
        ) : null}
        {status ? (
          <section className="desyncSection" aria-labelledby={servicesTitleId}>
            <h4 id={servicesTitleId} className="desyncSectionTitle">
              {t('desync.servicesTitle')}
            </h4>
            <ul className="desyncServices">
              {rows.map((row) => (
                <li
                  key={row.key}
                  className="desyncService"
                  data-service={row.key}
                >
                  <span className="desyncServiceName">{row.name}</span>
                  <div className="desyncServiceStatus">
                    {onboarding ? (
                      <span className="desyncServiceHint">
                        {t(`desync.onboarding.hint.${row.key}`)}
                      </span>
                    ) : (
                      <>
                        <DesyncStatusChip view={row.status} />
                        {row.voice ? (
                          <DesyncStatusChip
                            view={row.voice}
                            prefix={t('desync.voice')}
                            sub
                          />
                        ) : null}
                      </>
                    )}
                  </div>
                  <button
                    type="button"
                    role="switch"
                    aria-checked={row.checked}
                    aria-label={row.name}
                    className={`settingsSwitch${row.checked ? ' on' : ''}`}
                    disabled={busy || (!row.supported && !row.checked)}
                    onClick={() => void save(row.patch)}
                  >
                    <span className="settingsSwitchKnob" aria-hidden />
                  </button>
                </li>
              ))}
            </ul>
          </section>
        ) : (
          <p className="small muted desyncLoading">{t('desync.loading')}</p>
        )}
        {status ? (
          <section
            className="desyncSection desyncCustom"
            aria-labelledby={customTitleId}
          >
            <h4 id={customTitleId} className="desyncSectionTitle">
              {t('desync.custom.title')}
            </h4>
            <p className="small muted desyncCustomHint">
              {t('desync.custom.hint')}
            </p>
            {customRows.length > 0 ? (
              <ul className="desyncServices desyncCustomList">
                {customRows.map((row) => (
                  <li
                    key={row.id}
                    className="desyncService desyncCustomRow"
                    data-custom={row.id}
                  >
                    <span
                      className="desyncServiceName desyncCustomDomain"
                      title={row.domain}
                    >
                      {row.domain}
                    </span>
                    <div className="desyncServiceStatus">
                      {onboarding ? null : (
                        <>
                          <DesyncStatusChip view={row.status} />
                          {row.status.phase === 'unavailable' ? (
                            <span className="desyncCustomNote">
                              {t('desync.custom.ipHint')}
                            </span>
                          ) : null}
                        </>
                      )}
                    </div>
                    <div className="desyncCustomControls">
                      <button
                        type="button"
                        role="switch"
                        aria-checked={row.checked}
                        aria-label={row.domain}
                        className={`settingsSwitch${row.checked ? ' on' : ''}`}
                        disabled={busy || (!supported && !row.checked)}
                        onClick={() =>
                          void writeCustom(() =>
                            setCustomEnabled(row.id, !row.checked),
                          )
                        }
                      >
                        <span className="settingsSwitchKnob" aria-hidden />
                      </button>
                      <button
                        type="button"
                        className="desyncCustomRemove"
                        aria-label={t('desync.custom.remove', {
                          domain: row.domain,
                        })}
                        title={t('desync.custom.remove', {
                          domain: row.domain,
                        })}
                        disabled={busy}
                        onClick={() =>
                          void writeCustom(() => removeCustom(row.id))
                        }
                      >
                        <span aria-hidden>×</span>
                      </button>
                    </div>
                  </li>
                ))}
              </ul>
            ) : null}
            <form
              className="desyncCustomAdd"
              onSubmit={(e) => {
                e.preventDefault()
                void submitDomain()
              }}
            >
              <input
                className="input"
                type="text"
                value={domainInput}
                onChange={(e) => {
                  setDomainInput(e.target.value)
                  setCustomError('')
                }}
                placeholder="example.com"
                aria-label={t('desync.custom.inputLabel')}
                aria-invalid={customError ? true : undefined}
                aria-describedby={customError ? customErrorId : undefined}
                disabled={busy || atLimit || !supported}
                autoComplete="off"
                autoCapitalize="off"
                spellCheck={false}
              />
              <button
                type="submit"
                className="btn"
                disabled={busy || atLimit || !supported || !domainInput.trim()}
              >
                {t('desync.custom.add')}
              </button>
            </form>
            {customError ? (
              <p id={customErrorId} className="error small" role="alert">
                {customError}
              </p>
            ) : atLimit ? (
              <p className="small muted">
                {t('desync.custom.limitReached', {
                  limit: DESYNC_CUSTOM_LIMIT,
                })}
              </p>
            ) : null}
          </section>
        ) : null}
        {children}
      </div>
      {shownError ? (
        <p className="error small desyncPanelError" role="alert">
          {shownError}
        </p>
      ) : null}
      <div className="modalFooter desyncPanelFooter">
        {onboarding ? (
          <div className="modalFooterLeft">{footerNote}</div>
        ) : (
          <div className="modalFooterLeft">
            <button
              type="button"
              className="linkBtn"
              disabled={copying}
              onClick={() => void copyReport()}
            >
              {t(
                copied
                  ? 'desync.copied'
                  : copying
                    ? 'desync.copying'
                    : 'desync.copy',
              )}
            </button>
          </div>
        )}
        <div className="modalFooterRight">
          {actions}
          {onboarding ? null : (
            <button
              type="button"
              className="btn primary"
              disabled={!enabled || !connected || !ruleMode || busy}
              onClick={() => void retry()}
            >
              {t('desync.retry')}
            </button>
          )}
        </div>
      </div>
    </div>
  )
}
