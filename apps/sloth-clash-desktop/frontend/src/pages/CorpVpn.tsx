import { useCallback, useEffect, useState } from 'react'
import { useTranslation } from 'react-i18next'

import {
  GetCorpVpnStatus,
  StartCorpVpn,
  StopCorpVpn,
  type main,
} from '../api/corp'

type Status = main.CorpVpnStatus

function errText(e: unknown): string {
  const m = (e as { message?: string })?.message
  return String(m ?? e ?? '').trim() || 'unknown error'
}

export function CorpVpnPage() {
  const { t } = useTranslation()
  const [status, setStatus] = useState<Status | null>(null)
  const [gateway, setGateway] = useState('')
  const [username, setUsername] = useState('')
  const [password, setPassword] = useState('')
  const [busy, setBusy] = useState(false)
  const [error, setError] = useState('')
  // Pending server-cert to trust (first connect against an untrusted gateway).
  const [pendingCert, setPendingCert] = useState('')

  const refresh = useCallback(() => {
    GetCorpVpnStatus()
      .then((s) => setStatus(s))
      .catch(() => {})
  }, [])

  useEffect(() => {
    refresh()
  }, [refresh])

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
        if (s.needsCertTrust) {
          // Gateway presented an untrusted cert — surface its fingerprint and
          // let the user accept it (trust-on-first-use), then reconnect pinned.
          setPendingCert(s.servercertSha256)
        } else {
          setPendingCert('')
          if (s.connected) setPassword('') // don't keep the secret around once up
        }
      } catch (e) {
        setError(errText(e))
      } finally {
        setBusy(false)
      }
    },
    [gateway, username, password],
  )

  const disconnect = useCallback(async () => {
    setBusy(true)
    setError('')
    try {
      await StopCorpVpn()
      setPendingCert('')
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
      <p className="muted small">{t('corp.subtitle')}</p>

      {error ? <div className="error">{error}</div> : null}

      {/* Cert-trust step: shown after a first connect against an untrusted cert. */}
      {pendingCert ? (
        <div className="corpCard corpCert">
          <h3>{t('corp.certTitle')}</h3>
          <p className="muted small">{t('corp.certBody')}</p>
          <code className="corpFingerprint">{pendingCert}</code>
          <div className="corpActions">
            <button
              type="button"
              className="btn"
              disabled={busy}
              onClick={() => void connect(pendingCert)}
            >
              {t('corp.trustConnect')}
            </button>
            <button
              type="button"
              className="btn subtle"
              disabled={busy}
              onClick={() => setPendingCert('')}
            >
              {t('corp.cancel')}
            </button>
          </div>
        </div>
      ) : connected ? (
        <div className="corpCard">
          <div className="corpStatusRow">
            <span className="muted small">{t('corp.gateway')}</span>
            <strong>{gateway || status?.routes?.length ? gateway : '—'}</strong>
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
                  <span className="muted small">{t('corp.dnsDomains')}</span>
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
          <div className="corpActions">
            <button
              type="submit"
              className="btn"
              disabled={busy || !gateway.trim() || !username.trim()}
            >
              {busy ? t('corp.connecting') : t('corp.connect')}
            </button>
          </div>
        </form>
      )}
    </div>
  )
}
