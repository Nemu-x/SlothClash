import { useTranslation } from 'react-i18next'

import type { main } from '../api/models'

type TunDrafts = { dnsHijack?: string; mtu?: string; device?: string }

export function TunSettingsModal({
  open,
  tunPrefs,
  trafficPrefs,
  tunDnsHijackDraft,
  tunMtuDraft,
  tunDeviceDraft,
  tunPrefsSaving,
  onTunDnsHijackDraftChange,
  onTunMtuDraftChange,
  onTunDeviceDraftChange,
  onCommitTunPrefs,
  onCommitTrafficPrefs,
  onClose,
}: {
  open: boolean
  tunPrefs: main.TunSettings
  trafficPrefs: main.TrafficSettings
  tunDnsHijackDraft: string
  tunMtuDraft: string
  tunDeviceDraft: string
  tunPrefsSaving: boolean
  onTunDnsHijackDraftChange: (next: string) => void
  onTunMtuDraftChange: (next: string) => void
  onTunDeviceDraftChange: (next: string) => void
  onCommitTunPrefs: (
    patch: Partial<main.TunSettings>,
    drafts?: TunDrafts,
  ) => void
  onCommitTrafficPrefs: (patch: Partial<main.TrafficSettings>) => void
  onClose: () => void
}) {
  const { t } = useTranslation()
  if (!open) return null

  const tunStackValue = tunPrefs.stack ?? ''
  const findProcessModeValue: string = trafficPrefs.findProcessMode ?? ''

  const tunStackOptions: { id: string; label: string }[] = [
    { id: '', label: t('settings.tun.inherit') },
    { id: 'gvisor', label: 'gvisor' },
    { id: 'system', label: 'system' },
    { id: 'mixed', label: 'mixed' },
  ]

  const findProcessModeOptions: { id: string; label: string }[] = [
    { id: '', label: t('settings.tun.inherit') },
    { id: 'off', label: 'off' },
    { id: 'strict', label: 'strict' },
    { id: 'always', label: 'always' },
  ]

  const tristatePills = (
    value: boolean | undefined,
    onChange: (next: boolean | undefined) => void,
  ) => (
    <div className="segPill settingsTunPillRow">
      <button
        type="button"
        className={value === undefined ? 'pillOpt active' : 'pillOpt'}
        onClick={() => onChange(undefined)}
        disabled={tunPrefsSaving}
      >
        {t('settings.tun.inherit')}
      </button>
      <button
        type="button"
        className={value === true ? 'pillOpt active' : 'pillOpt'}
        onClick={() => onChange(true)}
        disabled={tunPrefsSaving}
      >
        {t('common.on')}
      </button>
      <button
        type="button"
        className={value === false ? 'pillOpt active' : 'pillOpt'}
        onClick={() => onChange(false)}
        disabled={tunPrefsSaving}
      >
        {t('common.off')}
      </button>
    </div>
  )

  return (
    <div className="modalOverlay" role="presentation" onClick={onClose}>
      <div
        className="modalCard modalCardVisualTall tunSettingsModal"
        role="dialog"
        aria-modal="true"
        aria-labelledby="tunSettingsTitle"
        onClick={(e) => e.stopPropagation()}
      >
        <h3 id="tunSettingsTitle" className="modalTitle">
          {t('settings.tun.title')}
        </h3>
        <p className="muted small">{t('settings.tun.hint')}</p>
        <div className="tunModalBody">
          <label className="field modalField">
            <span className="fieldLab">{t('settings.tun.stackLabel')}</span>
            <div className="segPill settingsTunPillRow">
              {tunStackOptions.map((opt) => (
                <button
                  key={opt.id || 'inherit'}
                  type="button"
                  className={
                    tunStackValue === opt.id ? 'pillOpt active' : 'pillOpt'
                  }
                  disabled={tunPrefsSaving}
                  onClick={() => onCommitTunPrefs({ stack: opt.id })}
                >
                  {opt.label}
                </button>
              ))}
            </div>
          </label>
          <label className="field modalField">
            <span className="fieldLab">{t('settings.tun.autoRouteLabel')}</span>
            {tristatePills(tunPrefs.autoRoute, (next) =>
              onCommitTunPrefs({ autoRoute: next }),
            )}
          </label>
          <label className="field modalField">
            <span className="fieldLab">
              {t('settings.tun.autoDetectInterfaceLabel')}
            </span>
            {tristatePills(tunPrefs.autoDetectInterface, (next) =>
              onCommitTunPrefs({ autoDetectInterface: next }),
            )}
          </label>
          <label className="field modalField">
            <span className="fieldLab">
              {t('settings.tun.strictRouteLabel')}
            </span>
            {tristatePills(tunPrefs.strictRoute, (next) =>
              onCommitTunPrefs({ strictRoute: next }),
            )}
          </label>
          <label className="field modalField">
            <span className="fieldLab">{t('settings.tun.dnsHijackLabel')}</span>
            <input
              className="input"
              placeholder="any:53"
              value={tunDnsHijackDraft}
              onChange={(e) => onTunDnsHijackDraftChange(e.target.value)}
              onBlur={() =>
                onCommitTunPrefs({}, { dnsHijack: tunDnsHijackDraft })
              }
            />
          </label>
          <label className="field modalField">
            <span className="fieldLab">{t('settings.tun.mtuLabel')}</span>
            <input
              className="input"
              placeholder="1500"
              value={tunMtuDraft}
              onChange={(e) => onTunMtuDraftChange(e.target.value)}
              onBlur={() => onCommitTunPrefs({}, { mtu: tunMtuDraft })}
            />
          </label>
          <label className="field modalField">
            <span className="fieldLab">{t('settings.tun.deviceLabel')}</span>
            <input
              className="input"
              placeholder="Mihomo"
              value={tunDeviceDraft}
              onChange={(e) => onTunDeviceDraftChange(e.target.value)}
              onBlur={() => onCommitTunPrefs({}, { device: tunDeviceDraft })}
            />
          </label>
          <label className="field modalField">
            <span className="fieldLab">{t('settings.tun.snifferLabel')}</span>
            {tristatePills(trafficPrefs.snifferEnabled, (next) =>
              onCommitTrafficPrefs({ snifferEnabled: next }),
            )}
          </label>
          <label className="field modalField">
            <span className="fieldLab">
              {t('settings.tun.findProcessLabel')}
            </span>
            <div className="segPill settingsTunPillRow">
              {findProcessModeOptions.map((opt) => (
                <button
                  key={opt.id || 'inherit'}
                  type="button"
                  className={
                    findProcessModeValue === opt.id
                      ? 'pillOpt active'
                      : 'pillOpt'
                  }
                  disabled={tunPrefsSaving}
                  onClick={() =>
                    onCommitTrafficPrefs({ findProcessMode: opt.id })
                  }
                >
                  {opt.label}
                </button>
              ))}
            </div>
          </label>
        </div>
        <div className="modalFooter">
          <div className="modalFooterRight">
            <button type="button" className="btn primary" onClick={onClose}>
              {t('common.close') || 'Close'}
            </button>
          </div>
        </div>
      </div>
    </div>
  )
}
