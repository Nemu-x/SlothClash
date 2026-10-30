import { type ReactNode, useEffect, useId, useRef, useState } from 'react'
import { useTranslation } from 'react-i18next'

import { ClipboardSetText } from '../../wailsjs/runtime/runtime'
import {
  GetDesyncDiagnosticReport,
  ReconnectDiscordUDP,
  type DesyncRuleInput,
  type DesyncSettings,
} from '../api/desync'
import { useDesyncServices } from '../hooks/queries/useDesyncServices'
import { useDesyncStatus } from '../hooks/queries/useDesyncStatus'
import type { DesyncDraft } from '../hooks/useDesyncDraft'
import {
  desyncErrorText,
  DESYNC_RULE_LIMIT,
  desyncCustomViews,
  desyncEnablePatch,
  desyncServiceName,
  desyncServiceViews,
  type DesyncServiceView,
  desyncBuiltinViews,
} from '../utils/desyncStatus'

import { DesyncCapabilities } from './DesyncCapabilities'
import { DesyncRules } from './DesyncRules'
import { DesyncServiceList } from './DesyncServiceList'
import { DesyncStatusChip } from './DesyncStatusChip'
import { DesyncStrategyLine } from './DesyncStrategyLine'

/**
 * - manage: the active profile's controls — master switch (not for a
 *   Desync-only profile, which is always on), "Popular services" (YouTube,
 *   Discord and the catalog) and "My rules" (domains and apps sent through
 *   Desync, the VPN or directly) with live
 *   status and the strategy in use, "What this computer can do", Check
 *   network / Copy diagnostics, and on the Discord row Reconnect voice /
 *   Turn on TUN.
 * - onboarding: choosing what a new Desync-only profile unblocks — the same
 *   rows without the master switch, status or diagnostics (Desync is turned
 *   on when the profile is created).
 */
export type DesyncPanelVariant = 'manage' | 'onboarding'

