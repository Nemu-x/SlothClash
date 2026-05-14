import { useTranslation } from 'react-i18next'

import type { main } from '../api/models'
import { friendlyErrorMessage } from '../utils/yaml'

export type ConnectivityTarget = 'google' | 'youtube' | 'telegram'

export function AdvancedPage({
  connectionStatus,
  coreVersion,
  controllerAddr,
  mixedPort,
  profilePaths,
  deviceIdentity,
  connectivityBusy,
  connectivityResults,
  error,
  onConnectivityCheck,
  onCopyHwid,
  onCopyAllIdentity,
  onRefreshProxies,
  onRefreshHomeInsight,
}: {
  connectionStatus: string
  coreVersion: string
  controllerAddr: string
  mixedPort: number | string
  profilePaths: main.ProfilePaths | null
  deviceIdentity: main.SubscriptionDeviceIdentityPublic | null
  connectivityBusy: string | null
  connectivityResults: Partial<Record<ConnectivityTarget, string>>
  error: string | null
  onConnectivityCheck: (target: ConnectivityTarget, url: string) => void
  onCopyHwid: () => void
  onCopyAllIdentity: () => void
  onRefreshProxies: () => void
  onRefreshHomeInsight: () => void
}) {
  const { t } = useTranslation()
  return (
    <div className="panel advancedPanel">
      <h2>{t('advanced.title')}</h2>
      <p className="muted">{t('ui.advanced.lead')}</p>

      <div className="advancedGrid">
        <div className="homeCard">
          <h3 className="homeCardTitle">{t('ui.advanced.diagnostics')}</h3>
          <div className="statusRow">
            <span>{t('advanced.connection')}</span>
            <strong>{String(connectionStatus || '—')}</strong>
          </div>
          <div className="statusRow">
            <span>{t('ui.advanced.coreVersion')}</span>
            <strong>{String(coreVersion || '—')}</strong>
          </div>
          <div className="statusRow">
            <span>{t('ui.advanced.controller')}</span>
            <strong className="monoTight">
              {String(controllerAddr || '—')}
            </strong>
          </div>
          <div className="statusRow">
            <span>{t('ui.advanced.mixedPort')}</span>
            <strong>{mixedPort || '—'}</strong>
          </div>
          <div className="statusRow">
            <span>{t('ui.advanced.runtimeDir')}</span>
            <strong className="monoTight">
              {String(profilePaths?.dataDir ?? '—')}
            </strong>
          </div>
          <div className="statusRow">
            <span>{t('ui.advanced.configFile')}</span>
            <strong className="monoTight">
              {String(profilePaths?.configPath ?? '—')}
            </strong>
          </div>
        </div>

        <div className="homeCard">
          <h3 className="homeCardTitle">
            {t('ui.advanced.connectivityTools')}
          </h3>
          <p className="muted small">{t('ui.advanced.connectivityLead')}</p>
          <div className="row">
            <button
              type="button"
              className="btn ghost"
              onClick={() =>
                onConnectivityCheck(
                  'google',
                  'https://www.google.com/generate_204',
                )
              }
              disabled={connectivityBusy === 'google'}
            >
              Google
            </button>
            <button
              type="button"
              className="btn ghost"
              onClick={() =>
                onConnectivityCheck(
                  'youtube',
                  'https://www.youtube.com/generate_204',
                )
              }
              disabled={connectivityBusy === 'youtube'}
            >
              YouTube
            </button>
            <button
              type="button"
              className="btn ghost"
              onClick={() =>
                onConnectivityCheck('telegram', 'https://web.telegram.org')
              }
              disabled={connectivityBusy === 'telegram'}
            >
              Telegram
            </button>
          </div>
          <div className="diagList">
            <div className="statusRow">
              <span>Google</span>
              <strong>{connectivityResults.google ?? '—'}</strong>
            </div>
            <div className="statusRow">
              <span>YouTube</span>
              <strong>{connectivityResults.youtube ?? '—'}</strong>
            </div>
            <div className="statusRow">
              <span>Telegram</span>
              <strong>{connectivityResults.telegram ?? '—'}</strong>
            </div>
          </div>
        </div>
      </div>

      <div className="homeCard advancedDeviceIdentityCard">
        <h3 className="homeCardTitle">{t('ui.advanced.deviceIdentity')}</h3>
        <p className="muted small">{t('ui.advanced.deviceIdentityLead')}</p>
        <div className="deviceIdentityHwidRow">
          <div className="deviceIdentityHwid monoTight">
            {deviceIdentity?.hwid ?? '—'}
          </div>
          <div className="deviceIdentityActions">
            <button
              type="button"
              className="btn btnCompact"
              disabled={!deviceIdentity?.hwid}
              onClick={onCopyHwid}
            >
              {t('ui.advanced.copyHwid')}
            </button>
            <button
              type="button"
              className="btn ghost btnCompact"
              disabled={!deviceIdentity}
              onClick={onCopyAllIdentity}
            >
              {t('ui.advanced.copyAllIdentity')}
            </button>
          </div>
        </div>
        <div className="statusRow">
          <span>{t('ui.advanced.identityDeviceOs')}</span>
          <strong className="monoTight">
            {deviceIdentity?.deviceOs ?? '—'}
          </strong>
        </div>
        <div className="statusRow">
          <span>{t('ui.advanced.identityOsVersion')}</span>
          <strong className="monoTight">
            {deviceIdentity?.osVersion ?? '—'}
          </strong>
        </div>
        <div className="statusRow">
          <span>{t('ui.advanced.identityDeviceModel')}</span>
          <strong className="monoTight">
            {deviceIdentity?.deviceModel ?? '—'}
          </strong>
        </div>
        <div className="statusRow">
          <span>{t('ui.advanced.identityAppVersion')}</span>
          <strong className="monoTight">
            {deviceIdentity?.appVersion ?? '—'}
          </strong>
        </div>
      </div>

      <div className="homeCard">
        <h3 className="homeCardTitle">Maintenance</h3>
        <p className="muted small">
          Safe operations to keep runtime healthy without full reset.
        </p>
        <div className="row">
          <button
            type="button"
            className="btn ghost"
            onClick={onRefreshProxies}
            disabled={connectionStatus !== 'connected'}
          >
            Refresh proxies snapshot
          </button>
          <button
            type="button"
            className="btn ghost"
            onClick={onRefreshHomeInsight}
            disabled={connectionStatus !== 'connected'}
          >
            Refresh home insight
          </button>
        </div>
      </div>
      {error ? <p className="error">{friendlyErrorMessage(error)}</p> : null}
    </div>
  )
}
