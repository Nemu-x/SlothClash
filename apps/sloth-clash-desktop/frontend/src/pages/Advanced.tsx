import { useState, type ReactNode } from 'react'
import { useTranslation } from 'react-i18next'

import type { main } from '../api/models'
import { friendlyErrorMessage } from '../utils/yaml'

/**
 * Collapsible card. Used for the secondary info cards on Advanced so the
 * page fits a 720px window without scrolling — the diagnostic cards stay
 * visible by default, chatty cards (Device Identity, Runtime Trace) start
 * collapsed and expand on demand.
 */
function CollapsibleCard({
  title,
  defaultOpen,
  actions,
  children,
  className,
}: {
  title: string
  defaultOpen?: boolean
  actions?: ReactNode
  children: ReactNode
  className?: string
}) {
  const { t } = useTranslation()
  const [open, setOpen] = useState(Boolean(defaultOpen))
  return (
    <div
      className={`homeCard collapsibleCard${className ? ` ${className}` : ''}`}
    >
      <div className="collapsibleCardHead">
        <button
          type="button"
          className="collapsibleCardToggle"
          onClick={() => setOpen((v) => !v)}
          aria-expanded={open}
        >
          <span className={`collapsibleCardChevron${open ? ' open' : ''}`}>
            ›
          </span>
          <span className="homeCardTitle">{title}</span>
        </button>
        <div className="collapsibleCardActions">
          {actions}
          <span className="muted small">
            {open ? t('common.hide') : t('common.show')}
          </span>
        </div>
      </div>
      {open ? <div className="collapsibleCardBody">{children}</div> : null}
    </div>
  )
}

export type ConnectivityTarget = 'google' | 'youtube' | 'telegram'

const TRACE_VISIBLE_LIMIT = 50

function formatTraceTimestamp(tsMs: number): string {
  if (!Number.isFinite(tsMs) || tsMs <= 0) return ''
  const d = new Date(tsMs)
  const hh = String(d.getHours()).padStart(2, '0')
  const mm = String(d.getMinutes()).padStart(2, '0')
  const ss = String(d.getSeconds()).padStart(2, '0')
  return `${hh}:${mm}:${ss}`
}

function serializeTrace(events: main.RuntimeDiagEvent[]): string {
  return events
    .map((e) => {
      const stamp = new Date(e.ts).toISOString()
      const msg = String(e.message ?? '').trim()
      return msg ? `${stamp} ${e.category} ${msg}` : `${stamp} ${e.category}`
    })
    .join('\n')
}

function formatBytes(b: number): string {
  if (!Number.isFinite(b) || b <= 0) return '—'
  if (b < 1024) return `${b} B`
  if (b < 1024 * 1024) return `${(b / 1024).toFixed(1)} KB`
  return `${(b / (1024 * 1024)).toFixed(2)} MB`
}

function formatModified(unix: number): string {
  if (!Number.isFinite(unix) || unix <= 0) return '—'
  return new Date(unix * 1000).toLocaleString()
}

