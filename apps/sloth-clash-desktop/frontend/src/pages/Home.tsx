import { type CSSProperties, type Ref } from 'react'
import { useTranslation } from 'react-i18next'

import type { main } from '../api/models'
import { FlagMark } from '../components/FlagMark'
import { formatSpeedKbps } from '../utils/format'
import {
  decodeSubscriptionAnnouncementDisplay,
  extractNodeFlagIso,
  filterProxyNodesForDisplay,
  isUnsafeGroupName,
  proxyNodeLabelBesideFlag,
} from '../utils/proxyNames'
import { supportSubscriptionUrlKind } from '../utils/subscription'

// Announcement card — always expanded so users see provider notices without
// extra clicks. The text body is height-bounded with internal scroll so a
// long announcement never pushes the Home layout below the viewport (which
// would force the whole page to scroll and put the sidebar nav out of reach).
function SubscriptionAnnouncement({ text }: { text: string }) {
  const { t } = useTranslation()
  const trimmed = text.trim()
  if (!trimmed) return null
  return (
    <div className="homeSubscriptionExtras">
      <div className="homeSubscriptionCard">
        <p className="homeSubscriptionCardTitle">
          {t('ui.home.subscriptionAnnouncement')}
        </p>
        <p className="homeSubscriptionAnnounce allowSelect">
          {decodeSubscriptionAnnouncementDisplay(trimmed)}
        </p>
      </div>
    </div>
  )
}

export type HomeUpdateSnap = { hasUpdate?: boolean }

export type HomeActiveNodeVisual = { iso: string; text: string }

