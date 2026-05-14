import { type CSSProperties, useMemo, useState } from 'react'
import { useTranslation } from 'react-i18next'

import { FlagMark } from '../components/FlagMark'
import {
  extractNodeFlagIso,
  filterProxyNodesForDisplay,
  isUnsafeGroupName,
  nodeDisplayName,
  nodeFeatureTags,
} from '../utils/proxyNames'
import { friendlyErrorMessage } from '../utils/yaml'

// Mihomo /proxies/{name}/delay surfaces a handful of recurring failure modes.
// Inline-rendering the raw error string blew the chip layout (the URL alone
// is ~70 chars). Map to a short label so the chip stays compact; the full
// message is preserved in title= for hover/inspection.
function shortProxyDelayError(raw: string | undefined): string {
  if (!raw) return ''
  const s = String(raw)
  const httpMatch = /HTTP\s+(\d{3})/i.exec(s)
  if (httpMatch) return httpMatch[1]
  if (/context deadline exceeded|timeout/i.test(s)) return 'timeout'
  if (/no delay value/i.test(s)) return 'no reply'
  if (/connection refused|no such host|network is unreachable/i.test(s)) {
    return 'no route'
  }
  if (/EOF|broken pipe|reset by peer/i.test(s)) return 'broken'
  return 'fail'
}

export function ProxiesPage({
  groups,
  activeGroup,
  connectionStatus,
  displayMode,
  showBuiltin,
  proxyDelayBusy,
  proxyDelayMap,
  proxyDelayErr,
  error,
  onRefreshProxies,
  onToggleShowBuiltin,
  onSetMode,
  onSelectGroup,
  onSelectNode,
  onPingAll,
}: {
  groups: any[]
  activeGroup: string
  connectionStatus: string
  displayMode: string
  showBuiltin: boolean
  proxyDelayBusy: Record<string, boolean>
  proxyDelayMap: Record<string, number>
  proxyDelayErr: Record<string, string>
  error: string | null
  onRefreshProxies: () => void
  onToggleShowBuiltin: () => void
  onSetMode: (mode: 'rule' | 'global') => void
  onSelectGroup: (name: string) => void
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
          {visibleGroups.map((g: any) => (
            <option key={g.name} value={g.name}>
              {`${g.name} (${g.type})`}
            </option>
          ))}
        </select>
      </div>
      <ProxyGroupsSplit
        visibleGroups={visibleGroups}
        connectionStatus={connectionStatus}
        showBuiltin={showBuiltin}
        proxyDelayBusy={proxyDelayBusy}
        proxyDelayMap={proxyDelayMap}
        proxyDelayErr={proxyDelayErr}
        onSelectNode={onSelectNode}
        onPingAll={onPingAll}
      />
      {error ? <p className="error">{friendlyErrorMessage(error)}</p> : null}
    </div>
  )
}

type SplitProps = {
  visibleGroups: any[]
  connectionStatus: string
  showBuiltin: boolean
  proxyDelayBusy: Record<string, boolean>
  proxyDelayMap: Record<string, number>
  proxyDelayErr: Record<string, string>
  onSelectNode: (group: string, node: string) => void
  onPingAll: (group: string, nodes: string[]) => void
}

/**
 * Two-pane proxy groups layout (upstream Verge-Rev pattern). Left rail holds
 * a dense list of groups with their selected node summary so the user can
 * eyeball the whole topology at once; right pane focuses on the active
 * group's nodes with its own search. Scales gracefully from 1 group to 50+
 * without the page turning into an endless scroll of expanded cards.
 */
