import { type CSSProperties } from 'react'
import { useTranslation } from 'react-i18next'

import { FlagMark } from '../components/FlagMark'
import {
  extractNodeFlagIso,
  filterProxyNodesForDisplay,
  isUnsafeGroupName,
  nodeDisplayName,
  nodeFeatureTags,
  selectedNodeEmoji,
} from '../utils/proxyNames'
import { friendlyErrorMessage } from '../utils/yaml'

export function ProxiesPage({
  groups,
  activeGroup,
  connectionStatus,
  displayMode,
  showBuiltin,
  expandedGroups,
  proxyDelayBusy,
  proxyDelayMap,
  proxyDelayErr,
  error,
  onRefreshProxies,
  onToggleShowBuiltin,
  onSetMode,
  onSelectGroup,
  onToggleExpand,
  onSelectNode,
  onPingAll,
}: {
  groups: any[]
  activeGroup: string
  connectionStatus: string
  displayMode: string
  showBuiltin: boolean
  expandedGroups: Record<string, boolean>
  proxyDelayBusy: Record<string, boolean>
  proxyDelayMap: Record<string, number>
  proxyDelayErr: Record<string, string>
  error: string | null
  onRefreshProxies: () => void
  onToggleShowBuiltin: () => void
  onSetMode: (mode: 'rule' | 'global') => void
  onSelectGroup: (name: string) => void
  onToggleExpand: (name: string) => void
  onSelectNode: (group: string, node: string) => void
  onPingAll: (group: string, nodes: string[]) => void
}) {
  const { t } = useTranslation()
  const visibleGroups = showBuiltin
    ? groups
    : groups.filter((g: any) => !isUnsafeGroupName(String(g?.name ?? '')))
  return (
    <div className="panel proxiesPanel">
      <h2>{t('ui.proxies.title')}</h2>
      <p className="muted">{t('ui.proxies.lead')}</p>
      <div className="row">
        <button
          type="button"
          className="btn"
          disabled={connectionStatus !== 'connected'}
          onClick={onRefreshProxies}
        >
          {t('ui.proxies.refreshGroups')}
        </button>
        <button
          type="button"
          className={showBuiltin ? 'btn' : 'btn ghost'}
          onClick={onToggleShowBuiltin}
        >
          {t('ui.proxies.showBuiltin')}
        </button>
      </div>
      <div className="proxyTopSwitches">
        <div
          className="segmentInset segmentInset2 proxyModeInset"
          role="group"
          aria-label="Proxy mode"
        >
          <div
            className="segmentGlider"
            aria-hidden
            style={
              {
                '--seg-i': displayMode === 'rule' ? 0 : 1,
              } as CSSProperties
            }
          />
          <button
            type="button"
            className={
              displayMode === 'rule'
                ? 'segmentInsetBtn isOn'
                : 'segmentInsetBtn'
            }
            onClick={() => onSetMode('rule')}
          >
            {t('ui.common.rule')}
          </button>
          <button
            type="button"
            className={
              displayMode === 'global'
                ? 'segmentInsetBtn isOn'
                : 'segmentInsetBtn'
            }
            onClick={() => onSetMode('global')}
          >
            {t('ui.common.global')}
          </button>
        </div>
      </div>
      <div className="segment modern policyBlock">
        <span className="segLabel">{t('ui.proxies.focusGroup')}</span>
        <select
          className="selectModern"
          value={activeGroup}
          onChange={(e) => {
            const v = e.target.value
            if (!v) return
            onSelectGroup(v)
          }}
        >
          <option value="">—</option>
          {visibleGroups.map((g: any) => {
            const flag = selectedNodeEmoji(g.selected)
            return (
              <option key={g.name} value={g.name}>
                {`${flag ? `${flag} ` : ''}${g.name} (${g.type})`}
              </option>
            )
          })}
        </select>
      </div>
      <div className="proxyCardList">
        {visibleGroups.length === 0 ? (
          <p className="muted">
            {connectionStatus === 'connected'
              ? t('ui.proxies.noGroups')
              : t('ui.proxies.connectFirst')}
          </p>
        ) : (
          visibleGroups.map((g: any) => {
            const emoji = selectedNodeEmoji(g.selected)
            const selectedIso = extractNodeFlagIso(String(g.selected ?? ''))
            const normalizedType = String(g.type ?? '')
              .toLowerCase()
              .replace(/-/g, '')
            const isAutoGroup =
              normalizedType === 'urltest' ||
              normalizedType === 'fallback' ||
              normalizedType === 'loadbalance'
            const pingAllKey = `__all_${String(g.name)}`
            const groupProxies = filterProxyNodesForDisplay(
              (g.proxies ?? []) as string[],
              showBuiltin,
              String(g.selected ?? ''),
            )
            const isExpanded = expandedGroups[String(g.name)]
            return (
              <div key={g.name} className="proxyGroupCard">
                <div className="proxyGroupHead">
                  <div className="proxyGroupHeadMain">
                    <div className="proxyGroupTitle">
                      {emoji ? (
                        <span className="proxyGroupFlag">{emoji}</span>
                      ) : null}
                      <span>{g.name}</span>
                    </div>
                    <div className="proxyGroupMeta">
                      <span className="proxyTypeChip">{g.type}</span>
                      <span className="proxyCountChip">
                        {(g.proxies ?? []).length}
                      </span>
                    </div>
                    <div
                      className="proxySelectedInline"
                      title={String(g.selected ?? '')}
                    >
                      <FlagMark iso2={selectedIso} width={14} height={10} />
                      <span className="proxySelectedName">
                        {nodeDisplayName(String(g.selected ?? ''))}
                      </span>
                      <div className="proxyNodeTags">
                        {nodeFeatureTags(String(g.selected ?? '')).map(
                          (tag) => (
                            <span key={tag} className="proxyNodeTag">
                              {tag}
                            </span>
                          ),
                        )}
                      </div>
                    </div>
                  </div>
                  <button
                    type="button"
                    className="btn ghost proxyExpandBtn"
                    onClick={() => onToggleExpand(String(g.name))}
                  >
                    {isExpanded ? 'Collapse' : 'Expand'}
                  </button>
                </div>
                {isExpanded ? (
                  <div className="proxyNodesGrid">
                    <div className="proxyGroupToolbar">
                      <button
                        type="button"
                        className="proxyToolbarIconBtn"
                        disabled={
                          connectionStatus !== 'connected' ||
                          Boolean(proxyDelayBusy[pingAllKey])
                        }
                        title={t('ui.proxies.pingAll')}
                        aria-label={t('ui.proxies.pingAll')}
                        onClick={() => onPingAll(String(g.name), groupProxies)}
                      >
                        <svg
                          className="proxyToolbarIcon"
                          viewBox="0 0 24 24"
                          aria-hidden
                        >
                          <path
                            fill="currentColor"
                            d="M11 21h-1l1-7H7.5c-.58 0-.57-.32-.38-.66.19-.34.05-.08.07-.12C8.48 10.94 10.42 7.54 13 3h1l-1 7h3.65c.58 0 .57.32.38.66-.19.34-.05.08-.07.12C14.52 13.06 12.58 16.46 10 21z"
                          />
                        </svg>
                      </button>
                    </div>
                    {isAutoGroup ? (
                      <p className="muted small proxyAutoGroupHint">
                        {t('ui.proxies.autoGroupHint')}
                      </p>
                    ) : null}
                    {groupProxies.map((p: string) => {
                      const active = String(g.selected ?? '') === p
                      const iso = extractNodeFlagIso(p)
                      return (
                        <button
                          key={p}
                          type="button"
                          className={`proxyNodeCard${active ? ' active' : ''}${isAutoGroup ? ' proxyNodeDisabled' : ''}`}
                          title={
                            isAutoGroup ? t('ui.proxies.autoGroupHint') : p
                          }
                          disabled={isAutoGroup}
                          onClick={() => {
                            if (!p || active || isAutoGroup) return
                            onSelectNode(g.name, p)
                          }}
                        >
                          <div className="proxyNodeTop">
                            <FlagMark iso2={iso} width={16} height={12} />
                            <span className="proxyNodeName">
                              {nodeDisplayName(p)}
                            </span>
                            <div className="proxyNodeTags">
                              {nodeFeatureTags(p).map((tag) => (
                                <span key={tag} className="proxyNodeTag">
                                  {tag}
                                </span>
                              ))}
                            </div>
                            <div className="proxyDelayBox proxyDelayBoxReadonly">
                              <span className="proxyDelayText">
                                {proxyDelayBusy[p]
                                  ? '…'
                                  : proxyDelayMap[p] > 0
                                    ? `${proxyDelayMap[p]} ms`
                                    : '—'}
                              </span>
                            </div>
                          </div>
                          {proxyDelayErr[p] ? (
                            <div className="error small">
                              {proxyDelayErr[p]}
                            </div>
                          ) : null}
                        </button>
                      )
                    })}
                  </div>
                ) : null}
              </div>
            )
          })
        )}
      </div>
      {error ? <p className="error">{friendlyErrorMessage(error)}</p> : null}
    </div>
  )
}
