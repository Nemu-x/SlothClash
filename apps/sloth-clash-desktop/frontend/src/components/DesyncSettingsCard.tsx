import { useEffect, useRef, useState } from 'react'
import { useTranslation } from 'react-i18next'

import {
  GetDesyncStatus,
  RunDesyncDiagnostics,
  SetDesyncSettings,
  type DesyncProbeResult,
  type DesyncSettings,
  type DesyncStatus,
} from '../api/desync'

import { DesyncUDPStatusPanel } from './DesyncUDPStatusPanel'

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
  const [ttl, setTTL] = useState('4')
  const [udpTtl, setUdpTtl] = useState('4')
  const [udpRepeats, setUdpRepeats] = useState('1')
  const [saving, setSaving] = useState(false)
  const [probing, setProbing] = useState(false)
  const [error, setError] = useState('')
  const [results, setResults] = useState<DesyncProbeResult[]>([])
  const generationRef = useRef({ value: 0 })

  useEffect(() => {
    const generation = generationRef.current
    const epoch = ++generation.value
    void GetDesyncStatus()
      .then((next) => {
        if (generation.value !== epoch) return
        setStatus(next)
        setTTL(String(next.settings.discordTTL))
        setUdpTtl(String(next.settings.discordUDPTTL))
        setUdpRepeats(String(next.settings.discordUDPRepeats))
      })
      .catch((err: unknown) => {
        if (generation.value === epoch) setError(String(err))
      })
      .finally(() => {
        if (generation.value === epoch) {
          setSaving(false)
          setProbing(false)
        }
      })
    return () => {
      generation.value++
    }
  }, [connected, mode, traffic])

  async function save(patch: Partial<DesyncSettings>) {
    if (!status) return
    const epoch = ++generationRef.current.value
    setSaving(true)
    setProbing(false)
    setResults([])
    setError('')
    try {
      const next = await SetDesyncSettings({ ...status.settings, ...patch })
      if (generationRef.current.value !== epoch) return
      setStatus(next)
      setTTL(String(next.settings.discordTTL))
      setUdpTtl(String(next.settings.discordUDPTTL))
      setUdpRepeats(String(next.settings.discordUDPRepeats))
    } catch (err: unknown) {
      if (generationRef.current.value === epoch) setError(String(err))
    } finally {
      if (generationRef.current.value === epoch) setSaving(false)
    }
  }

  async function probe() {
    const epoch = ++generationRef.current.value
    setProbing(true)
    setResults([])
    setError('')
    try {
      const next = await RunDesyncDiagnostics()
      if (generationRef.current.value === epoch) setResults(next)
    } catch (err: unknown) {
      if (generationRef.current.value === epoch) setError(String(err))
    } finally {
      if (generationRef.current.value === epoch) setProbing(false)
    }
  }

  function saveAdvanced() {
    const value = Number(ttl)
    const udpValue = Number(udpTtl)
    const repeats = Number(udpRepeats)
    if (
      !Number.isInteger(value) ||
      value < 1 ||
      value > 255 ||
      !Number.isInteger(udpValue) ||
      udpValue < 1 ||
      udpValue > 255
    ) {
      setError(t('desync.ttlInvalid'))
      return
    }
    if (!Number.isInteger(repeats) || repeats < 1 || repeats > 16) {
      setError(t('desync.repeatsInvalid'))
      return
    }
    void save({
      discordTTL: value,
      discordUDPTTL: udpValue,
      discordUDPRepeats: repeats,
    })
  }

  const enabled = status?.settings.enabled === true
  const supported = status?.coreSupported === true
  const ruleMode = mode.toLowerCase() === 'rule'
  const udpSupported =
    status?.udpCoreSupported === true && status.platform === 'windows'

  return (
    <div className="homeCard settingsCardCompact desyncSettingsCard">
      <h3 className="homeCardTitle">{t('desync.title')}</h3>
      <p className="small muted">{t('desync.description')}</p>
      {status ? (
        <>
          <div className="settingsToggleRow">
            <span>{t('desync.enabled')}</span>
            <button
              type="button"
              role="switch"
              aria-checked={enabled}
              aria-label={t('desync.enabled')}
              className={`settingsSwitch${enabled ? ' on' : ''}`}
              disabled={saving || (!supported && !enabled)}
              onClick={() => {
                void save({ enabled: !enabled })
              }}
            >
              <span className="settingsSwitchKnob" aria-hidden />
            </button>
          </div>
          {!supported ? (
            <p className="error small">{t('desync.unsupportedCore')}</p>
          ) : null}
          <label className="settingsToggleRow">
            <span>YouTube</span>
            <input
              type="checkbox"
              checked={status.settings.youtube}
              disabled={saving || !supported}
              onChange={(event) => {
                void save({ youtube: event.target.checked })
              }}
            />
          </label>
          <label className="settingsToggleRow">
            <span>Discord</span>
            <input
              type="checkbox"
              checked={status.settings.discord}
              disabled={
                saving ||
                !supported ||
                (status.platform !== 'windows' && !status.settings.discord)
              }
              onChange={(event) => {
                void save({ discord: event.target.checked })
              }}
            />
          </label>
          {status.platform !== 'windows' ? (
            <p className="small muted">{t('desync.windowsOnly')}</p>
          ) : null}
          <label className="settingsToggleRow">
            <span>{t('desync.udpEnabled')}</span>
            <input
              type="checkbox"
              checked={status.settings.discordUDP}
              disabled={
                saving || (!udpSupported && !status.settings.discordUDP)
              }
              onChange={(event) =>
                void save({ discordUDP: event.target.checked })
              }
            />
          </label>
          {enabled && status.settings.discordUDP && traffic !== 'tun' ? (
            <p className="banner small">{t('desync.udpNeedsTun')}</p>
          ) : null}
          <DesyncUDPStatusPanel
            enabled={enabled && status.settings.discordUDP}
            connected={connected}
          />
          <details>
            <summary>{t('desync.advanced')}</summary>
            <form
              onSubmit={(event) => {
                event.preventDefault()
                saveAdvanced()
              }}
            >
              <label className="desyncTTLRow">
                <span>{t('desync.ttl')}</span>
                <input
                  type="number"
                  min={1}
                  max={255}
                  value={ttl}
                  disabled={saving || !supported || !status.settings.discord}
                  onChange={(event) => setTTL(event.target.value)}
                />
              </label>
              <label className="desyncTTLRow">
                <span>{t('desync.udpTTL')}</span>
                <input
                  type="number"
                  min={1}
                  max={255}
                  value={udpTtl}
                  disabled={
                    saving || !udpSupported || !status.settings.discordUDP
                  }
                  onChange={(event) => setUdpTtl(event.target.value)}
                />
              </label>
              <label className="desyncTTLRow">
                <span>{t('desync.udpRepeats')}</span>
                <input
                  type="number"
                  min={1}
                  max={16}
                  value={udpRepeats}
                  disabled={
                    saving || !udpSupported || !status.settings.discordUDP
                  }
                  onChange={(event) => setUdpRepeats(event.target.value)}
                />
              </label>
              <button
                type="submit"
                className="btn ghost"
                disabled={saving || !supported}
              >
                {t('desync.apply')}
              </button>
            </form>
          </details>
          {enabled && !ruleMode ? (
            <p className="banner small">{t('desync.ruleMode')}</p>
          ) : null}
          <p className="small muted">{t('desync.reconnect')}</p>
          <div className="row">
            <button
              type="button"
              className="btn"
              disabled={
                !enabled ||
                !connected ||
                !ruleMode ||
                saving ||
                probing ||
                (!status.settings.youtube && !status.settings.discord)
              }
              onClick={() => {
                void probe()
              }}
            >
              {t(probing ? 'desync.testing' : 'desync.test')}
            </button>
            <button
              type="button"
              className="btn ghost"
              disabled={!enabled || saving}
              onClick={() => {
                void save({ enabled: false })
              }}
            >
              {t('desync.disable')}
            </button>
          </div>
          <p className="small muted">{t('desync.testScope')}</p>
          {results.length > 0 ? (
            <ul className="desyncResults">
              {results.map((result) => (
                <li key={result.url}>
                  <strong>
                    {result.name}:{' '}
                    {result.ok ? `${result.delayMS} ms` : t('desync.failed')}
                  </strong>
                  <span className="small muted">{result.route}</span>
                  {result.error ? (
                    <span className="error small">{result.error}</span>
                  ) : null}
                </li>
              ))}
            </ul>
          ) : null}
        </>
      ) : (
        <p className="small muted">{t('desync.loading')}</p>
      )}
      {error ? (
        <p className="error small" role="alert">
          {error}
        </p>
      ) : null}
    </div>
  )
}