function ProxyGroupsSplit({
  visibleGroups,
  connectionStatus,
  showBuiltin,
  proxyDelayBusy,
  proxyDelayMap,
  proxyDelayErr,
  onSelectNode,
  onPingAll,
}: SplitProps) {
  const { t } = useTranslation()
  const [groupSearch, setGroupSearch] = useState('')
  const [nodeSearch, setNodeSearch] = useState('')
  const [focusedName, setFocusedName] = useState<string>('')

  const groupList = useMemo(() => {
    const q = groupSearch.trim().toLowerCase()
    if (!q) return visibleGroups
    return visibleGroups.filter((g: any) =>
      String(g.name ?? '')
        .toLowerCase()
        .includes(q),
    )
  }, [visibleGroups, groupSearch])

  // Derive the effective focused group during render. This avoids a setState
  // effect (which the React lint flags) by treating focusedName as a "hint"
  // and falling back to the first available group when the hint no longer
  // matches anything in the filtered list.
  const effectiveFocused = useMemo(() => {
    if (groupList.length === 0) return ''
    if (
      focusedName &&
      groupList.some((g: any) => String(g.name ?? '') === focusedName)
    ) {
      return focusedName
    }
    return String(groupList[0]?.name ?? '')
  }, [groupList, focusedName])

  const focused = useMemo(
    () =>
      visibleGroups.find((g: any) => String(g.name ?? '') === effectiveFocused),
    [visibleGroups, effectiveFocused],
  )

  if (visibleGroups.length === 0) {
    return (
      <p className="muted">
        {connectionStatus === 'connected'
          ? t('ui.proxies.noGroups')
          : t('ui.proxies.connectFirst')}
      </p>
    )
  }

  const focusedProxies: string[] = focused
    ? filterProxyNodesForDisplay(
        (focused.proxies ?? []) as string[],
        showBuiltin,
        String(focused.selected ?? ''),
      )
    : []
  const focusedType = String(focused?.type ?? '')
    .toLowerCase()
    .replace(/-/g, '')
  const focusedIsAuto =
    focusedType === 'urltest' ||
    focusedType === 'fallback' ||
    focusedType === 'loadbalance'
  const focusedPingKey = focused ? `__all_${String(focused.name)}` : ''
  const focusedNodeQ = nodeSearch.trim().toLowerCase()
  const filteredNodes = focusedNodeQ
    ? focusedProxies.filter(
        (p) =>
          nodeDisplayName(p).toLowerCase().includes(focusedNodeQ) ||
          p.toLowerCase().includes(focusedNodeQ),
      )
    : focusedProxies

  return (
    <div className="proxySplit">
      <aside className="proxySplitSidebar">
        <div className="proxySplitSearchRow">
          <input
            type="text"
            value={groupSearch}
            onChange={(e) => setGroupSearch(e.target.value)}
            placeholder={t('ui.proxies.searchGroup')}
            className="inputModern"
            aria-label={t('ui.proxies.searchGroup')}
          />
        </div>
        <ul className="proxySplitGroupList" role="listbox">
          {groupList.map((g: any) => {
            const name = String(g.name ?? '')
            const selectedIso = extractNodeFlagIso(String(g.selected ?? ''))
            const isFocused = name === effectiveFocused
            return (
              <li key={name}>
                <button
                  type="button"
                  className={`proxySplitGroupItem${isFocused ? ' active' : ''}`}
                  onClick={() => setFocusedName(name)}
                  role="option"
                  aria-selected={isFocused}
                >
                  <div className="proxySplitGroupItemTop">
                    <span className="proxySplitGroupName">{name}</span>
                    <span className="proxyCountChip">
                      {(g.proxies ?? []).length}
                    </span>
                  </div>
                  <div className="proxySplitGroupItemBottom">
                    <span className="proxyTypeChip">{g.type}</span>
                    <FlagMark iso2={selectedIso} width={12} height={9} />
                    <span className="proxySplitGroupSelectedName">
                      {nodeDisplayName(String(g.selected ?? '')) || '—'}
                    </span>
                  </div>
                </button>
              </li>
            )
          })}
          {groupList.length === 0 ? (
            <li className="muted small proxySplitEmpty">
              {t('ui.proxies.noGroupMatch')}
            </li>
          ) : null}
        </ul>
      </aside>

      <section className="proxySplitDetail">
        {!focused ? (
          <p className="muted">{t('ui.proxies.pickGroupHint')}</p>
        ) : (
          <>
            <div className="proxySplitDetailHeader">
              <div className="proxySplitDetailTitle">
                <span className="proxySplitGroupName">
                  {String(focused.name)}
                </span>
                <span className="proxyTypeChip">{focused.type}</span>
                <span className="proxyCountChip">
                  {(focused.proxies ?? []).length}
                </span>
              </div>
              <div className="proxySplitDetailTools">
                <input
                  type="text"
                  value={nodeSearch}
                  onChange={(e) => setNodeSearch(e.target.value)}
                  placeholder={t('ui.proxies.searchNode')}
                  className="inputModern proxySplitNodeSearch"
                  aria-label={t('ui.proxies.searchNode')}
                />
                <button
                  type="button"
                  className="proxyToolbarIconBtn"
                  disabled={
                    connectionStatus !== 'connected' ||
                    Boolean(proxyDelayBusy[focusedPingKey])
                  }
                  title={t('ui.proxies.pingAll')}
                  aria-label={t('ui.proxies.pingAll')}
                  onClick={() =>
                    onPingAll(String(focused.name), focusedProxies)
                  }
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
            </div>

            {focusedIsAuto ? (
              <p className="muted small proxyAutoGroupHint">
                {t('ui.proxies.autoGroupHint')}
              </p>
            ) : null}

            <div className="proxyNodesGrid proxySplitNodesGrid">
              {filteredNodes.map((p: string) => {
                const active = String(focused.selected ?? '') === p
                const iso = extractNodeFlagIso(p)
                const rawErr = proxyDelayErr[p]
                const shortErr = shortProxyDelayError(rawErr)
                const titleParts: string[] = []
                if (focusedIsAuto)
                  titleParts.push(t('ui.proxies.autoGroupHint'))
                else titleParts.push(p)
                if (rawErr) titleParts.push(String(rawErr))
                return (
                  <button
                    key={p}
                    type="button"
                    className={`proxyNodeCard${active ? ' active' : ''}${focusedIsAuto ? ' proxyNodeDisabled' : ''}`}
                    title={titleParts.join(' — ')}
                    disabled={focusedIsAuto}
                    onClick={() => {
                      if (!p || active || focusedIsAuto) return
                      onSelectNode(String(focused.name), p)
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
                        <span
                          className={`proxyDelayText${shortErr ? ' proxyDelayFail' : ''}`}
                        >
                          {proxyDelayBusy[p]
                            ? '…'
                            : shortErr
                              ? shortErr
                              : proxyDelayMap[p] > 0
                                ? `${proxyDelayMap[p]} ms`
                                : '—'}
                        </span>
                      </div>
                    </div>
                  </button>
                )
              })}
              {filteredNodes.length === 0 ? (
                <p className="muted small">{t('ui.proxies.noNodeMatch')}</p>
              ) : null}
            </div>
          </>
        )}
      </section>
    </div>
  )
}
