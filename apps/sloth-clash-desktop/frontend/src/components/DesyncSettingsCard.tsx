import { useEffect, useRef, useState } from 'react'
import { useTranslation } from 'react-i18next'

import { ClipboardSetText } from '../../wailsjs/runtime/runtime'
import { GetDesyncDiagnosticReport, type DesyncSettings } from '../api/desync'
import { useDesyncStatus } from '../hooks/queries/useDesyncStatus'
import { desyncServiceViews, type DesyncPhaseView } from '../utils/desyncStatus'

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

export function DesyncSettingsCard({
  connected,
  mode,
  traffic,
}: {
  connected: boolean
  mode: string
  traffic: string
}) {
  const { t } = useTranslation()
  const {
    snapshot,
    error: pollError,
    save: saveSettings,
    retry: retryAuto,
  } = useDesyncStatus({ connected, mode, traffic })
  const status = snapshot?.status ?? null
  const [saving, setSaving] = useState(false)
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

  async function save(patch: Partial<DesyncSettings>) {
    if (!status || saving) return
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
  const shownError = error || pollError

  return (
    <div className="homeCard settingsCardCompact desyncSettingsCard">
      <div className="desyncHead">
        <h3 className="homeCardTitle">{t('desync.title')}</h3>
        <button
          type="button"
          role="switch"
          aria-checked={enabled}
          aria-label={t('desync.enabled')}
          title={t('desync.enabled')}
          className={`settingsSwitch${enabled ? ' on' : ''}`}
          disabled={saving || !status || (!supported && !enabled)}
          onClick={() =>
            void save({
              enabled: !enabled,
              ...(!enabled &&
              status &&
              (status.settings.discord || status.settings.discordUDP)
                ? { discord: true, discordUDP: status.udpCoreSupported }
                : {}),
            })
          }
        >
          <span className="settingsSwitchKnob" aria-hidden />
        </button>
      </div>
      {!supported && status ? (
        <p className="error small">{t('desync.unsupportedCore')}</p>
      ) : null}
      {status ? (
        <ul className="desyncServices">
          {rows.map((row) => (
            <li key={row.key} className="desyncService" data-service={row.key}>
              <span className="desyncServiceName">{row.name}</span>
              <div className="desyncServiceStatus">
                <DesyncStatusChip view={row.status} />
                {row.voice ? (
                  <DesyncStatusChip
                    view={row.voice}
                    prefix={t('desync.voice')}
                    sub
                  />
                ) : null}
              </div>
              <button
                type="button"
                role="switch"
                aria-checked={row.checked}
                aria-label={row.name}
                className={`settingsSwitch${row.checked ? ' on' : ''}`}
                disabled={saving || (!row.supported && !row.checked)}
                onClick={() => void save(row.patch)}
              >
                <span className="settingsSwitchKnob" aria-hidden />
              </button>
            </li>
          ))}
        </ul>
      ) : (
        <p className="small muted">{t('desync.loading')}</p>
      )}
      <div className="desyncActions">
        <button
          type="button"
          className="btn primary"
          disabled={!enabled || !connected || !ruleMode || saving}
          onClick={() => void retry()}
        >
          {t('desync.retry')}
        </button>
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
      {shownError ? (
        <p className="error small" role="alert">
          {shownError}
        </p>
      ) : null}
    </div>
  )
}
