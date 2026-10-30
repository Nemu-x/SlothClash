import { useCallback, useEffect, useState } from 'react'
import { useTranslation } from 'react-i18next'

import {
  ForgetCorpVpnCredentials,
  ForgetCorpVpnServercert,
  GetCorpVpnCredentials,
  GetCorpVpnStatus,
  StartCorpVpn,
  StopCorpVpn,
  splitCorpError,
  type main,
} from '../api/corp'
import { EventsOn } from '../api/runtime'
import { CorpCertTrustModal } from '../components/CorpCertTrustModal'

type Status = main.CorpVpnStatus

function errText(e: unknown): string {
  const m = (e as { message?: string })?.message
  return String(m ?? e ?? '').trim() || 'unknown error'
}

function CorpError({ raw }: { raw: string }) {
  const { t } = useTranslation()
  const { code, detail } = splitCorpError(raw)
  if (!code) return <div className="error">{detail}</div>
  return (
    <div className="error">
      <strong>{t(`corp.err.${code}`)}</strong>
      {detail ? <div className="small">{detail}</div> : null}
    </div>
  )
}

export function CorpVpnPage() {
  const { t } = useTranslation()
  const [status, setStatus] = useState<Status | null>(null)
  const [gateway, setGateway] = useState('')
  const [username, setUsername] = useState('')
  const [password, setPassword] = useState('')
  const [busy, setBusy] = useState(false)
  // Live connect step ("preparing" | "driver" | "connecting"), so the button
  // shows progress instead of a frozen "Connecting…" and users don't spam it.
  const [phase, setPhase] = useState('')
  const [error, setError] = useState('')
  // Remember server + username (never the password) between sessions.
  const [remember, setRemember] = useState(true)
  // Pending certificate decision (untrusted on first use, or changed since the
  // pin we trusted). Cleared on reject — the remembered pin is left untouched.
  const [pendingTrust, setPendingTrust] = useState<Status | null>(null)
  // A pin is remembered → offer to forget it (reset trust without hand-editing).
  const [hasSavedPin, setHasSavedPin] = useState(false)

  const refresh = useCallback(() => {
    GetCorpVpnStatus()
      .then((s) => setStatus(s))
      .catch(() => {})
  }, [])

  const loadCredentials = useCallback(() => {
    // Pre-fill the saved server + username (password is never stored).
    GetCorpVpnCredentials()
      .then((c) => {
        const gw = c?.gateway ?? ''
        const un = c?.username ?? ''
        if (gw) setGateway((g) => g || gw)
        if (un) setUsername((u) => u || un)
        setHasSavedPin(!!c?.servercert)
      })
      .catch(() => {})
  }, [])

  useEffect(() => {
    refresh()
    loadCredentials()
  }, [refresh, loadCredentials])

  // While connected, poll so the log view and status stay live.
  useEffect(() => {
    if (!status?.connected) return
    const id = window.setInterval(refresh, 3000)
    return () => window.clearInterval(id)
  }, [status?.connected, refresh])

  // Live connect-progress step from the backend, so the button reflects what's
  // actually happening (preparing → driver → connecting) during the long first
  // connect instead of looking frozen.
  useEffect(() => {
    const off = EventsOn('corp:phase', (p: unknown) =>
      setPhase(String(p ?? '')),
    )
    return () => {
      if (typeof off === 'function') off()
    }
  }, [])

  const connect = useCallback(
    async (servercert: string) => {
      setBusy(true)
      setError('')
      try {
        const s = await StartCorpVpn(
          gateway.trim(),
          username.trim(),
          password,
          servercert,
        )
        setStatus(s)
        // The backend saves server+username on connect; honour "remember" by
        // forgetting them when the box is unticked.
        if (!remember) void ForgetCorpVpnCredentials()
        if (s.needsCertTrust) {
          // Gateway certificate failed verification (new, or changed since we
          // trusted it) — show it and let the user decide explicitly.
          setPendingTrust(s)
        } else {
          setPendingTrust(null)
          if (s.connected) setPassword('') // don't keep the secret around once up
        }
      } catch (e) {
        setPendingTrust(null)
        setError(errText(e))
      } finally {
        setBusy(false)
        setPhase('')
        loadCredentials()
      }
    },
    [gateway, username, password, remember, loadCredentials],
  )

  const forgetPin = useCallback(async () => {
    try {
      await ForgetCorpVpnServercert()
    } catch (e) {
      setError(errText(e))
    }
    loadCredentials()
  }, [loadCredentials])

  const disconnect = useCallback(async () => {
    setBusy(true)
    setError('')
    try {
      await StopCorpVpn()
      setPendingTrust(null)
      refresh()
    } catch (e) {
      setError(errText(e))
    } finally {
      setBusy(false)
    }
  }, [refresh])

  // Platform gate: P1 ships macOS-only. Show a friendly note elsewhere.
  if (status && !status.supported) {
    return (
      <div className="corp panel">
        <div className="corpHeader">
          <h2>{t('corp.title')}</h2>
        </div>
        <p className="muted">{t('corp.unsupported')}</p>
      </div>
    )
  }

  const connected = !!status?.connected
  const fullTunnel = !!status?.fullTunnel

  return (
    <div className="corp panel">
      <div className="corpHeader">
        <h2>{t('corp.title')}</h2>
        <span className={`pill ${connected ? 'corpPillOn' : ''}`}>
          {connected ? t('corp.connected') : t('corp.disconnected')}
        </span>
      </div>

      {error ? <CorpError raw={error} /> : null}

      <CorpCertTrustModal
        gateway={gateway}
        pending={pendingTrust}
        busy={busy}
        onReject={() => setPendingTrust(null)}
        onTrust={(pin) => void connect(pin)}
      />

      <div className="corpGrid">
        <div className="corpMain">
          {connected ? (
            <div className="corpCard">
              <div className="corpStatusRow">
                <span className="muted small">{t('corp.gateway')}</span>
                <strong>
                  {gateway || status?.routes?.length ? gateway : '—'}
                </strong>
              </div>
              {fullTunnel ? (
                <p className="corpWarn small">{t('corp.fullTunnelWarn')}</p>
              ) : (
                <>
                  <div className="corpStatusRow">
                    <span className="muted small">{t('corp.routes')}</span>
                    <div className="corpChips">
                      {(status?.routes ?? []).map((r) => (
                        <span key={r} className="pill">
                          {r}
                        </span>
                      ))}
                    </div>
                  </div>
                  {(status?.dnsDomains ?? []).length > 0 ? (
                    <div className="corpStatusRow">
                      <span className="muted small">
                        {t('corp.dnsDomains')}
                      </span>
                      <div className="corpChips">
                        {(status?.dnsDomains ?? []).map((d) => (
                          <span key={d} className="pill">
                            {d}
                          </span>
                        ))}
                      </div>
                    </div>
                  ) : null}
                </>
              )}
              <div className="corpActions">
                <button
                  type="button"
                  className="btn subtle"
                  disabled={busy}
                  onClick={() => void disconnect()}
                >
                  {t('corp.disconnect')}
                </button>
              </div>
            </div>
          ) : (
            <form
              className="corpCard corpForm"
              onSubmit={(e) => {
                e.preventDefault()
                void connect('')
              }}
            >
              <label className="corpField">
                <span className="muted small">{t('corp.gateway')}</span>
                <input
                  className="input"
                  placeholder="vpn.company.com"
                  autoComplete="off"
                  spellCheck={false}
                  value={gateway}
                  onChange={(e) => setGateway(e.target.value)}
                />
              </label>
              <label className="corpField">
                <span className="muted small">{t('corp.username')}</span>
                <input
                  className="input"
                  autoComplete="username"
                  value={username}
                  onChange={(e) => setUsername(e.target.value)}
                />
              </label>
              <label className="corpField">
                <span className="muted small">{t('corp.password')}</span>
                <input
                  className="input"
                  type="password"
                  autoComplete="current-password"
                  value={password}
                  onChange={(e) => setPassword(e.target.value)}
                />
              </label>
              <label className="corpRemember">
                <input
                  type="checkbox"
                  checked={remember}
                  onChange={(e) => setRemember(e.target.checked)}
                />
                <span className="muted small">{t('corp.remember')}</span>
              </label>
              {hasSavedPin ? (
                <div className="corpRemember">
                  <span className="muted small">{t('corp.cert.saved')}</span>
                  <button
                    type="button"
                    className="btn subtle"
                    disabled={busy}
                    onClick={() => void forgetPin()}
                  >
                    {t('corp.cert.forget')}
                  </button>
                </div>
              ) : null}
              <div className="corpActions">
                <button
                  type="submit"
                  className="btn"
                  disabled={busy || !gateway.trim() || !username.trim()}
                >
                  {busy
                    ? t(phase ? `corp.phase.${phase}` : 'corp.connecting')
                    : t('corp.connect')}
                </button>
              </div>
            </form>
          )}
        </div>

        <aside className="corpAside">
          <h3 className="corpAsideTitle">{t('corp.howTitle')}</h3>
          <ul className="corpHow">
            <li>{t('corp.howCorp')}</li>
            <li>{t('corp.howRest')}</li>
          </ul>
          <p className="muted small corpProtocols">{t('corp.protocols')}</p>
        </aside>
      </div>

      {(status?.logTail ?? []).length > 0 ? (
        <details className="corpCard corpLogs" open={connected}>
          <summary className="muted small">{t('corp.logs')}</summary>
          <pre className="corpLogBody">
            {(status?.logTail ?? []).join('\n')}
          </pre>
        </details>
      ) : null}
    </div>
  )
}