export function AdvancedPage({
  connectionStatus,
  coreVersion,
  controllerAddr,
  mixedPort,
  deviceIdentity,
  connectivityBusy,
  connectivityResults,
  runtimeDiagEvents,
  advancedPaths,
  advancedGeo,
  toolsBusy,
  onCopyRuntimeTrace,
  error,
  onConnectivityCheck,
  onCopyHwid,
  onCopyAllIdentity,
  onOpenPath,
  onRestartCore,
  onResetSubscriptionCache,
  onReextractBundled,
  hwidEnabled,
  hwidSaving,
  onToggleHwid,
}: {
  connectionStatus: string
  coreVersion: string
  controllerAddr: string
  mixedPort: number | string
  profilePaths: main.ProfilePaths | null
  deviceIdentity: main.SubscriptionDeviceIdentityPublic | null
  connectivityBusy: string | null
  connectivityResults: Partial<Record<ConnectivityTarget, string>>
  runtimeDiagEvents: main.RuntimeDiagEvent[]
  advancedPaths: main.AdvancedPaths | null
  advancedGeo: main.AdvancedGeoStatus | null
  toolsBusy: string | null
  onCopyRuntimeTrace: (serialized: string) => void
  error: string | null
  onConnectivityCheck: (target: ConnectivityTarget, url: string) => void
  onCopyHwid: () => void
  onCopyAllIdentity: () => void
  onRefreshProxies: () => void
  onRefreshHomeInsight: () => void
  onOpenPath: (path: string) => void
  onRestartCore: () => void
  onResetSubscriptionCache: () => void
  onReextractBundled: () => void
  hwidEnabled: boolean
  hwidSaving: boolean
  onToggleHwid: (next: boolean) => void
}) {
  const { t } = useTranslation()
  return (
    <div className="panel advancedPanel">
      <h2>{t('advanced.title')}</h2>
      <p className="muted">{t('ui.advanced.lead')}</p>

      <div className="advancedGrid">
        <div className="advancedCol">
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
            <p className="muted small advancedStatusNote">
              {t('ui.advanced.pathsMovedNote')}
            </p>
          </div>

          <CollapsibleCard
            title={t('ui.advanced.connectivityTools')}
            defaultOpen
          >
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
          </CollapsibleCard>

          <CollapsibleCard
            title={t('ui.advanced.deviceIdentity')}
            className="advancedDeviceIdentityCard"
          >
            <p className="muted small">{t('ui.advanced.deviceIdentityLead')}</p>
            <div
              className="deviceIdentityHwidRow"
              data-hwid-disabled={hwidEnabled ? undefined : 'true'}
            >
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
              <span>{t('ui.advanced.hwidSendToggle')}</span>
              <label className="hwidSendToggle">
                <input
                  type="checkbox"
                  checked={hwidEnabled}
                  disabled={hwidSaving}
                  onChange={(e) => onToggleHwid(e.target.checked)}
                />
                <strong className="monoTight">
                  {hwidEnabled
                    ? t('ui.advanced.hwidSendOn')
                    : t('ui.advanced.hwidSendOff')}
                </strong>
              </label>
            </div>
            {!hwidEnabled ? (
              <p className="muted small hwidSendNote">
                {t('ui.advanced.hwidSendOffNote')}
              </p>
            ) : null}
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
          </CollapsibleCard>
        </div>

        <div className="advancedCol">
          <CollapsibleCard
            title={t('ui.advanced.runtimeTrace')}
            className="advancedRuntimeTraceCard"
            actions={
              <button
                type="button"
                className="btn ghost btnCompact"
                disabled={runtimeDiagEvents.length === 0}
                onClick={(e) => {
                  // Prevent the collapse toggle from firing when clicking Copy.
                  e.stopPropagation()
                  onCopyRuntimeTrace(serializeTrace(runtimeDiagEvents))
                }}
              >
                {t('ui.advanced.runtimeTraceCopy')}
              </button>
            }
          >
            <p className="muted small">{t('ui.advanced.runtimeTraceLead')}</p>
            {runtimeDiagEvents.length === 0 ? (
              <p className="muted small">
                {t('ui.advanced.runtimeTraceEmpty')}
              </p>
            ) : (
              <div className="advancedRuntimeTraceList">
                {runtimeDiagEvents
                  .slice(-TRACE_VISIBLE_LIMIT)
                  .reverse()
                  .map((ev) => (
                    <div
                      key={`${ev.ts}-${ev.category}-${ev.message ?? ''}`}
                      className="advancedRuntimeTraceRow"
                    >
                      <span className="advancedRuntimeTraceTime">
                        {formatTraceTimestamp(ev.ts)}
                      </span>
                      <span className="advancedRuntimeTraceCat">
                        {ev.category}
                      </span>
                      {ev.message ? (
                        <span className="advancedRuntimeTraceMsg">
                          {ev.message}
                        </span>
                      ) : null}
                    </div>
                  ))}
              </div>
            )}
          </CollapsibleCard>

          <CollapsibleCard title={t('ui.advanced.paths')}>
            <p className="muted small">{t('ui.advanced.pathsLead')}</p>
            <div className="advancedPathsGrid">
              <button
                type="button"
                className="btn ghost btnCompact"
                disabled={!advancedPaths?.dataRoot}
                onClick={() => onOpenPath(advancedPaths?.dataRoot ?? '')}
              >
                {t('ui.advanced.openDataRoot')}
              </button>
              <button
                type="button"
                className="btn ghost btnCompact"
                disabled={!advancedPaths?.runtimeDir}
                onClick={() => onOpenPath(advancedPaths?.runtimeDir ?? '')}
              >
                {t('ui.advanced.openRuntimeDir')}
              </button>
              <button
                type="button"
                className="btn ghost btnCompact"
                disabled={!advancedPaths?.activeConfig}
                onClick={() => onOpenPath(advancedPaths?.activeConfig ?? '')}
              >
                {t('ui.advanced.openActiveConfig')}
              </button>
              <button
                type="button"
                className="btn ghost btnCompact"
                disabled={!advancedPaths?.geoDir}
                onClick={() => onOpenPath(advancedPaths?.geoDir ?? '')}
              >
                {t('ui.advanced.openGeoDir')}
              </button>
              <button
                type="button"
                className="btn ghost btnCompact"
                disabled={!advancedPaths?.profilesJson}
                onClick={() => onOpenPath(advancedPaths?.profilesJson ?? '')}
              >
                {t('ui.advanced.openProfilesJson')}
              </button>
              <button
                type="button"
                className="btn ghost btnCompact"
                disabled={!advancedPaths?.prefsJson}
                onClick={() => onOpenPath(advancedPaths?.prefsJson ?? '')}
              >
                {t('ui.advanced.openPrefsJson')}
              </button>
              <button
                type="button"
                className="btn ghost btnCompact"
                disabled={!advancedPaths?.debugLog}
                onClick={() => onOpenPath(advancedPaths?.debugLog ?? '')}
              >
                {t('ui.advanced.openDebugLog')}
              </button>
              <button
                type="button"
                className="btn ghost btnCompact"
                disabled={!advancedPaths?.serviceLog}
                onClick={() => onOpenPath(advancedPaths?.serviceLog ?? '')}
              >
                {t('ui.advanced.openServiceLog')}
              </button>
            </div>
          </CollapsibleCard>

          <CollapsibleCard title={t('ui.advanced.geoStatus')}>
            <p className="muted small">{t('ui.advanced.geoStatusLead')}</p>
            {!advancedGeo?.geoIpPath && !advancedGeo?.geoSitePath ? (
              <p className="muted small">{t('ui.advanced.geoNotExtracted')}</p>
            ) : (
              <>
                <div className="statusRow">
                  <span>{t('ui.advanced.geoIpLabel')}</span>
                  <strong className="monoTight">
                    {formatBytes(advancedGeo?.geoIpSize ?? 0)} ·{' '}
                    {formatModified(advancedGeo?.geoIpModified ?? 0)}
                  </strong>
                </div>
                <div className="statusRow">
                  <span>{t('ui.advanced.geoSiteLabel')}</span>
                  <strong className="monoTight">
                    {formatBytes(advancedGeo?.geoSiteSize ?? 0)} ·{' '}
                    {formatModified(advancedGeo?.geoSiteModified ?? 0)}
                  </strong>
                </div>
              </>
            )}
          </CollapsibleCard>

          <CollapsibleCard title={t('ui.advanced.tools')} defaultOpen>
            <p className="muted small">{t('ui.advanced.toolsLead')}</p>
            <div className="advancedPowerGrid">
              <div className="advancedPowerRow">
                <button
                  type="button"
                  className="btn ghost btnCompact"
                  disabled={toolsBusy === 'restartCore'}
                  onClick={onRestartCore}
                >
                  {t('ui.advanced.restartCore')}
                </button>
                <span className="muted small">
                  {t('ui.advanced.restartCoreHint')}
                </span>
              </div>
              <div className="advancedPowerRow">
                <button
                  type="button"
                  className="btn ghost btnCompact"
                  disabled={toolsBusy === 'resetSubCache'}
                  onClick={onResetSubscriptionCache}
                >
                  {t('ui.advanced.resetSubCache')}
                </button>
                <span className="muted small">
                  {t('ui.advanced.resetSubCacheHint')}
                </span>
              </div>
              <div className="advancedPowerRow">
                <button
                  type="button"
                  className="btn ghost btnCompact"
                  disabled={toolsBusy === 'reextract'}
                  onClick={onReextractBundled}
                >
                  {t('ui.advanced.reextractBundled')}
                </button>
                <span className="muted small">
                  {t('ui.advanced.reextractBundledHint')}
                </span>
              </div>
            </div>
          </CollapsibleCard>
        </div>
      </div>

      {error ? <p className="error">{friendlyErrorMessage(error)}</p> : null}
    </div>
  )
}
