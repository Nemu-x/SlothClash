import { type ReactNode, useEffect } from 'react'
import { useTranslation } from 'react-i18next'

import type { main } from '../api/corp'

type Status = main.CorpVpnStatus

function fmtDate(iso: string | undefined): string {
  if (!iso) return '—'
  const d = new Date(iso)
  return Number.isNaN(d.getTime()) ? iso : d.toLocaleString()
}

// Shown when OpenConnect refuses the gateway certificate: first use (untrusted),
// or a previously trusted pin no longer matches (CERTIFICATE_CHANGED). The user
// must explicitly choose; there is no timeout and no default action, and the
// overlay click does not confirm.
export function CorpCertTrustModal({
  gateway,
  pending,
  busy,
  onReject,
  onTrust,
}: {
  gateway: string
  pending: Status | null
  busy: boolean
  onReject: () => void
  onTrust: (pin: string) => void
}) {
  const { t } = useTranslation()
  // Escape rejects (never confirms); ignored while a connect is running.
  useEffect(() => {
    if (!pending || busy) return
    const onKey = (e: KeyboardEvent) => {
      if (e.key === 'Escape') onReject()
    }
    document.addEventListener('keydown', onKey)
    return () => document.removeEventListener('keydown', onKey)
  }, [pending, busy, onReject])
  if (!pending) return null
  const code = pending.certError || 'CERTIFICATE_UNTRUSTED'
  const changed = code === 'CERTIFICATE_CHANGED'
  const cert = pending.certificate
  const pin = pending.servercertSha256
  const row = (label: string, value: ReactNode) => (
    <div className="installConfigRow">
      <span className="fieldLab">{label}</span>
      <span>{value}</span>
    </div>
  )
  return (
    <div
      className="modalOverlay"
      role="presentation"
      style={{ zIndex: 74 }}
      onClick={busy ? undefined : onReject}
    >
      <div
        className="modalCard"
        role="dialog"
        aria-modal="true"
        aria-label={t(changed ? 'corp.cert.changedTitle' : 'corp.certTitle')}
        onClick={(e) => e.stopPropagation()}
      >
        <h3 className="modalTitle">
          {t(changed ? 'corp.cert.changedTitle' : 'corp.certTitle')}
        </h3>
        <p className="muted">{t(`corp.err.${code}`)}</p>
        {changed ? (
          <p className="installConfigWarn">{t('corp.cert.changedWarn')}</p>
        ) : (
          <p className="muted small">{t('corp.certBody')}</p>
        )}

        <div className="installConfigDetails">
          {row(t('corp.gateway'), <strong>{cert?.host || gateway}</strong>)}
          {cert && !cert.probeError ? (
            <>
              {row(t('corp.cert.subject'), cert.subject || '—')}
              {row(t('corp.cert.issuer'), cert.issuer || '—')}
              {row(
                t('corp.cert.validity'),
                `${fmtDate(cert.notBefore)} → ${fmtDate(cert.notAfter)}`,
              )}
              {row(
                t('corp.cert.fingerprint'),
                <code className="corpFingerprint">
                  {cert.sha256Fingerprint}
                </code>,
              )}
              {row(
                t('corp.cert.validation'),
                <span>
                  {cert.chainTrusted
                    ? t('corp.cert.chainOk')
                    : t('corp.cert.chainBad')}
                  {' · '}
                  {cert.hostnameMatches
                    ? t('corp.cert.hostOk')
                    : t('corp.cert.hostBad')}
                  {cert.expired ? ` · ${t('corp.cert.expired')}` : ''}
                </span>,
              )}
            </>
          ) : (
            row(t('corp.cert.details'), t('corp.cert.probeFailed'))
          )}
          {pending.certReason
            ? row(t('corp.cert.reason'), pending.certReason)
            : null}
          {row(
            t('corp.cert.newPin'),
            <code className="corpFingerprint">{pin}</code>,
          )}
          {changed && pending.previousServercert
            ? row(
                t('corp.cert.oldPin'),
                <code className="corpFingerprint">
                  {pending.previousServercert}
                </code>,
              )
            : null}
        </div>

        {cert && !cert.probeError && !cert.matchesReported ? (
          <p className="installConfigWarn">{t('corp.cert.mismatchWarn')}</p>
        ) : null}

        <div className="modalActions">
          <button
            type="button"
            className="btn ghost"
            disabled={busy}
            autoFocus
            onClick={onReject}
          >
            {t('corp.cert.reject')}
          </button>
          <button
            type="button"
            className="btn primary"
            disabled={busy || !pin}
            onClick={() => onTrust(pin)}
          >
            {t('corp.cert.trust')}
          </button>
        </div>
      </div>
    </div>
  )
}
