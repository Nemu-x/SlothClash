import { type CSSProperties, useEffect, useRef, useState } from 'react'
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
// Map to a short label so the chip stays compact; full message stays in title=.
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

// Latency bands → quality class for at-a-glance colouring.
function delayBand(ms: number): 'good' | 'ok' | 'slow' | '' {
  if (ms <= 0) return ''
  if (ms < 120) return 'good'
  if (ms < 300) return 'ok'
  return 'slow'
}

// Bar fill (%) by band — a quick visual on top of the colour.
function delayBarPct(ms: number): number {
  const b = delayBand(ms)
  if (b === 'good') return 100
  if (b === 'ok') return 66
  if (b === 'slow') return 33
  return 0
}

type GroupHealth = { alive: number; total: number; best: number }

function groupHealth(
  proxies: string[],
  delayMap: Record<string, number>,
  delayErr: Record<string, string>,
): GroupHealth {
  let alive = 0
  let best = 0
  for (const p of proxies) {
    const d = delayMap[p] ?? 0
    if (d > 0 && !delayErr[p]) {
      alive++
      if (best === 0 || d < best) best = d
    }
  }
  return { alive, total: proxies.length, best }
}

function fastestNode(
  proxies: string[],
  delayMap: Record<string, number>,
  delayErr: Record<string, string>,
): string {
  let best = ''
  let bestD = Number.POSITIVE_INFINITY
  for (const p of proxies) {
    const d = delayMap[p] ?? 0
    if (d > 0 && !delayErr[p] && d < bestD) {
      bestD = d
      best = p
    }
  }
  return best
}

type SortMode = 'default' | 'latency' | 'name'
type Density = 'comfortable' | 'compact'

const LS = {
  open: 'slothProxies.open',
  sort: 'slothProxies.sort',
  hideDead: 'slothProxies.hideDead',
  density: 'slothProxies.density',
}

function lsGet(key: string, fallback: string): string {
  try {
    return localStorage.getItem(key) ?? fallback
  } catch {
    return fallback
  }
}

function lsSet(key: string, value: string): void {
  try {
    localStorage.setItem(key, value)
  } catch {
    /* ignore */
  }
}

