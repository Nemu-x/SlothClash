import { useState } from 'react'
import { useTranslation } from 'react-i18next'

import type { main } from '../api/models'
import { formatBytesSmart } from '../utils/format'
import {
  parseSubscriptionUsage,
  profileSubscriptionHost,
  subscriptionDaysLeft,
} from '../utils/subscription'

/**
 * Operator info dialog — everything about the provider in one place: what the
 * panel sent via X-Brand-Desktop-* (logo, name, links) plus the two things a
 * user actually opens this for: how much of the subscription is left, and how
 * to reach support with the details support always asks for.
 */
export function OperatorModal({
  branding,
  activeProfile,
  deviceIdentity,
  appVersion,
  coreVersion,
  serviceVersion,
  lightUI,
  open,
  onClose,
  onBrowserOpen,
}: {
  branding: main.ActiveBranding | null
  activeProfile: any
  deviceIdentity: main.SubscriptionDeviceIdentityPublic | null
  appVersion: string
  coreVersion: string
  serviceVersion: string
  lightUI: boolean
  open: boolean
  onClose: () => void
  onBrowserOpen: (url: string) => void
}) {
  const { t } = useTranslation()
  const [copied, setCopied] = useState(false)
  const m = branding?.manifest
  if (!open || !m) return null

  const logo = lightUI
    ? branding?.logoLightDataUri || branding?.logoDataUri
    : branding?.logoDataUri || branding?.logoLightDataUri

  const usage = parseSubscriptionUsage(activeProfile)
  const daysLeft = subscriptionDaysLeft(usage)
  const host = profileSubscriptionHost(String(activeProfile?.url ?? ''))
  // Only a known total makes a ratio meaningful; unlimited plans just show used.
  const pct =
    usage && usage.totalBytes > 0
      ? Math.min(100, Math.round((usage.usedBytes / usage.totalBytes) * 100))
      : null

  const links: Array<[string, string | undefined]> = [
    [t('settings.operatorCabinet'), m.cabinetUrl],
    [t('settings.operatorRenew'), m.renewUrl],
    [t('settings.operatorSupport'), m.supportUrl],
    ['Telegram', m.telegramUrl],
    [t('settings.operatorBot'), m.botUrl],
    [t('settings.operatorWebsite'), m.websiteUrl],
    [t('settings.operatorStatus'), m.statusUrl],
    [t('settings.operatorHelp'), m.helpUrl],
    [t('settings.operatorPrivacy'), m.privacyUrl],
    [t('settings.operatorTerms'), m.termsUrl],
  ]
  const shown = links.filter(([, url]) => !!url)

  // One click instead of a ten-message back-and-forth: everything support
  // usually asks for, in a paste-ready block.
  const copySupportInfo = async () => {
    const lines = [
      m.name ? `Operator: ${m.name}` : '',
      host ? `Subscription: ${host}` : '',
      `App: ${appVersion}`,
      coreVersion ? `Core: ${coreVersion}` : '',
      serviceVersion ? `Service: ${serviceVersion}` : '',
      deviceIdentity?.deviceOs ? `OS: ${deviceIdentity.deviceOs}` : '',
      deviceIdentity?.osVersion
        ? `OS version: ${deviceIdentity.osVersion}`
        : '',
      deviceIdentity?.deviceModel
        ? `Device: ${deviceIdentity.deviceModel}`
        : '',
      deviceIdentity?.hwid ? `HWID: ${deviceIdentity.hwid}` : '',
    ].filter(Boolean)
    try {
      await navigator.clipboard.writeText(lines.join('\n'))
      setCopied(true)
      setTimeout(() => setCopied(false), 2000)
    } catch {
      // clipboard unavailable — nothing actionable, the dialog stays usable
    }
  }

  return (
    <div className="modalOverlay" role="presentation" onClick={onClose}>
      <div
        className="modalCard operatorModalCard"
        role="dialog"
        aria-modal="true"
        aria-labelledby="operatorTitle"
        onClick={(e) => e.stopPropagation()}
      >
        {logo ? (
          <img className="operatorLogo" src={logo} alt={m.name || 'logo'} />
        ) : null}
        <h3 id="operatorTitle" className="modalTitle">
          {m.name || t('settings.operator')}
        </h3>
        {m.tagline ? <p className="muted small">{m.tagline}</p> : null}
        {host ? <p className="muted small operatorHost">{host}</p> : null}
        {m.greeting || m.userDisplayName ? (
          <p className="operatorGreeting">
            {m.greeting}
            {m.greeting && m.userDisplayName ? ' ' : ''}
            {m.userDisplayName}
          </p>
        ) : null}

        {usage ? (
          <div className="operatorUsage">
            <div className="operatorUsageRow">
              <span className="muted small">{t('ui.operator.traffic')}</span>
              <span className="operatorUsageValue">
                {formatBytesSmart(usage.usedBytes)}
                {usage.totalBytes > 0
                  ? ` / ${formatBytesSmart(usage.totalBytes)}`
                  : ''}
              </span>
            </div>
            {pct !== null ? (
              <div
                className="operatorUsageBar"
                role="progressbar"
                aria-valuenow={pct}
                aria-valuemin={0}
                aria-valuemax={100}
              >
                <div
                  className="operatorUsageBarFill"
                  style={{ width: `${pct}%` }}
                />
              </div>
            ) : null}
            {daysLeft !== null ? (
              <div className="operatorUsageRow">
                <span className="muted small">{t('ui.operator.expires')}</span>
                <span
                  className={
                    daysLeft <= 3
                      ? 'operatorUsageValue operatorUsageWarn'
                      : 'operatorUsageValue'
                  }
                >
                  {daysLeft >= 0
                    ? t('ui.operator.daysLeft', { count: daysLeft })
                    : t('ui.operator.expired')}
                </span>
              </div>
            ) : null}
          </div>
        ) : null}

        {shown.length > 0 ? (
          <div className="operatorLinks">
            {shown.map(([label, url]) => (
              <button
                key={label}
                type="button"
                className="btn ghost operatorLinkBtn"
                onClick={() => onBrowserOpen(String(url))}
              >
                {label}
              </button>
            ))}
          </div>
        ) : null}

        <div className="modalFooter">
          <button
            type="button"
            className="btn btnModalSecondary"
            onClick={() => void copySupportInfo()}
          >
            {copied ? t('ui.operator.copied') : t('ui.operator.copyForSupport')}
          </button>
          <button type="button" className="btn" onClick={onClose}>
            {t('common.close')}
          </button>
        </div>
      </div>
    </div>
  )
}
