import { useTranslation } from 'react-i18next'

import type { main } from '../api/models'
import type { SettingsResetMode } from '../components/SettingsResetModal'
import { APP_REPO_URL } from '../constants'
import type { CompactSettings } from '../types/app'
import { UI_SCALE_OPTIONS } from '../utils/settings'
import { friendlyErrorMessage } from '../utils/yaml'

export type ThemeMode = 'dark' | 'light' | 'system'
export type Lang = 'en' | 'ru' | 'zh'

// iOS-style sliding switch for boolean settings (scoped to Settings; the Home
// Proxy/TUN selector keeps its own .trafficKnob style).
function SettingsSwitch({
  checked,
  disabled,
  onToggle,
  label,
}: {
  checked: boolean
  disabled?: boolean
  onToggle: () => void
  label: string
}) {
  return (
    <button
      type="button"
      role="switch"
      aria-checked={checked}
      aria-label={label}
      disabled={disabled}
      className={`settingsSwitch${checked ? ' on' : ''}`}
      onClick={onToggle}
    >
      <span className="settingsSwitchKnob" aria-hidden />
    </button>
  )
}

export function SettingsPage({
  theme,
  lang,
  settings,
  settingsBusy,
  trayAvailable,
  tunStackValue,
  tunPrefs,
  trafficPrefs,
  tunBanner,
  state,
  updateSnap,
  error,
  onBrowserOpen,
  onSetTheme,
  onSetLang,
  onSetSetting,
  onSetLaunchOnStartup,
  onInstallService,
  onEnsureTun,
  onShowTunModal,
  onApplyDefaultAutoUpdate,
  onRefreshAllSubs,
  onExportDiagnostics,
  onClearCache,
  onOpenResetModal,
  onCheckUpdates,
  onApplyUpdate,
  updateProgress,
  appUpdateEnabled,
  onToggleAppUpdate,
}: {
  theme: ThemeMode
  lang: Lang
  settings: CompactSettings
  settingsBusy: boolean
  trayAvailable: boolean
  tunStackValue: string
  tunPrefs: main.TunSettings
  trafficPrefs: main.TrafficSettings
  tunBanner: string
  state: any
  updateSnap: any
  error: string | null
  onBrowserOpen: (url: string) => void
  onSetTheme: (theme: ThemeMode) => void
  onSetLang: (lang: Lang) => void
  onSetSetting: <K extends keyof CompactSettings>(
    key: K,
    value: CompactSettings[K],
  ) => void
  onSetLaunchOnStartup: (next: boolean) => void
  onInstallService: () => void
  onEnsureTun: () => void
  onShowTunModal: () => void
  onApplyDefaultAutoUpdate: () => void
  onRefreshAllSubs: () => void
  onExportDiagnostics: () => void
  onClearCache: () => void
  onOpenResetModal: (mode: SettingsResetMode) => void
  onCheckUpdates: () => void
  onApplyUpdate: () => void
  updateProgress: { downloaded: number; total: number; pct: number } | null
  appUpdateEnabled: boolean
  onToggleAppUpdate: (next: boolean) => void
}) {
  const { t } = useTranslation()
  return (
    <div className="panel settingsPanel">
      <div className="settingsTopBar">
        <h2>{t('settings.title')}</h2>
        <button
          type="button"
          className="settingsGithubBtn settingsGithubTopBtn"
          title={t('settings.openGithubRepo')}
          aria-label={t('settings.openGithubRepo')}
          onClick={() => onBrowserOpen(APP_REPO_URL)}
        >
          <svg
            className="settingsGithubSvg"
            viewBox="0 0 24 24"
            width="22"
            height="22"
            aria-hidden
          >
            <path
              fill="currentColor"
              d="M12 0c-6.626 0-12 5.373-12 12 0 5.302 3.438 9.8 8.207 11.387.599.111.793-.261.793-.577v-2.234c-3.338.726-4.033-1.416-4.033-1.416-.546-1.387-1.333-1.756-1.333-1.756-1.089-.745.083-.729.083-.729 1.205.084 1.839 1.237 1.839 1.237 1.07 1.834 2.807 1.304 3.492.997.107-.775.418-1.305.762-1.604-2.665-.305-5.467-1.334-5.467-5.931 0-1.311.469-2.381 1.236-3.221-.124-.303-.535-1.524.117-3.176 0 0 1.008-.322 3.301 1.23.957-.266 1.983-.399 3.003-.404 1.02.005 2.047.138 3.006.404 2.291-1.552 3.297-1.23 3.297-1.23.653 1.653.242 2.874.118 3.176.77.84 1.235 1.911 1.235 3.221 0 4.609-2.807 5.624-5.479 5.921.43.372.823 1.102.823 2.222v3.293c0 .319.192.694.801.576 4.765-1.589 8.199-6.086 8.199-11.386 0-6.627-5.373-12-12-12z"
            />
          </svg>
        </button>
      </div>
      <p className="muted settingsPanelLead">{t('settings.lead')}</p>

      <div className="settingsGridCompact">
        <div className="settingsCol">
          <div className="homeCard settingsCardCompact">
            <h3 className="homeCardTitle">
              <span className="settingsCardIcon" aria-hidden>
                ⚙️
              </span>
              {t('settings.general')}
            </h3>
            <label className="field">
              <span className="fieldLab">{t('settings.theme')}</span>
              <div className="segPill">
                {(['system', 'dark', 'light'] as const).map((th) => (
                  <button
                    key={th}
                    type="button"
                    className={theme === th ? 'pillOpt active' : 'pillOpt'}
                    onClick={() => onSetTheme(th)}
                  >
                    {th === 'system'
                      ? t('settings.themeSystem')
                      : th === 'dark'
                        ? t('settings.themeDark')
                        : t('settings.themeLight')}
                  </button>
                ))}
              </div>
            </label>
            <label className="field">
              <span className="fieldLab">{t('settings.language')}</span>
              <select
                className="selectModern"
                value={lang}
                onChange={(e) => onSetLang(e.target.value as Lang)}
              >
                <option value="en">English</option>
                <option value="ru">Русский</option>
                <option value="zh">简体中文</option>
              </select>
            </label>
            <label className="field">
              <span className="fieldLab">{t('settings.uiScale')}</span>
              <select
                className="selectModern"
                value={String(settings.uiScale)}
                onChange={(e) =>
                  onSetSetting('uiScale', Number(e.target.value))
                }
              >
                {UI_SCALE_OPTIONS.map((s) => (
                  <option key={s} value={String(s)}>
                    {Math.round(s * 100)}%
                  </option>
                ))}
              </select>
            </label>
            <div className="settingsToggleRow">
              <span>{t('settings.startMinimized')}</span>
              <SettingsSwitch
                checked={settings.startMinimized}
                label={t('settings.startMinimized')}
                onToggle={() =>
                  onSetSetting('startMinimized', !settings.startMinimized)
                }
              />
            </div>
            <div className="settingsToggleRow">
              <span>{t('settings.launchOnStartup')}</span>
              <SettingsSwitch
                checked={settings.launchOnStartup}
                label={t('settings.launchOnStartup')}
                onToggle={() => onSetLaunchOnStartup(!settings.launchOnStartup)}
              />
            </div>
            <div className="settingsToggleRow">
              <span>{t('settings.closeToTray')}</span>
              <SettingsSwitch
                checked={settings.closeToTray}
                disabled={!trayAvailable}
                label={t('settings.closeToTray')}
                onToggle={() =>
                  onSetSetting('closeToTray', !settings.closeToTray)
                }
              />
            </div>
          </div>

          <div className="homeCard settingsCardCompact">
            <h3 className="homeCardTitle">
              <span className="settingsCardIcon" aria-hidden>
                🌐
              </span>
              {t('settings.connection')}
            </h3>
            <div className="settingsToggleRow">
              <span>{t('settings.smartDns')}</span>
              <SettingsSwitch
                checked={settings.dnsSmartFallback}
                label={t('settings.smartDns')}
                onToggle={() =>
                  onSetSetting('dnsSmartFallback', !settings.dnsSmartFallback)
                }
              />
            </div>
            <div className="settingsToggleRow">
              <span>{t('settings.ipv6Dns')}</span>
              <SettingsSwitch
                checked={settings.dnsIpv6}
                label={t('settings.ipv6Dns')}
                onToggle={() => onSetSetting('dnsIpv6', !settings.dnsIpv6)}
              />
            </div>
            <div className="settingsToggleRow">
              <span>{t('settings.allowLanBinding')}</span>
              <SettingsSwitch
                checked={settings.dnsAllowLan}
                label={t('settings.allowLanBinding')}
                onToggle={() =>
                  onSetSetting('dnsAllowLan', !settings.dnsAllowLan)
                }
              />
            </div>
            <p className="muted settingsMicroHint">{t('settings.dnsHint')}</p>
            <div className="row">
              <button
                type="button"
                className="btn ghost"
                onClick={onInstallService}
              >
                {t('settings.installService')}
              </button>
              <button type="button" className="btn ghost" onClick={onEnsureTun}>
                {t('settings.guidedTun')}
              </button>
            </div>
          </div>

          <div className="homeCard settingsCardCompact">
            <h3 className="homeCardTitle">
              <span className="settingsCardIcon" aria-hidden>
                🛡️
              </span>
              {t('settings.tun.title')}
            </h3>
            <p className="muted settingsMicroHint">{t('settings.tun.hint')}</p>
            <div className="settingsTunSummary">
              <span className="settingsTunSummaryItem">
                <span className="settingsTunSummaryLabel">
                  {t('settings.tun.stackLabel')}:
                </span>
                <span className="settingsTunSummaryValue">
                  {tunStackValue || t('settings.tun.inherit')}
                </span>
              </span>
              <span className="settingsTunSummaryItem">
                <span className="settingsTunSummaryLabel">
                  {t('settings.tun.autoRouteLabel')}:
                </span>
                <span className="settingsTunSummaryValue">
                  {tunPrefs.autoRoute === undefined
                    ? t('settings.tun.inherit')
                    : tunPrefs.autoRoute
                      ? t('common.on')
                      : t('common.off')}
                </span>
              </span>
              <span className="settingsTunSummaryItem">
                <span className="settingsTunSummaryLabel">
                  {t('settings.tun.snifferLabel')}:
                </span>
                <span className="settingsTunSummaryValue">
                  {trafficPrefs.snifferEnabled === undefined
                    ? t('settings.tun.inherit')
                    : trafficPrefs.snifferEnabled
                      ? t('common.on')
                      : t('common.off')}
                </span>
              </span>
            </div>
            <div className="settingsInlineActions">
              <button type="button" className="btn" onClick={onShowTunModal}>
                {t('settings.tun.configure')}
              </button>
            </div>
          </div>
        </div>

        <div className="settingsCol">
          <div className="homeCard settingsCardCompact">
            <h3 className="homeCardTitle">
              <span className="settingsCardIcon" aria-hidden>
                🔄
              </span>
              {t('settings.profilesUpdates')}
            </h3>
            <label className="field">
              <span className="fieldLab">
                Default auto-update interval (min)
              </span>
              <input
                className="input"
                value={String(settings.defaultAutoUpdateMinutes)}
                onChange={(e) =>
                  onSetSetting(
                    'defaultAutoUpdateMinutes',
                    Math.max(5, Number(e.target.value || 360)),
                  )
                }
                placeholder="360"
              />
            </label>
            <div className="settingsToggleRow">
              <span>{t('settings.reconnectOnManualProfileUpdate')}</span>
              <SettingsSwitch
                checked={settings.reconnectOnManualProfileUpdate}
                label={t('settings.reconnectOnManualProfileUpdate')}
                onToggle={() =>
                  onSetSetting(
                    'reconnectOnManualProfileUpdate',
                    !settings.reconnectOnManualProfileUpdate,
                  )
                }
              />
            </div>
            <div className="row">
              <button
                type="button"
                className="btn ghost"
                disabled={settingsBusy}
                onClick={onApplyDefaultAutoUpdate}
              >
                Apply defaults to profiles
              </button>
              <button
                type="button"
                className="btn"
                disabled={settingsBusy}
                onClick={onRefreshAllSubs}
              >
                Refresh all subscriptions
              </button>
            </div>
          </div>

          <div className="homeCard settingsCardCompact">
            <h3 className="homeCardTitle">
              <span className="settingsCardIcon" aria-hidden>
                🧰
              </span>
              {t('settings.dataDiag')}
            </h3>
            <label className="field">
              <span className="fieldLab">{t('settings.logLevel')}</span>
              <select
                className="selectModern"
                value={settings.logLevel}
                onChange={(e) =>
                  onSetSetting(
                    'logLevel',
                    e.target.value as CompactSettings['logLevel'],
                  )
                }
              >
                <option value="error">error</option>
                <option value="warn">warn</option>
                <option value="info">info</option>
                <option value="debug">debug</option>
              </select>
            </label>
            <div className="row">
              <button
                type="button"
                className="btn ghost"
                disabled={settingsBusy}
                onClick={onExportDiagnostics}
              >
                Export diagnostics bundle
              </button>
              <button
                type="button"
                className="btn ghost"
                onClick={onClearCache}
              >
                Clear cache/temp
              </button>
            </div>
            <div className="row">
              <button
                type="button"
                className="btn ghost"
                onClick={() => onOpenResetModal('keep_profiles')}
              >
                Reset app settings
              </button>
              <button
                type="button"
                className="btn ghost"
                onClick={() => onOpenResetModal('with_profiles')}
              >
                Reset + remove profiles
              </button>
            </div>
          </div>

          <div className="homeCard settingsCardCompact settingsInfoDevCard">
            <h3 className="homeCardTitle settingsInfoDevTitle">
              <span className="settingsCardIcon" aria-hidden>
                ℹ️
              </span>
              {t('settings.info')}
            </h3>
            <div className="settingsInfoDevBody">
              <div className="settingsKpiGrid">
                <div className="settingsKpi">
                  <span>Core</span>
                  <strong title={t('settings.coreTitle')}>
                    {state?.core?.version?.trim() ? state.core.version : '—'}
                  </strong>
                </div>
                <div className="settingsKpi">
                  <span>{t('settings.appVersionLabel')}</span>
                  <strong>
                    {String(updateSnap?.currentVersion ?? '').trim()
                      ? String(updateSnap.currentVersion)
                      : ((import.meta.env.VITE_APP_VERSION as
                          | string
                          | undefined) ?? 'dev')}
                  </strong>
                </div>
                <div className="settingsKpi">
                  <span>{t('settings.updateChannelLabel')}</span>
                  <strong>{updateSnap?.channel ?? 'stable'}</strong>
                </div>
                <div className="settingsKpi">
                  <span>{t('settings.lastCheckedLabel')}</span>
                  <strong>
                    {updateSnap?.lastCheckedAt
                      ? new Date(
                          (updateSnap.lastCheckedAt as number) * 1000,
                        ).toLocaleString()
                      : 'Never'}
                  </strong>
                </div>
              </div>
              <div className="settingsInfoDevActions">
                <div className="settingsToggleRow">
                  <span>{t('settings.autoUpdate')}</span>
                  <SettingsSwitch
                    checked={appUpdateEnabled}
                    label={t('settings.autoUpdate')}
                    onToggle={() => onToggleAppUpdate(!appUpdateEnabled)}
                  />
                </div>
                {updateSnap?.hasUpdate ? (
                  <p className="banner" role="status">
                    {t('settings.updateAvailable', {
                      version: String(updateSnap.latestVersion ?? ''),
                    })}
                  </p>
                ) : updateSnap?.lastCheckedAt && !updateSnap?.lastError ? (
                  <p className="small settingsUpToDate" role="status">
                    ✓ {t('settings.upToDate')}
                  </p>
                ) : null}
                {updateSnap?.lastError ? (
                  <p className="error small">{String(updateSnap.lastError)}</p>
                ) : null}
                {updateSnap?.hasUpdate &&
                !String(updateSnap?.assetDownloadUrl ?? '').trim() ? (
                  <p className="muted small">
                    {t('settings.updatePlatformNote')}
                  </p>
                ) : null}
                {updateSnap?.hasUpdate &&
                String(updateSnap?.releaseNotes ?? '').trim() ? (
                  <details className="settingsChangelog">
                    <summary>{t('settings.whatsNew')}</summary>
                    <pre className="settingsChangelogBody">
                      {String(updateSnap.releaseNotes)}
                    </pre>
                  </details>
                ) : null}
                {updateProgress ? (
                  <div className="updateProgress">
                    <div className="updateProgressTrack">
                      <div
                        className={`updateProgressBar${updateProgress.pct < 0 ? ' indeterminate' : ''}`}
                        style={
                          updateProgress.pct >= 0
                            ? {
                                width: `${Math.min(100, Math.max(0, updateProgress.pct))}%`,
                              }
                            : undefined
                        }
                      />
                    </div>
                    <span className="muted small">
                      {updateProgress.pct >= 100
                        ? t('settings.updateStarting')
                        : t('settings.updateDownloading', {
                            pct:
                              updateProgress.pct >= 0
                                ? Math.round(updateProgress.pct)
                                : '…',
                          })}
                    </span>
                  </div>
                ) : null}
                <div className="row settingsInfoDevBtnRow">
                  <button
                    type="button"
                    className="btn"
                    onClick={onCheckUpdates}
                  >
                    {t('settings.checkUpdates')}
                  </button>
                  {updateSnap?.hasUpdate &&
                  String(updateSnap?.assetDownloadUrl ?? '').trim() ? (
                    <button
                      type="button"
                      className="btn primary"
                      disabled={Boolean(updateProgress)}
                      onClick={onApplyUpdate}
                    >
                      {t('settings.downloadInstaller')}
                    </button>
                  ) : null}
                  {String(updateSnap?.releaseUrl ?? '').trim() ? (
                    <button
                      type="button"
                      className="btn ghost"
                      onClick={() =>
                        onBrowserOpen(String(updateSnap.releaseUrl))
                      }
                    >
                      {t('settings.openReleasePage')}
                    </button>
                  ) : null}
                </div>
              </div>
            </div>
          </div>
        </div>
      </div>
      {tunBanner ? <p className="banner">{tunBanner}</p> : null}
      {error ? <p className="error">{friendlyErrorMessage(error)}</p> : null}
    </div>
  )
}