export function ProxiesPage({
  groups,
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
  const visibleGroups = groups.filter((g: any) => {
    const name = String(g?.name ?? '')
    if (!showBuiltin) {
      if (isUnsafeGroupName(name)) return false
      if (displayMode === 'rule' && name.toUpperCase() === 'GLOBAL')
        return false
    }
    return true
  })

  return (
    <div className="panel proxiesPanel">
      <h2>{t('ui.proxies.title')}</h2>
      <p className="muted small proxiesLead">{t('ui.proxies.lead')}</p>
      <div className="proxyToolbar">
        <div
          className="segmentInset segmentInset2 proxyModeInset"
          role="group"
          aria-label={t('ui.proxies.proxyModeAria')}
        >
          <div
            className="segmentGlider"
            aria-hidden
            style={
              { '--seg-i': displayMode === 'rule' ? 0 : 1 } as CSSProperties
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
        <div className="proxyToolbarActions">
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
      </div>
      <ProxyGroupsAccordion
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

type AccordionProps = {
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
 * Single-open accordion: groups are full-width rows; clicking one expands its
 * nodes inline. Adds a group quick-filter, per-group health (alive/total +
 * best latency), auto-test + "select fastest", latency bars, density toggle,
 * and persists the open group / sort / filters. "Our own" take on the
 * FlClashX / mobile accordion — not a 1:1 clash-verge clone.
 */
function ProxyGroupsAccordion({
  visibleGroups,
  connectionStatus,
  showBuiltin,
  proxyDelayBusy,
  proxyDelayMap,
  proxyDelayErr,
  onSelectNode,
  onPingAll,
}: AccordionProps) {
  const { t } = useTranslation()
  const [groupSearch, setGroupSearch] = useState('')
  const [openName, setOpenName] = useState(() => lsGet(LS.open, ''))
  const [nodeSearch, setNodeSearch] = useState('')
  const [sortMode, setSortMode] = useState<SortMode>(
    () => lsGet(LS.sort, 'default') as SortMode,
  )
  const [hideDead, setHideDead] = useState(
    () => lsGet(LS.hideDead, '0') === '1',
  )
  const [density, setDensity] = useState<Density>(
    () => lsGet(LS.density, 'compact') as Density,
  )
  const autoTestedRef = useRef<Set<string>>(new Set())
  const activeCardRef = useRef<HTMLButtonElement | null>(null)

  const connected = connectionStatus === 'connected'

  useEffect(() => lsSet(LS.open, openName), [openName])
  useEffect(() => lsSet(LS.sort, sortMode), [sortMode])
  useEffect(() => lsSet(LS.hideDead, hideDead ? '1' : '0'), [hideDead])
  useEffect(() => lsSet(LS.density, density), [density])

  // Auto-test the open group's nodes once, if they have no delays yet — so
  // opening a group surfaces live latencies without a manual ping.
  useEffect(() => {
    if (!openName || !connected) return
    if (autoTestedRef.current.has(openName)) return
    const g = visibleGroups.find((x: any) => String(x.name ?? '') === openName)
    if (!g) return
    const ps = filterProxyNodesForDisplay(
      (g.proxies ?? []) as string[],
      showBuiltin,
      String(g.selected ?? ''),
    )
    if (ps.length === 0) return
    const anyMeasured = ps.some(
      (p) => (proxyDelayMap[p] ?? 0) > 0 || proxyDelayErr[p],
    )
    if (!anyMeasured) {
      autoTestedRef.current.add(openName)
      onPingAll(openName, ps)
    }
  }, [
    openName,
    connected,
    visibleGroups,
    showBuiltin,
    proxyDelayMap,
    proxyDelayErr,
    onPingAll,
  ])

  // Scroll the selected node into view when a group opens.
  useEffect(() => {
    activeCardRef.current?.scrollIntoView({ block: 'nearest' })
  }, [openName])

  if (visibleGroups.length === 0) {
    return (
      <p className="muted">
        {connected ? t('ui.proxies.noGroups') : t('ui.proxies.connectFirst')}
      </p>
    )
  }

  const gq = groupSearch.trim().toLowerCase()
  const shownGroups = gq
    ? visibleGroups.filter((g: any) =>
        String(g.name ?? '')
          .toLowerCase()
          .includes(gq),
      )
    : visibleGroups

  const sortNodes = (nodes: string[]): string[] => {
    if (sortMode === 'name') {
      return [...nodes].sort((a, b) =>
        nodeDisplayName(a).localeCompare(nodeDisplayName(b)),
      )
    }
    if (sortMode === 'latency') {
      const rank = (p: string): number => {
        if (proxyDelayErr[p]) return Number.MAX_SAFE_INTEGER
        const d = proxyDelayMap[p] ?? 0
        return d > 0 ? d : Number.MAX_SAFE_INTEGER - 1
      }
      return [...nodes].sort((a, b) => rank(a) - rank(b))
    }
    return nodes
  }

  return (
    <div className="proxyAccordion">
      <div className="proxyAccFilterRow">
        <input
          type="text"
          value={groupSearch}
          onChange={(e) => setGroupSearch(e.target.value)}
          placeholder={t('ui.proxies.searchGroup')}
          className="inputModern proxyAccGroupSearch"
          aria-label={t('ui.proxies.searchGroup')}
        />
      </div>
      <div className="proxyAccList">
        {shownGroups.map((g: any) => {
          const name = String(g.name ?? '')
          const isOpen = name === openName
          const selected = String(g.selected ?? '')
          const type = String(g.type ?? '')
            .toLowerCase()
            .replace(/-/g, '')
          const isAuto =
            type === 'urltest' || type === 'fallback' || type === 'loadbalance'
          const allProxies = filterProxyNodesForDisplay(
            (g.proxies ?? []) as string[],
            showBuiltin,
            selected,
          )
          const pingKey = `__all_${name}`
          const selectedIso = extractNodeFlagIso(selected)
          const health = groupHealth(allProxies, proxyDelayMap, proxyDelayErr)

          let nodes = allProxies
          if (isOpen) {
            const q = nodeSearch.trim().toLowerCase()
            if (q) {
              nodes = nodes.filter(
                (p) =>
                  nodeDisplayName(p).toLowerCase().includes(q) ||
                  p.toLowerCase().includes(q),
              )
            }
            if (hideDead) nodes = nodes.filter((p) => !proxyDelayErr[p])
            nodes = sortNodes(nodes)
          }

          return (
            <div className={`proxyAccGroup${isOpen ? ' open' : ''}`} key={name}>
              <button
                type="button"
                className="proxyAccHeader"
                aria-expanded={isOpen}
                onClick={() => setOpenName(isOpen ? '' : name)}
              >
                <span className="proxyAccName">{name}</span>
                <span className={`proxyTypeChip proxyType-${type}`}>
                  {g.type}
                </span>
                <span className="proxyCountChip">
                  {(g.proxies ?? []).length}
                </span>
                {health.best > 0 ? (
                  <span
                    className="proxyAccHealth"
                    title={t('ui.proxies.healthTitle')}
                  >
                    {health.alive}/{health.total} · {health.best} ms
                  </span>
                ) : null}
                <span className="proxyAccCurrent">
                  <FlagMark iso2={selectedIso} width={12} height={9} />
                  <span className="proxyAccCurrentName">
                    {nodeDisplayName(selected) || '—'}
                  </span>
                </span>
                <svg
                  className="proxyAccChevron"
                  viewBox="0 0 24 24"
                  aria-hidden
                >
                  <path
                    fill="none"
                    stroke="currentColor"
                    strokeWidth="2"
                    strokeLinecap="round"
                    strokeLinejoin="round"
                    d="M6 9l6 6 6-6"
                  />
                </svg>
              </button>

              {isOpen ? (
                <div className="proxyAccBody">
                  <div className="proxyAccActions">
                    <button
                      type="button"
                      className="btn ghost proxyAccAction"
                      disabled={!connected || Boolean(proxyDelayBusy[pingKey])}
                      title={t('ui.proxies.pingAll')}
                      onClick={() => onPingAll(name, allProxies)}
                    >
                      <span className="proxyAccActionEmoji" aria-hidden>
                        📶
                      </span>
                      {t('ui.proxies.pingAllShort')}
                    </button>
                    {!isAuto ? (
                      <button
                        type="button"
                        className="btn ghost proxyAccAction proxyAccFastest"
                        title={t('ui.proxies.selectFastest')}
                        disabled={
                          !fastestNode(allProxies, proxyDelayMap, proxyDelayErr)
                        }
                        onClick={() => {
                          const f = fastestNode(
                            allProxies,
                            proxyDelayMap,
                            proxyDelayErr,
                          )
                          if (f && f !== selected) onSelectNode(name, f)
                        }}
                      >
                        <span className="proxyAccActionEmoji" aria-hidden>
                          ⚡
                        </span>
                        {t('ui.proxies.fastest')}
                      </button>
                    ) : null}
                    <select
                      className="selectModern selectInline proxyAccSort"
                      value={sortMode}
                      onChange={(e) => setSortMode(e.target.value as SortMode)}
                      aria-label={t('ui.proxies.sortLabel')}
                    >
                      <option value="default">
                        {t('ui.proxies.sortDefault')}
                      </option>
                      <option value="latency">
                        {t('ui.proxies.sortLatency')}
                      </option>
                      <option value="name">{t('ui.proxies.sortName')}</option>
                    </select>
                    <button
                      type="button"
                      className={`btn ghost proxyAccHideDead${hideDead ? ' isOn' : ''}`}
                      aria-pressed={hideDead}
                      onClick={() => setHideDead((v) => !v)}
                    >
                      {t('ui.proxies.hideDead')}
                    </button>
                    <button
                      type="button"
                      className="btn ghost proxyAccDensity"
                      onClick={() =>
                        setDensity((d) =>
                          d === 'compact' ? 'comfortable' : 'compact',
                        )
                      }
                      title={t('ui.proxies.density')}
                    >
                      {density === 'compact'
                        ? t('ui.proxies.densityComfortable')
                        : t('ui.proxies.densityCompact')}
                    </button>
                    <input
                      type="text"
                      value={nodeSearch}
                      onChange={(e) => setNodeSearch(e.target.value)}
                      placeholder={t('ui.proxies.searchNode')}
                      className="inputModern proxySplitNodeSearch proxyAccSearch"
                      aria-label={t('ui.proxies.searchNode')}
                    />
                  </div>

                  {isAuto ? (
                    <p className="muted small proxyAutoGroupHint">
                      {t('ui.proxies.autoGroupHint')}
                    </p>
                  ) : null}

                  <div
                    className={`proxyNodesGrid${density === 'compact' ? ' compact' : ''}`}
                  >
                    {nodes.map((p: string) => {
                      const active = selected === p
                      const iso = extractNodeFlagIso(p)
                      const rawErr = proxyDelayErr[p]
                      const shortErr = shortProxyDelayError(rawErr)
                      const ms = proxyDelayMap[p] ?? 0
                      const band = delayBand(ms)
                      const titleParts: string[] = []
                      if (isAuto) titleParts.push(t('ui.proxies.autoGroupHint'))
                      else titleParts.push(p)
                      if (rawErr) titleParts.push(String(rawErr))
                      return (
                        <button
                          key={p}
                          type="button"
                          ref={active ? activeCardRef : undefined}
                          className={`proxyNodeCard${active ? ' active' : ''}${isAuto ? ' proxyNodeDisabled' : ''}`}
                          title={titleParts.join(' — ')}
                          disabled={isAuto}
                          onClick={() => {
                            if (!p || active || isAuto) return
                            onSelectNode(name, p)
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
                                className={`proxyDelayText${shortErr ? ' proxyDelayFail' : band ? ` proxyDelay${band[0].toUpperCase()}${band.slice(1)}` : ''}`}
                              >
                                {proxyDelayBusy[p]
                                  ? '…'
                                  : shortErr
                                    ? shortErr
                                    : ms > 0
                                      ? `${ms} ms`
                                      : '—'}
                              </span>
                            </div>
                          </div>
                          {band ? (
                            <span
                              className={`proxyDelayBar proxyDelayBar${band[0].toUpperCase()}${band.slice(1)}`}
                              aria-hidden
                            >
                              <span
                                className="proxyDelayBarFill"
                                style={{ width: `${delayBarPct(ms)}%` }}
                              />
                            </span>
                          ) : null}
                        </button>
                      )
                    })}
                    {nodes.length === 0 ? (
                      <p className="muted small">
                        {t('ui.proxies.noNodeMatch')}
                      </p>
                    ) : null}
                  </div>
                </div>
              ) : null}
            </div>
          )
        })}
        {shownGroups.length === 0 ? (
          <p className="muted small proxyAccEmpty">
            {t('ui.proxies.noGroupMatch')}
          </p>
        ) : null}
      </div>
    </div>
  )
}
