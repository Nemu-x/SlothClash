import { useEffect, useRef, useState } from 'react'
import { useTranslation } from 'react-i18next'

import { ClipboardSetText } from '../../wailsjs/runtime/runtime'
import {
  GetDesyncAutoStatus,
  GetDesyncDiagnosticReport,
  GetDesyncStatus,
  RetryDesyncAuto,
  SetDesyncSettings,
  type DesyncAutoStatus,
  type DesyncSettings,
  type DesyncStatus,
} from '../api/desync'

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
  const [status, setStatus] = useState<DesyncStatus | null>(null)
  const [automatic, setAutomatic] = useState<DesyncAutoStatus | null>(null)
  const [saving, setSaving] = useState(false)
  const [copying, setCopying] = useState(false)
  const [copied, setCopied] = useState(false)
  const [error, setError] = useState('')
  const lifeRef = useRef({
    mounted: true,
    generation: 0,
    revision: 0,
    saving: false,
  })

  useEffect(() => {
    const life = lifeRef.current
    life.mounted = true
    const generation = ++life.generation
    let pending = false
    async function refresh() {
      if (pending || life.saving) return
      pending = true
      const revision = life.revision
      try {
        const [next, auto] = await Promise.all([
          GetDesyncStatus(),
          GetDesyncAutoStatus(),
        ])
        if (
          life.mounted &&
          life.generation === generation &&
          revision === life.revision &&
          !life.saving
        ) {
          setStatus(next)
          setAutomatic(auto)
        }
      } catch (err: unknown) {
        if (life.mounted && life.generation === generation)
          setError(String(err))
      } finally {
        pending = false
      }
    }
    void refresh()
    const timer = setInterval(() => void refresh(), 2500)
    return () => {
      life.generation++
      life.mounted = false
      clearInterval(timer)
    }
  }, [connected, mode, traffic])

  async function save(patch: Partial<DesyncSettings>) {
    if (!status) return
    const life = lifeRef.current
    if (life.saving) return
    life.revision++
    const generation = life.generation
    life.saving = true
    setSaving(true)
    setError('')
    try {
      const next = await SetDesyncSettings({ ...status.settings, ...patch })
      if (life.mounted && life.generation === generation) setStatus(next)
    } catch (err: unknown) {
      if (life.mounted && life.generation === generation) setError(String(err))
    } finally {
      life.saving = false
      if (life.mounted) setSaving(false)
    }
  }

  async function retry() {
    setError('')
    try {
      const next = await RetryDesyncAuto()
      if (lifeRef.current.mounted) setAutomatic(next)
    } catch (err: unknown) {
      if (lifeRef.current.mounted) setError(String(err))
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
      if (lifeRef.current.mounted) setCopied(true)
    } catch (err: unknown) {
      if (lifeRef.current.mounted) setError(String(err))
    } finally {
      if (lifeRef.current.mounted) setCopying(false)
    }
  }

  const enabled = status?.settings.enabled === true
  const ruleMode = mode.toLowerCase() === 'rule'
  const supported = status?.coreSupported === true
  const rows = status
    ? [
        {
          key: 'youtube',
          name: 'YouTube',
          checked: status.settings.youtube,
          supported,
          state: automatic?.youtube,
          patch: { youtube: !status.settings.youtube },
        },
        {
          key: 'discord',
          name: 'Discord',
          checked: status.settings.discord || status.settings.discordUDP,
          supported: supported && status.platform === 'windows',
          state: automatic?.discord,
          patch: {
            discord: !(status.settings.discord || status.settings.discordUDP),
            discordUDP:
              !(status.settings.discord || status.settings.discordUDP) &&
              status.udpCoreSupported,
          },
        },
      ]
    : []

  return (
    <div className="homeCard settingsCardCompact desyncSettingsCard">
      <h3 className="homeCardTitle">{t('desync.title')}</h3>
      <div className="settingsToggleRow">
        <span>{t('desync.enabled')}</span>
        <button
          type="button"
          role="switch"
          aria-checked={enabled}
          aria-label={t('desync.enabled')}
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
        rows.map((row) => {
          const phase =
            !enabled || !row.checked
              ? 'disabled'
              : !connected
                ? 'connect'
                : !ruleMode
                  ? 'rule'
                  : row.state?.phase || 'queued'
          return (
            <div key={row.key}>
              <label className="settingsToggleRow">
                <span>{row.name}</span>
                <input
                  type="checkbox"
                  checked={row.checked}
                  disabled={saving || (!row.supported && !row.checked)}
                  onChange={() => void save(row.patch)}
                />
              </label>
              {enabled && row.checked ? (
                <p
                  className={`small ${phase === 'failed' ? 'error' : 'muted'}`}
                  role="status"
                >
                  {t(`desync.phase.${phase}`, {
                    attempt: row.state?.attempt || 1,
                    total: row.state?.total || 1,
                  })}
                </p>
              ) : null}
              {enabled && row.key === 'discord' && row.checked ? (
                <p className="small muted" role="status">
                  {t('desync.voice')}:{' '}
                  {t(
                    `desync.phase.${
                      !status.settings.discordUDP
                        ? 'disabled'
                        : !connected
                          ? 'connect'
                          : !ruleMode
                            ? 'rule'
                            : traffic !== 'tun'
                              ? 'needs_tun'
                              : automatic?.voice.phase || 'waiting'
                    }`,
                    {
                      attempt: automatic?.voice.attempt || 1,
                      total: automatic?.voice.total || 1,
                    },
                  )}
                </p>
              ) : null}
            </div>
          )
        })
      ) : (
        <p className="small muted">{t('desync.loading')}</p>
      )}
      <div className="row">
        <button
          type="button"
          className="btn"
          disabled={!enabled || !connected || !ruleMode || saving}
          onClick={() => void retry()}
        >
          {t('desync.retry')}
        </button>
        <button
          type="button"
          className="btn ghost"
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
      {error ? (
        <p className="error small" role="alert">
          {error}
        </p>
      ) : null}
    </div>
  )
}
