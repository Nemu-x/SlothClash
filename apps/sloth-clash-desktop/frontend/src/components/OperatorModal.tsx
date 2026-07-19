import { useTranslation } from 'react-i18next'

import type { main } from '../api/models'

/**
 * Operator info dialog — everything the panel sent via X-Brand-Desktop-*:
 * logo, name, tagline, greeting and the operator links (cabinet, renew,
 * support, …). Opened from the Home header so the links are reachable without
 * burying a permanently-visible card in Settings.
 */
export function OperatorModal({
  branding,
  lightUI,
  open,
  onClose,
  onBrowserOpen,
}: {
  branding: main.ActiveBranding | null
  lightUI: boolean
  open: boolean
  onClose: () => void
  onBrowserOpen: (url: string) => void
}) {
  const { t } = useTranslation()
  const m = branding?.manifest
  if (!open || !m) return null

  const logo = lightUI
    ? branding?.logoLightDataUri || branding?.logoDataUri
    : branding?.logoDataUri || branding?.logoLightDataUri

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
        {m.greeting || m.userDisplayName ? (
          <p className="operatorGreeting">
            {m.greeting}
            {m.greeting && m.userDisplayName ? ' ' : ''}
            {m.userDisplayName}
          </p>
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
          <button type="button" className="btn" onClick={onClose}>
            {t('common.close')}
          </button>
        </div>
      </div>
    </div>
  )
}