export function HomePage({
  state,
  activeProfile,
  service,
  linkToast,
  error,
  updateSnap,
  homeUpdateTooltip,
  homeAlertTooltip,
  hasAnyProfile,
  displayMode,
  displayTraffic,
  connectBusy,
  connectVisual,
  connectionLabel,
  showProtectedBadge,
  homeTrafficHealthSubtitle,
  nodePickerGroup,
  homeActiveNodeOpen,
  homeActiveNodeRef,
  activeNode,
  activeNodeVisual,
  showBuiltinProxyGroups,
  onOpenImport,
  onOpenSupport,
  onOpenUpdate,
  onSetMode,
  onSwitchTraffic,
  onConnectClick,
  onInstallService,
  onRefreshService,
  onSelectGroup,
  onSelectNode,
  onToggleActiveNodeOpen,
}: {
  state: any
  activeProfile: any
  service: main.ServiceState | null | undefined
  linkToast: string
  error: string | null
  updateSnap: HomeUpdateSnap | null | undefined
  homeUpdateTooltip: string
  homeAlertTooltip: string
  hasAnyProfile: boolean
  displayMode: string
  displayTraffic: string
  connectBusy: boolean
  connectVisual: string
  connectionLabel: string
  showProtectedBadge: boolean
  homeTrafficHealthSubtitle: string
  nodePickerGroup: any
  homeActiveNodeOpen: boolean
  homeActiveNodeRef: Ref<HTMLDivElement>
  activeNode: string
  activeNodeVisual: HomeActiveNodeVisual
  showBuiltinProxyGroups: boolean
  onOpenImport: (reason: 'beacon' | 'manual') => void
  onOpenSupport: (url: string) => void
  onOpenUpdate: () => void
  onSetMode: (mode: 'rule' | 'global') => void
  onSwitchTraffic: (mode: 'proxy' | 'tun') => void
  onConnectClick: () => void
  onInstallService: () => void
  onRefreshService: () => void
  onSelectGroup: (name: string) => void
  onSelectNode: (group: string, node: string) => void
  onToggleActiveNodeOpen: () => void
}) {
  const { t } = useTranslation()
  const connectionStatus = state?.connection?.status ?? ''
  const lastConnectionError =
    connectionStatus === 'error'
      ? String(state?.connection?.lastError ?? '')
      : ''
  const errorLines: string[] = (() => {
    if (lastConnectionError && error === lastConnectionError) {
      return [lastConnectionError]
    }
    const lines: string[] = []
    if (error) lines.push(error)
    if (lastConnectionError && lastConnectionError !== error) {
      lines.push(lastConnectionError)
    }
    return lines
  })()
  return (
    <div className="home">
      {linkToast ? (
        <p className="homeLinkToast" role="status">
          {linkToast}
        </p>
      ) : null}
      <header className="homeHeader">
        <div>
          <p className="eyebrow">{t('ui.home.activeProfile')}</p>
          <div className="homeTitleWithAlert">
            <h2>{activeProfile?.name ?? t('ui.home.noProfileYet')}</h2>
            {updateSnap?.hasUpdate ? (
              <button
                type="button"
                className="homeUpdateBadge"
                title={homeUpdateTooltip}
                aria-label={homeUpdateTooltip}
                onClick={onOpenUpdate}
              >
                <span aria-hidden>⭳</span>
              </button>
            ) : null}
            {homeAlertTooltip ? (
              <span
                className="homeAlertBadge"
                title={homeAlertTooltip}
                role="status"
                aria-label={homeAlertTooltip}
              >
                !
              </span>
            ) : null}
          </div>
          <p className="muted">
            {activeProfile?.type === 'subscription'
              ? t('ui.common.subscription')
              : t('ui.common.local')}
          </p>
        </div>
        <div className="homeHeaderActions">
          {!hasAnyProfile ? (
            <button
              type="button"
              className="pulseBeacon"
              onClick={() => onOpenImport('beacon')}
            >
              {t('ui.home.addSubscription')}
            </button>
          ) : (
            <button
              type="button"
              className="btn subtle"
              onClick={() => onOpenImport('manual')}
            >
              {t('ui.home.addSubscriptionShort')}
            </button>
          )}
          {activeProfile?.type === 'subscription' &&
          String(activeProfile?.subscriptionSupportUrl ?? '').trim() ? (
            <button
              type="button"
              className="homeSupportHeaderBtn"
              title={String(activeProfile?.subscriptionSupportUrl ?? '').trim()}
              aria-label={t('ui.home.subscriptionSupport')}
              onClick={() =>
                onOpenSupport(
                  String(activeProfile?.subscriptionSupportUrl ?? '').trim(),
                )
              }
            >
              <span>{t('ui.home.supportShort')}</span>
              {supportSubscriptionUrlKind(
                String(activeProfile?.subscriptionSupportUrl ?? ''),
              ) === 'telegram' ? (
                <svg
                  className="homeSupportHeaderGlyph"
                  viewBox="0 0 24 24"
                  aria-hidden
                >
                  <path
                    fill="currentColor"
                    d="M2.01 21 23 12 2.01 3 2 10l15 2-15 2z"
                  />
                </svg>
              ) : (
                <svg
                  className="homeSupportHeaderGlyph"
                  viewBox="0 0 24 24"
                  aria-hidden
                >
                  <path
                    fill="currentColor"
                    d="M3.9 12c0-1.71 1.39-3.1 3.1-3.1h4V7H7c-2.76 0-5 2.24-5 5s2.24 5 5 5h4v-1.9H7c-1.71 0-3.1-1.39-3.1-3.1zM8 13h8v-2H8v2zm9-6h-4v1.9h4c1.71 0 3.1 1.39 3.1 3.1s-1.39 3.1-3.1 3.1h-4V17h4c2.76 0 5-2.24 5-5s-2.24-5-5-5z"
                  />
                </svg>
              )}
            </button>
          ) : null}
        </div>
      </header>

      <div className="connectArea">
        <div className="connectRow">
          <div className="connectSide connectSideLeft" data-tour="mode">
            <span className="sideLabel sideLabelCentered">
              {t('ui.home.mode')}
            </span>
            <div
              className="segmentInset segmentInset2"
              role="group"
              aria-label={t('ui.home.routingModeAria')}
            >
              <div
                className="segmentGlider"
                aria-hidden
                style={
                  {
                    '--seg-i': displayMode === 'global' ? 1 : 0,
                  } as CSSProperties
                }
              />
              {(['rule', 'global'] as const).map((m) => (
                <button
                  key={m}
                  type="button"
                  title={
                    m === 'rule'
                      ? t('ui.home.ruleTitle')
                      : t('ui.home.globalTitle')
                  }
                  className={
                    displayMode === m
                      ? 'segmentInsetBtn isOn'
                      : 'segmentInsetBtn'
                  }
                  onClick={() => onSetMode(m)}
                >
                  {m === 'rule' ? t('ui.common.rule') : t('ui.common.global')}
                </button>
              ))}
            </div>
          </div>
          <div className="connectCenter">
            <button
              type="button"
              className="connectBtn"
              data-tour="connect"
              data-visual={connectVisual}
              disabled={connectBusy}
              onClick={onConnectClick}
            >
              {connectionStatus === 'connected'
                ? t('ui.home.disconnect')
                : connectBusy
                  ? '…'
                  : t('ui.home.connect')}
            </button>
            <div className="statusLine statusLineSolo protectedLine">
              {showProtectedBadge ? (
                <span className="protectedBadge">
                  <span className="protectedDot" aria-hidden />
                  <span>{t('ui.home.protected')}</span>
                </span>
              ) : (
                <span className="pill">{connectionLabel}</span>
              )}
              {homeTrafficHealthSubtitle ? (
                <div className="homeTrafficHealth" role="status">
                  {homeTrafficHealthSubtitle}
                </div>
              ) : null}
            </div>
          </div>
          <div className="connectSide connectSideRight" data-tour="traffic">
            <span className="sideLabel sideLabelCentered">
              {t('ui.home.traffic')}
            </span>
            <div
              className="segmentInset segmentInset2"
              role="group"
              aria-label={t('ui.home.trafficModeAria')}
            >
              <div
                className="segmentGlider"
                aria-hidden
                style={
                  {
                    '--seg-i': displayTraffic === 'proxy' ? 0 : 1,
                  } as CSSProperties
                }
              />
              <button
                type="button"
                title={t('ui.home.systemProxyTitle')}
                className={
                  displayTraffic === 'proxy'
                    ? 'segmentInsetBtn isOn'
                    : 'segmentInsetBtn'
                }
                onClick={() => onSwitchTraffic('proxy')}
              >
                {t('ui.common.proxy')}
              </button>
              <button
                type="button"
                title={t('ui.home.tunTitle')}
                className={
                  displayTraffic === 'tun'
                    ? 'segmentInsetBtn isOn'
                    : 'segmentInsetBtn'
                }
                onClick={() => onSwitchTraffic('tun')}
              >
                TUN
              </button>
            </div>
          </div>
        </div>
      </div>

      <div className="homeStatusGrid">
        <div className="statusCard statusCardCompact">
          <div className="statusRow" data-tour="service">
            <span>{t('ui.home.service')}</span>
            <div className="statusRowValue">
              {!service?.installed ? (
                <button
                  type="button"
                  className="btn btnCompact"
                  onClick={onInstallService}
                >
                  {t('settings.installService')}
                </button>
              ) : null}
              <span
                className={
                  service?.installed
                    ? 'statusDot statusDotOk'
                    : 'statusDot statusDotBad'
                }
                title={
                  service?.installed
                    ? t('ui.home.installed')
                    : t('ui.home.notInstalled')
                }
                aria-label={
                  service?.installed
                    ? t('ui.home.installed')
                    : t('ui.home.notInstalled')
                }
                role="img"
              />
            </div>
          </div>
          {service?.installed &&
          (!service?.running || String(service?.lastError ?? '').trim()) ? (
            <div className="statusRow serviceIssueRow">
              <p className="muted serviceIssueText">
                {String(service?.lastError ?? '').trim()
                  ? String(service.lastError).trim()
                  : t('ui.home.serviceStoppedHint')}
              </p>
              <button
                type="button"
                className="btn btnCompact"
                onClick={onRefreshService}
              >
                {t('ui.home.serviceRetry')}
              </button>
            </div>
          ) : null}
          <div className="statusRow">
            <span>{t('ui.home.activeGroup')}</span>
            <strong>
              {String(state?.proxy?.activeGroup ?? '').trim() || '—'}
            </strong>
          </div>
          <div className="statusRow statusRowNode">
            <span>{t('ui.home.pickGroup')}</span>
            <div className="statusRowValue statusRowNodeValue">
              {connectionStatus === 'connected' ? (
                <div className="statusNodePick">
                  <select
                    className="selectModern selectInline selectCompact"
                    aria-label={t('ui.home.activeProxyGroupAria')}
                    value={String(state?.proxy?.activeGroup ?? '')}
                    onChange={(e) => {
                      const v = e.target.value
                      if (!v) return
                      onSelectGroup(v)
                    }}
                  >
                    {(state?.proxy?.groups ?? [])
                      .filter((g: any) => {
                        const name = String(g?.name ?? '')
                        if (isUnsafeGroupName(name)) return false
                        // Same rule as the Proxies sidebar: GLOBAL is mihomo's
                        // catch-all selector used only in Global mode. In Rule
                        // mode it is dead weight and can confuse the user into
                        // picking it as an active group.
                        if (
                          displayMode === 'rule' &&
                          name.toUpperCase() === 'GLOBAL'
                        )
                          return false
                        return true
                      })
                      .map((g: any) => (
                        <option key={g.name} value={g.name}>
                          {g.name}
                        </option>
                      ))}
                  </select>
                </div>
              ) : (
                <strong className="monoTight statusNodeTextClamp">—</strong>
              )}
            </div>
          </div>
          <div className="statusRow statusRowNode">
            <span>{t('ui.home.activeNode')}</span>
            <div className="statusRowValue statusRowNodeValue">
              {nodePickerGroup && connectionStatus === 'connected' ? (
                <div className="statusNodeMenu" ref={homeActiveNodeRef}>
                  <button
                    type="button"
                    className="statusNodeMenuTrigger selectModern selectInline selectCompact"
                    aria-haspopup="listbox"
                    aria-expanded={homeActiveNodeOpen}
                    aria-label={t('ui.home.activeNode')}
                    onClick={onToggleActiveNodeOpen}
                  >
                    <span className="statusNodeMenuTriggerInner">
                      <FlagMark
                        iso2={extractNodeFlagIso(
                          String(nodePickerGroup.selected ?? ''),
                        )}
                        width={20}
                        height={14}
                      />
                      <span className="statusNodeMenuTriggerLabel">
                        {proxyNodeLabelBesideFlag(
                          String(nodePickerGroup.selected ?? ''),
                        )}
                      </span>
                    </span>
                  </button>
                  {homeActiveNodeOpen ? (
                    <ul
                      className="statusNodeMenuList"
                      role="listbox"
                      aria-label={t('ui.home.activeNode')}
                    >
                      {filterProxyNodesForDisplay(
                        (nodePickerGroup.proxies ?? []) as string[],
                        showBuiltinProxyGroups,
                        String(nodePickerGroup.selected ?? ''),
                      ).map((p: string) => {
                        const sel = String(nodePickerGroup.selected ?? '')
                        const isSel = sel === p
                        return (
                          <li
                            key={p}
                            role="option"
                            aria-selected={isSel}
                            className={
                              'statusNodeMenuItem' + (isSel ? ' isActive' : '')
                            }
                            onClick={() => {
                              if (!p || isSel) return
                              onSelectNode(String(nodePickerGroup.name), p)
                            }}
                          >
                            <FlagMark
                              iso2={extractNodeFlagIso(p)}
                              width={16}
                              height={11}
                            />
                            <span className="statusNodeMenuItemLabel">
                              {proxyNodeLabelBesideFlag(p)}
                            </span>
                          </li>
                        )
                      })}
                    </ul>
                  ) : null}
                </div>
              ) : (
                <span className="statusNodeStatic">
                  {activeNodeVisual.iso ? (
                    <FlagMark
                      iso2={activeNodeVisual.iso}
                      width={20}
                      height={14}
                    />
                  ) : null}
                  <strong className="monoTight statusNodeTextClamp">
                    {activeNodeVisual.text || activeNode || '—'}
                  </strong>
                </span>
              )}
            </div>
          </div>
          <div className="statusRow">
            <span>Core</span>
            <span
              className={
                state?.core?.running
                  ? 'statusDot statusDotOk'
                  : 'statusDot statusDotBad'
              }
              title={state?.core?.running ? 'Running' : 'Not running'}
              aria-label={state?.core?.running ? 'Running' : 'Not running'}
              role="img"
            />
          </div>
        </div>

        <div className="statusCard statusCardCompact">
          <div className="statusRow">
            <span>{t('ui.home.insightLatency')}</span>
            <strong
              title={
                (state?.insight?.nodeLatencyMs ?? 0) <= 0 &&
                state?.insight?.latencyError
                  ? state.insight.latencyError
                  : undefined
              }
            >
              {(state?.insight?.nodeLatencyMs ?? 0) > 0
                ? `${state.insight.nodeLatencyMs} ms`
                : '—'}
            </strong>
          </div>
          <div className="statusRow">
            <span>{t('ui.home.insightExit')}</span>
            <div
              className="statusRowValue insightFlagCell"
              title={
                [state?.insight?.exitLine, state?.insight?.exitIp]
                  .map((s) => String(s ?? '').trim())
                  .filter(Boolean)
                  .join(' · ') || undefined
              }
            >
              {state?.insight?.exitFlagIso2 ? (
                <FlagMark
                  iso2={String(state.insight.exitFlagIso2)}
                  width={22}
                  height={15}
                />
              ) : state?.insight?.lastError ? (
                <span className="muted small insightExitErr">
                  {state.insight.lastError}
                </span>
              ) : state?.insight?.exitIp || state?.insight?.exitLine ? (
                <span
                  className="muted small"
                  title={[state?.insight?.exitLine, state?.insight?.exitIp]
                    .map((s) => String(s ?? '').trim())
                    .filter(Boolean)
                    .join(' · ')}
                >
                  …
                </span>
              ) : (
                <span className="muted">—</span>
              )}
            </div>
          </div>
          <div className="statusRow">
            <span>{t('ui.home.insightDirect')}</span>
            <div
              className="statusRowValue insightFlagCell"
              title={
                String(state?.mode?.current ?? '') === 'rule'
                  ? [state?.insight?.directIp, state?.insight?.directError]
                      .map((s) => String(s ?? '').trim())
                      .filter(Boolean)
                      .join(' — ') || undefined
                  : undefined
              }
            >
              {String(state?.mode?.current ?? '') === 'rule' ? (
                state?.insight?.directFlagIso2 ? (
                  <FlagMark
                    iso2={String(state.insight.directFlagIso2)}
                    width={22}
                    height={15}
                  />
                ) : state?.insight?.directError ? (
                  <span className="muted small insightExitErr">
                    {state.insight.directError}
                  </span>
                ) : state?.insight?.directIp ? (
                  <span className="muted small" title={state.insight.directIp}>
                    …
                  </span>
                ) : (
                  <span className="muted">—</span>
                )
              ) : (
                <span className="muted">—</span>
              )}
            </div>
          </div>
          <div className="statusRow">
            <span>{t('ui.home.insightSpeed')}</span>
            <div
              className="statusRowValue speedRow"
              title={state?.insight?.trafficError || undefined}
            >
              <span className="speedChip" title="Upload">
                ↑{' '}
                {state?.insight?.trafficError
                  ? '—'
                  : formatSpeedKbps(state?.insight?.uploadKbps ?? 0)}
              </span>
              <span className="speedChip" title="Download">
                ↓{' '}
                {state?.insight?.trafficError
                  ? '—'
                  : formatSpeedKbps(state?.insight?.downloadKbps ?? 0)}
              </span>
            </div>
          </div>
        </div>
      </div>

      {activeProfile?.type === 'subscription' ? (
        <SubscriptionAnnouncement
          key={String(activeProfile?.id ?? '')}
          text={String(activeProfile?.subscriptionAnnouncement ?? '')}
        />
      ) : null}

      {errorLines.length ? (
        <p className="error">{errorLines.join(' · ')}</p>
      ) : null}
    </div>
  )
}