/**
 * The Desync controls, laid out for the Desync modal: a scrolling body and a
 * modal footer. Every switch saves at once into the active profile, or, with
 * `draft` (onboarding), only changes the draft.
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
  draft,
  onEnableTun,
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
  /** Edit this draft instead of the active profile's settings. */
  draft?: DesyncDraft
  /**
   * Manage: switch traffic to TUN the way Home's toggle does (Discord voice
   * needs it). Resolves to an error text, empty on success. Without it the
   * Turn on TUN button is not shown.
   */
  onEnableTun?: () => Promise<string | void>
}) {
  const { t } = useTranslation()
  const baseId = useId()
  const live = useDesyncStatus({ connected, mode, traffic })
  const { error: pollError, retry: retryAuto } = live
  const {
    save: saveSettings,
    addRule,
    updateRule,
    addService,
    removeCustom,
    setCustomEnabled,
  } = draft ?? live
  // A draft is shown with the live status' platform and core support.
  const snapshot =
    draft && live.snapshot
      ? draft.settings
        ? {
            ...live.snapshot,
            status: { ...live.snapshot.status, settings: draft.settings },
          }
        : null
      : live.snapshot
  const catalog = useDesyncServices()
  const status = snapshot?.status ?? null
  const [saving, setSaving] = useState(false)
  const [customError, setCustomError] = useState('')
  const [serviceError, setServiceError] = useState<{
    id: string
    text: string
  } | null>(null)
  const [replaced, setReplaced] = useState<{
    id: string
    domains: string[]
  } | null>(null)
  const [copying, setCopying] = useState(false)
  const [copied, setCopied] = useState(false)
  const [tunBusy, setTunBusy] = useState(false)
  const [reconnecting, setReconnecting] = useState(false)
  /** Voice flows the last Reconnect voice closed; null before one. */
  const [reconnected, setReconnected] = useState<number | null>(null)
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
      if (mountedRef.current) setError(desyncErrorText(t, err))
    } finally {
      if (mountedRef.current) setSaving(false)
    }
  }

  const serviceName = (id: string) => desyncServiceName(catalog, id)

  /**
   * One custom-list write. A rule write's validation error shows under the
   * form (worded for a process name with `forProcess`), a service switch's
   * (`service` = its catalog ID) under the Popular list.
   */
  async function writeCustom(
    call: () => Promise<boolean>,
    service?: string,
    forProcess = false,
  ): Promise<boolean> {
    if (!status || busy) return false
    setSaving(true)
    setCustomError('')
    setServiceError(null)
    setReplaced(null)
    try {
      return await call()
    } catch (err: unknown) {
      if (mountedRef.current) {
        const text = desyncErrorText(t, err, {
          serviceName,
          forService: service !== undefined,
          forProcess,
        })
        if (service !== undefined) setServiceError({ id: service, text })
        else setCustomError(text)
      }
      return false
    } finally {
      if (mountedRef.current) setSaving(false)
    }
  }

  /**
   * Service switch: ON adds the service the first time, then re-enables its
   * entry; OFF only disables it (the entry and its cached strategy stay).
   */
  function toggleService(view: DesyncServiceView) {
    void writeCustom(async () => {
      if (view.entryId) return setCustomEnabled(view.entryId, !view.checked)
      const result = await addService(view.id)
      if (result === false) return false
      if (result.length > 0 && mountedRef.current)
        setReplaced({ id: view.id, domains: result })
      return true
    }, view.id)
  }

  /** Add a rule, or replace rule `editing`. */
  async function submitRule(rule: DesyncRuleInput, editing: string | null) {
    const ok = await writeCustom(
      () => (editing ? updateRule(editing, rule) : addRule(rule)),
      undefined,
      rule.kind === 'process',
    )
    return ok && mountedRef.current
  }

  async function retry() {
    setError('')
    try {
      await retryAuto()
    } catch (err: unknown) {
      if (mountedRef.current) setError(String(err))
    }
  }

  async function enableTun() {
    if (!onEnableTun) return
    setTunBusy(true)
    setError('')
    try {
      const failure = await onEnableTun()
      if (failure && mountedRef.current) setError(failure)
    } catch (err: unknown) {
      if (mountedRef.current) setError(String(err))
    } finally {
      if (mountedRef.current) setTunBusy(false)
    }
  }

  async function reconnectVoice() {
    setReconnecting(true)
    setReconnected(null)
    setError('')
    try {
      const closed = await ReconnectDiscordUDP()
      if (mountedRef.current) setReconnected(Number(closed) || 0)
    } catch (err: unknown) {
      if (mountedRef.current) setError(String(err))
    } finally {
      if (mountedRef.current) setReconnecting(false)
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
    ? desyncBuiltinViews(snapshot, { connected, mode, traffic })
    : []
  const customRows = snapshot
    ? desyncCustomViews(snapshot, { connected, mode, traffic })
    : []
  const serviceRows = snapshot
    ? desyncServiceViews(snapshot, { connected, mode, traffic }, catalog)
    : []
  // The whole list's cap (desyncRuleLimit); the backend refuses a rule or
  // service past the cap on entries through Desync with its own error.
  const atLimit = (status?.settings.custom?.length ?? 0) >= DESYNC_RULE_LIMIT
  const shownError = outerError || error || pollError
  const servicesTitleId = `${baseId}-services`
  const customTitleId = `${baseId}-custom`
  // Without a VPN a site Desync cannot unblock stays blocked.
  const ipHint = t(
    status?.desyncOnly ? 'desync.custom.ipHintDirect' : 'desync.custom.ipHint',
  )
  const customErrorId = `${baseId}-custom-error`

  /**
   * Under the Discord voice status: Turn on TUN while voice waits for it
   * (the same switch as Home's traffic toggle), Reconnect voice while voice
   * runs through Desync (connected, Rule mode, TUN).
   */
  function voiceActions(phase: string, tone: string) {
    const needsTun = phase === 'needs_tun' && onEnableTun !== undefined
    const canReconnect =
      tone !== 'off' && connected && ruleMode && traffic === 'tun'
    if (!needsTun && !canReconnect) return null
    return (
      <div className="desyncRowActions">
        {needsTun ? (
          <button
            type="button"
            className="btn btnCompact"
            disabled={tunBusy || locked}
            aria-busy={tunBusy || undefined}
            onClick={() => void enableTun()}
          >
            {t(tunBusy ? 'desync.enablingTun' : 'desync.enableTun')}
          </button>
        ) : null}
        {canReconnect ? (
          <>
            <button
              type="button"
              className="linkBtn"
              disabled={reconnecting || locked}
              onClick={() => void reconnectVoice()}
            >
              {t(
                reconnecting ? 'desync.reconnecting' : 'desync.reconnectVoice',
              )}
            </button>
            {reconnected !== null && !reconnecting ? (
              <span className="desyncRowActionNote" role="status">
                {t(
                  reconnected > 0
                    ? 'desync.reconnected'
                    : 'desync.reconnectedNone',
                )}
              </span>
            ) : null}
          </>
        ) : null}
      </div>
    )
  }

  return (
    <div className="desyncPanel" data-variant={variant}>
      <div className="desyncPanelBody">
        {/* A Desync-only profile is always on: no master switch. */}
        {onboarding || status?.desyncOnly ? null : (
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
              {t('desync.services.title')}
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
                        <DesyncStrategyLine view={row.status} />
                        {row.voice ? (
                          <>
                            <DesyncStatusChip
                              view={row.voice}
                              prefix={t('desync.voice')}
                              sub
                            />
                            <DesyncStrategyLine view={row.voice} voice />
                            {voiceActions(row.voice.phase, row.voice.tone)}
                          </>
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
            {serviceRows.length > 0 ? (
              <DesyncServiceList
                views={serviceRows}
                labelledBy={servicesTitleId}
                onboarding={onboarding}
                busy={busy}
                supported={supported}
                ipHint={ipHint}
                replaced={replaced}
                error={serviceError}
                onToggle={toggleService}
              />
            ) : null}
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
              {t(
                onboarding || status.desyncOnly
                  ? 'desync.custom.hintDesyncOnly'
                  : 'desync.custom.hint',
              )}
            </p>
            <DesyncRules
              rows={customRows}
              onboarding={onboarding}
              busy={busy}
              supported={supported}
              allowVPN={!onboarding && status.desyncOnly !== true}
              atLimit={atLimit}
              limit={DESYNC_RULE_LIMIT}
              ipHint={ipHint}
              error={customError}
              errorId={customErrorId}
              onSubmit={submitRule}
              onToggle={(id, on) =>
                void writeCustom(() => setCustomEnabled(id, on))
              }
              onRemove={(id) => void writeCustom(() => removeCustom(id))}
              onClearError={() => setCustomError('')}
            />
          </section>
        ) : null}
        {children}
        {onboarding || !status ? null : <DesyncCapabilities />}
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
              {t('desync.checkNetwork')}
            </button>
          )}
        </div>
      </div>
    </div>
  )
}
