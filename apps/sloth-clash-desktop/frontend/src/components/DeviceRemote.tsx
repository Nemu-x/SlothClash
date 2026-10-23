import { useCallback, useEffect, useMemo, useRef, useState } from 'react'
import { useTranslation } from 'react-i18next'

import {
  CompanionActions,
  CompanionCloseConnections,
  CompanionConnections,
  CompanionDeleteProfile,
  CompanionHealthCheckProvider,
  CompanionLogs,
  CompanionMode,
  CompanionPower,
  CompanionProfiles,
  CompanionProviders,
  CompanionProxies,
  CompanionRename,
  CompanionRunAction,
  CompanionSelectProfile,
  CompanionSelectProxy,
  CompanionSetMode,
  CompanionSetSetting,
  CompanionSettings,
  CompanionShareSubscription,
  CompanionStatus,
  CompanionTestDelay,
  CompanionTraffic,
  CompanionUnfixProxy,
  CompanionUnpair,
  CompanionUnwatchDevice,
  CompanionUpdateProfile,
  CompanionUpdateProvider,
  CompanionWatchDevice,
} from '../api/companion'
import type { companion } from '../api/models'
import { EventsOn } from '../api/runtime'
import { formatBytesSmart } from '../utils/format'

/** One event of the agent's stream (Go: companion.EventView). */
type DeviceEvent = {
  deviceId: string
  type: string
  resource?: string
  status?: companion.StatusView
  traffic?: companion.TrafficView
  log?: companion.LogLineView
  error?: string
}

type TFn = ReturnType<typeof useTranslation>['t']

/** Tabs in display order; each shows only when the agent advertises its capability. */
const TABS = [
  'proxies',
  'mode',
  'profiles',
  'providers',
  'connections',
  'traffic',
  'logs',
  'settings',
  'actions',
] as const
type Tab = (typeof TABS)[number]

const LOG_LEVELS = ['error', 'warning', 'info', 'debug']
const TRAFFIC_POINTS = 60
const STREAM_LOG_CAP = 500

/** Agent errors read "[code] message" (companion.RemoteError). */
function remoteErrorText(e: unknown, t: TFn): string {
  const raw =
    String((e as { message?: string })?.message ?? e ?? '').trim() ||
    'unknown error'
  const m = raw.match(/^\[([a-z_]+)\]\s*([\s\S]*)$/)
  if (!m) return raw
  const label = t(`ui.devices.remote.err.${m[1]}`, { defaultValue: m[1] })
  return m[2] ? `${label}: ${m[2]}` : label
}

function severity(level: string): number {
  const i = LOG_LEVELS.indexOf(level)
  return i < 0 ? LOG_LEVELS.length : i
}

function formatTime(ms: number): string {
  if (!ms) return ''
  return new Date(ms).toLocaleTimeString()
}

function formatUnix(sec: number): string {
  if (!sec) return '—'
  return new Date(sec * 1000).toLocaleString()
}

/** Loads data for a tab and reloads it whenever `version` changes. */
function useRemote<T>(
  load: () => Promise<T>,
  version: number | string,
): { data: T | null; error: string; reload: () => void; t: TFn } {
  const { t } = useTranslation()
  const [data, setData] = useState<T | null>(null)
  const [error, setError] = useState('')
  const [tick, setTick] = useState(0)
  const loadRef = useRef(load)
  loadRef.current = load
  // biome-ignore lint/correctness/useExhaustiveDependencies: version and tick are reload triggers
  useEffect(() => {
    let alive = true
    loadRef
      .current()
      .then((d) => {
        if (!alive) return
        setData(d)
        setError('')
      })
      .catch((e) => alive && setError(remoteErrorText(e, t)))
    return () => {
      alive = false
    }
  }, [version, tick, t])
  return { data, error, reload: () => setTick((n) => n + 1), t }
}

export function DeviceRemote({
  agent,
  onAgentsChanged,
  onForgotten,
}: {
  agent: companion.AgentInfo
  onAgentsChanged: () => void
  onForgotten: () => void
}) {
  const { t } = useTranslation()
  const id = agent.deviceId
  const [status, setStatus] = useState<companion.StatusView | null>(null)
  const [stream, setStream] = useState<
    'connecting' | 'open' | 'down' | 'revoked'
  >('connecting')
  const [versions, setVersions] = useState<Record<string, number>>({})
  const [traffic, setTraffic] = useState<companion.TrafficView[]>([])
  const [streamLogs, setStreamLogs] = useState<companion.LogLineView[]>([])
  const [tab, setTab] = useState<Tab | ''>('')
  const [busy, setBusy] = useState(false)
  const [error, setError] = useState('')
  const [notice, setNotice] = useState('')
  const [renameVal, setRenameVal] = useState('')
  const [confirmForget, setConfirmForget] = useState(false)

  const bump = useCallback((resource?: string) => {
    setVersions((v) => {
      if (!resource) {
        const next: Record<string, number> = { all: (v.all ?? 0) + 1 }
        for (const k of Object.keys(v)) next[k] = (v[k] ?? 0) + 1
        return next
      }
      return { ...v, [resource]: (v[resource] ?? 0) + 1 }
    })
  }, [])
  const versionOf = (resource: string) =>
    (versions[resource] ?? 0) + (versions.all ?? 0)

  const refreshStatus = useCallback(async () => {
    try {
      setStatus(await CompanionStatus(id))
    } catch (e) {
      setError(remoteErrorText(e, t))
    }
  }, [id, t])

  // Status on open, then the event stream for as long as this page shows the
  // device. The parent keys this component by deviceId, so a different device
  // starts from fresh state.
  useEffect(() => {
    void refreshStatus()
    const off = EventsOn('companion:event', (payload: unknown) => {
      const e = payload as DeviceEvent
      if (!e || e.deviceId !== id) return
      switch (e.type) {
        case 'open':
          // Events missed while down are not replayed: refetch everything shown.
          setStream('open')
          bump()
          void refreshStatus()
          break
        case 'down':
          setStream('down')
          break
        case 'unauthorized':
          setStream('revoked')
          break
        case 'status':
          if (e.status) setStatus(e.status)
          break
        case 'changed':
          if (e.resource) bump(e.resource)
          break
        case 'traffic':
          if (e.traffic) {
            const point = e.traffic
            setTraffic((s) => [...s.slice(-(TRAFFIC_POINTS - 1)), point])
          }
          break
        case 'log':
          if (e.log) {
            const line = e.log
            setStreamLogs((s) => [...s.slice(-(STREAM_LOG_CAP - 1)), line])
          }
          break
        default:
        // Unknown event types are ignored (PROTOCOL.md §9.2).
      }
    })
    void CompanionWatchDevice(id).catch(() => setStream('down'))
    return () => {
      off()
      void CompanionUnwatchDevice(id)
    }
  }, [id, bump, refreshStatus])

  const caps = useMemo(() => new Set(status?.capabilities ?? []), [status])
  const tabs = useMemo(() => TABS.filter((x) => caps.has(x)), [caps])
  const activeTab: Tab | '' = tab && tabs.includes(tab) ? tab : (tabs[0] ?? '')

  const run = useCallback(
    async (fn: () => Promise<void>) => {
      setBusy(true)
      setError('')
      setNotice('')
      try {
        await fn()
      } catch (e) {
        setError(remoteErrorText(e, t))
      } finally {
        setBusy(false)
      }
    },
    [t],
  )

  const summary = status?.summary
  const summaryParts = [
    summary?.mode
      ? t(`ui.common.${summary.mode}`, { defaultValue: summary.mode })
      : '',
    summary?.profile ?? '',
    summary?.proxy ?? '',
    summary?.appVersion
      ? `${status?.app ?? ''} ${summary.appVersion}`.trim()
      : '',
    summary?.coreVersion ? `core ${summary.coreVersion}` : '',
  ].filter(Boolean)

  return (
    <div className="deviceRemote">
      <div className="deviceRemoteHead">
        <div>
          <h3>{agent.name || agent.deviceId}</h3>
          {summaryParts.length ? (
            <p className="muted small deviceRemoteSummary">
              {summaryParts.join(' · ')}
            </p>
          ) : null}
        </div>
        <span className={`pill deviceStream deviceStream-${stream}`}>
          {t(`ui.devices.remote.stream.${stream}`)}
        </span>
      </div>

      {error ? <p className="error">{error}</p> : null}
      {notice ? <p className="muted small">{notice}</p> : null}
      {stream === 'revoked' ? (
        <p className="error">{t('ui.devices.remote.revoked')}</p>
      ) : null}

      <div className="devicesStatusRow">
        <span className="muted">{t('ui.devices.power')}</span>
        <strong>{status?.power ?? '—'}</strong>
      </div>
      <div className="devicesActions">
        <button
          type="button"
          className="btn"
          disabled={busy || status?.power === 'on'}
          onClick={() =>
            run(async () => {
              await CompanionPower(id, 'on')
              await refreshStatus()
            })
          }
        >
          {t('ui.devices.powerOn')}
        </button>
        <button
          type="button"
          className="btn subtle"
          disabled={busy || status?.power === 'off'}
          onClick={() =>
            run(async () => {
              await CompanionPower(id, 'off')
              await refreshStatus()
            })
          }
        >
          {t('ui.devices.powerOff')}
        </button>
        <button
          type="button"
          className="btn subtle"
          disabled={busy}
          onClick={() =>
            run(async () => {
              await CompanionShareSubscription(id)
              setNotice(t('ui.devices.shared'))
            })
          }
        >
          {t('ui.devices.share')}
        </button>
        <button
          type="button"
          className="btn subtle"
          disabled={busy}
          onClick={() =>
            run(async () => {
              await refreshStatus()
              bump()
            })
          }
        >
          {t('ui.devices.refresh')}
        </button>
      </div>

      {tabs.length ? (
        <>
          <div className="deviceTabs" role="tablist">
            {tabs.map((x) => (
              <button
                key={x}
                type="button"
                role="tab"
                aria-selected={x === activeTab}
                className={`deviceTab${x === activeTab ? ' active' : ''}`}
                onClick={() => setTab(x)}
              >
                {t(`ui.devices.remote.tab.${x}`)}
              </button>
            ))}
          </div>
          <div className="deviceTabBody">
            {activeTab === 'proxies' ? (
              <ProxiesTab id={id} caps={caps} version={versionOf('proxies')} />
            ) : null}
            {activeTab === 'mode' ? (
              <ModeTab id={id} version={versionOf('mode')} />
            ) : null}
            {activeTab === 'profiles' ? (
              <ProfilesTab
                id={id}
                caps={caps}
                version={versionOf('profiles')}
              />
            ) : null}
            {activeTab === 'providers' ? (
              <ProvidersTab id={id} version={versionOf('providers')} />
            ) : null}
            {activeTab === 'connections' ? (
              <ConnectionsTab id={id} version={versionOf('connections')} />
            ) : null}
            {activeTab === 'traffic' ? (
              <TrafficTab
                id={id}
                live={caps.has('events') && stream === 'open'}
                series={traffic}
              />
            ) : null}
            {activeTab === 'logs' ? (
              <LogsTab
                id={id}
                version={versionOf('logs')}
                streamed={streamLogs}
              />
            ) : null}
            {activeTab === 'settings' ? (
              <SettingsTab id={id} version={versionOf('settings')} />
            ) : null}
            {activeTab === 'actions' ? <ActionsTab id={id} /> : null}
          </div>
        </>
      ) : null}

      <div className="devicesPairRow">
        <input
          className="input"
          placeholder={t('ui.devices.renamePlaceholder')}
          value={renameVal}
          onChange={(e) => setRenameVal(e.target.value)}
        />
        <button
          type="button"
          className="btn subtle"
          disabled={busy || !renameVal.trim()}
          onClick={() =>
            run(async () => {
              await CompanionRename(id, renameVal)
              setRenameVal('')
              onAgentsChanged()
              setNotice(t('ui.devices.renamed'))
            })
          }
        >
          {t('ui.devices.rename')}
        </button>
      </div>
      {confirmForget ? (
        <div className="devicesActions">
          <span className="muted small">
            {t('ui.devices.remote.forgetConfirm')}
          </span>
          <button
            type="button"
            className="btn subtle devicesUnpair"
            disabled={busy}
            onClick={() =>
              run(async () => {
                await CompanionUnpair(id)
                onForgotten()
              })
            }
          >
            {t('ui.devices.unpair')}
          </button>
          <button
            type="button"
            className="btn subtle"
            onClick={() => setConfirmForget(false)}
          >
            {t('ui.devices.remote.cancel')}
          </button>
        </div>
      ) : (
        <button
          type="button"
          className="btn subtle devicesUnpair"
          disabled={busy}
          onClick={() => setConfirmForget(true)}
        >
          {t('ui.devices.unpair')}
        </button>
      )}
    </div>
  )
}

/** Shared action runner for tabs: one busy flag, one error line. */
function useTabAction(t: TFn, after?: () => void) {
  const [busy, setBusy] = useState(false)
  const [error, setError] = useState('')
  const act = useCallback(
    async (fn: () => Promise<unknown>) => {
      setBusy(true)
      setError('')
      try {
        await fn()
        after?.()
      } catch (e) {
        setError(remoteErrorText(e, t))
      } finally {
        setBusy(false)
      }
    },
    [t, after],
  )
  return { busy, error, act }
}

function ProxiesTab({
  id,
  caps,
  version,
}: {
  id: string
  caps: Set<string>
  version: number
}) {
  const { data, error, reload, t } = useRemote(
    () => CompanionProxies(id),
    version,
  )
  const { busy, error: actErr, act } = useTabAction(t, reload)
  const [delays, setDelays] = useState<Record<string, number>>({})
  const canSelect = caps.has('select')
  const canDelay = caps.has('delay')
  const canUnfix = caps.has('unfix')

  const test = (group: string, node: string) =>
    act(async () => {
      const res = await CompanionTestDelay(id, group, node)
      setDelays((d) => {
        const next = { ...d }
        for (const r of res ?? []) next[r.name] = r.delay
        return next
      })
    })

  if (error) return <p className="error">{error}</p>
  if (!data) return <p className="muted small">…</p>
  return (
    <div className="deviceList">
      {actErr ? <p className="error">{actErr}</p> : null}
      {data.length === 0 ? (
        <p className="muted small">{t('ui.devices.remote.empty')}</p>
      ) : null}
      {data.map((g) => (
        <div key={g.name} className="deviceGroup">
          <div className="deviceGroupHead">
            <strong>{g.name}</strong>
            <span className="pill">{g.type}</span>
            <span className="muted small">{g.now}</span>
            {g.fixed ? (
              <span className="muted small">
                {t('ui.devices.remote.fixed', { node: g.fixed })}
              </span>
            ) : null}
            <span className="deviceGroupActions">
              {g.fixed && canUnfix ? (
                <button
                  type="button"
                  className="btn subtle"
                  disabled={busy}
                  onClick={() => act(() => CompanionUnfixProxy(id, g.name))}
                >
                  {t('ui.devices.remote.unfix')}
                </button>
              ) : null}
              {canDelay ? (
                <button
                  type="button"
                  className="btn subtle"
                  disabled={busy}
                  onClick={() => test(g.name, '')}
                >
                  {t('ui.devices.remote.pingGroup')}
                </button>
              ) : null}
            </span>
          </div>
          <div className="deviceNodes">
            {(g.nodes ?? []).map((n) => {
              const delay = delays[n.name] ?? n.delay
              const active = n.name === g.now
              const pickable = canSelect && g.selectable && !active
              return (
                <div
                  key={n.name}
                  className={`deviceNode${active ? ' active' : ''}`}
                >
                  <button
                    type="button"
                    className="deviceNodeName"
                    disabled={busy || !pickable}
                    title={n.type}
                    onClick={() =>
                      act(() => CompanionSelectProxy(id, g.name, n.name))
                    }
                  >
                    {n.name}
                  </button>
                  {canDelay ? (
                    <button
                      type="button"
                      className="deviceNodeDelay"
                      disabled={busy}
                      onClick={() => test(g.name, n.name)}
                    >
                      {delay > 0 ? `${delay} ms` : '—'}
                    </button>
                  ) : (
                    <span className="deviceNodeDelay muted">
                      {delay > 0 ? `${delay} ms` : '—'}
                    </span>
                  )}
                </div>
              )
            })}
          </div>
        </div>
      ))}
    </div>
  )
}

function ModeTab({ id, version }: { id: string; version: number }) {
  const { data, error, reload, t } = useRemote(() => CompanionMode(id), version)
  const { busy, error: actErr, act } = useTabAction(t, reload)
  if (error) return <p className="error">{error}</p>
  if (!data) return <p className="muted small">…</p>
  return (
    <div className="devicesActions">
      {actErr ? <p className="error">{actErr}</p> : null}
      {(data.modes ?? []).map((mode) => (
        <button
          key={mode}
          type="button"
          className={`btn${mode === data.mode ? '' : ' subtle'}`}
          disabled={busy || mode === data.mode}
          onClick={() => act(() => CompanionSetMode(id, mode))}
        >
          {t(`ui.common.${mode}`, { defaultValue: mode })}
        </button>
      ))}
    </div>
  )
}

function UsageLine({ usage, t }: { usage?: companion.UsageView; t: TFn }) {
  if (!usage) return null
  const used = usage.upload + usage.download
  return (
    <span className="muted small">
      {formatBytesSmart(used)}
      {usage.total > 0 ? ` / ${formatBytesSmart(usage.total)}` : ''}
      {usage.expire > 0
        ? ` · ${t('ui.devices.remote.expires', { date: formatUnix(usage.expire) })}`
        : ''}
    </span>
  )
}

function ProfilesTab({
  id,
  caps,
  version,
}: {
  id: string
  caps: Set<string>
  version: number
}) {
  const { data, error, reload, t } = useRemote(
    () => CompanionProfiles(id),
    version,
  )
  const { busy, error: actErr, act } = useTabAction(t, reload)
  const [confirmDelete, setConfirmDelete] = useState('')
  const canUpdate = caps.has('profile_update')
  const canDelete = caps.has('profile_delete')
  if (error) return <p className="error">{error}</p>
  if (!data) return <p className="muted small">…</p>
  return (
    <div className="deviceList">
      {actErr ? <p className="error">{actErr}</p> : null}
      {canUpdate ? (
        <div className="devicesActions">
          <button
            type="button"
            className="btn subtle"
            disabled={busy}
            onClick={() => act(() => CompanionUpdateProfile(id, ''))}
          >
            {t('ui.devices.remote.updateAll')}
          </button>
        </div>
      ) : null}
      {(data.profiles ?? []).map((p) => {
        const active = p.id === data.active
        return (
          <div key={p.id} className={`deviceItem${active ? ' active' : ''}`}>
            <div className="deviceItemMain">
              <strong>{p.name || p.id}</strong>
              <span className="muted small">
                {p.type} · {formatUnix(p.updatedAt)}
              </span>
              <UsageLine usage={p.usage} t={t} />
            </div>
            <div className="deviceItemActions">
              {!active ? (
                <button
                  type="button"
                  className="btn subtle"
                  disabled={busy}
                  onClick={() => act(() => CompanionSelectProfile(id, p.id))}
                >
                  {t('ui.devices.remote.use')}
                </button>
              ) : (
                <span className="pill">{t('ui.devices.remote.active')}</span>
              )}
              {canUpdate && p.type === 'remote' ? (
                <button
                  type="button"
                  className="btn subtle"
                  disabled={busy}
                  onClick={() => act(() => CompanionUpdateProfile(id, p.id))}
                >
                  {t('ui.devices.remote.update')}
                </button>
              ) : null}
              {canDelete ? (
                confirmDelete === p.id ? (
                  <button
                    type="button"
                    className="btn subtle devicesUnpair"
                    disabled={busy}
                    onClick={() =>
                      act(async () => {
                        setConfirmDelete('')
                        await CompanionDeleteProfile(id, p.id)
                      })
                    }
                  >
                    {t('ui.devices.remote.deleteConfirm')}
                  </button>
                ) : (
                  <button
                    type="button"
                    className="btn subtle"
                    disabled={busy}
                    onClick={() => setConfirmDelete(p.id)}
                  >
                    {t('ui.devices.remote.delete')}
                  </button>
                )
              ) : null}
            </div>
          </div>
        )
      })}
    </div>
  )
}

function ProvidersTab({ id, version }: { id: string; version: number }) {
  const { data, error, reload, t } = useRemote(
    () => CompanionProviders(id),
    version,
  )
  const { busy, error: actErr, act } = useTabAction(t, reload)
  if (error) return <p className="error">{error}</p>
  if (!data) return <p className="muted small">…</p>
  return (
    <div className="deviceList">
      {actErr ? <p className="error">{actErr}</p> : null}
      {data.length === 0 ? (
        <p className="muted small">{t('ui.devices.remote.empty')}</p>
      ) : null}
      {data.map((p) => (
        <div key={`${p.kind}/${p.name}`} className="deviceItem">
          <div className="deviceItemMain">
            <strong>{p.name}</strong>
            <span className="muted small">
              {p.kind} · {p.vehicle} · {p.count} · {formatUnix(p.updatedAt)}
            </span>
            <UsageLine usage={p.usage} t={t} />
          </div>
          <div className="deviceItemActions">
            {p.vehicle !== 'inline' ? (
              <button
                type="button"
                className="btn subtle"
                disabled={busy}
                onClick={() =>
                  act(() => CompanionUpdateProvider(id, p.kind, p.name))
                }
              >
                {t('ui.devices.remote.update')}
              </button>
            ) : null}
            {p.kind === 'proxy' ? (
              <button
                type="button"
                className="btn subtle"
                disabled={busy}
                onClick={() =>
                  act(() => CompanionHealthCheckProvider(id, p.name))
                }
              >
                {t('ui.devices.remote.healthcheck')}
              </button>
            ) : null}
          </div>
        </div>
      ))}
    </div>
  )
}

function ConnectionsTab({ id, version }: { id: string; version: number }) {
  const { data, error, reload, t } = useRemote(
    () => CompanionConnections(id),
    version,
  )
  const { busy, error: actErr, act } = useTabAction(t, reload)
  if (error) return <p className="error">{error}</p>
  if (!data) return <p className="muted small">…</p>
  const conns = data.connections ?? []
  return (
    <div className="deviceList">
      {actErr ? <p className="error">{actErr}</p> : null}
      <div className="devicesActions">
        <span className="muted small">
          ↑ {formatBytesSmart(data.uploadTotal)} · ↓{' '}
          {formatBytesSmart(data.downloadTotal)} · {conns.length}
        </span>
        <button
          type="button"
          className="btn subtle"
          disabled={busy}
          onClick={reload}
        >
          {t('ui.devices.refresh')}
        </button>
        <button
          type="button"
          className="btn subtle devicesUnpair"
          disabled={busy || conns.length === 0}
          onClick={() => act(() => CompanionCloseConnections(id, ''))}
        >
          {t('ui.devices.remote.closeAll')}
        </button>
      </div>
      {conns.map((c) => (
        <div key={c.id} className="deviceItem">
          <div className="deviceItemMain">
            <strong>{c.host}</strong>
            <span className="muted small">
              {c.network}
              {c.process ? ` · ${c.process}` : ''} · {c.rule} ·{' '}
              {/* the core lists the chain last hop first */}
              {[...(c.chains ?? [])].reverse().join(' → ')}
            </span>
            <span className="muted small">
              ↑ {formatBytesSmart(c.upload)} · ↓ {formatBytesSmart(c.download)}
            </span>
          </div>
          <div className="deviceItemActions">
            <button
              type="button"
              className="btn subtle"
              disabled={busy}
              onClick={() => act(() => CompanionCloseConnections(id, c.id))}
            >
              {t('ui.devices.remote.close')}
            </button>
          </div>
        </div>
      ))}
    </div>
  )
}

function TrafficTab({
  id,
  live,
  series,
}: {
  id: string
  live: boolean
  series: companion.TrafficView[]
}) {
  const { t } = useTranslation()
  const [polled, setPolled] = useState<companion.TrafficView[]>([])
  const [error, setError] = useState('')
  // Without a live stream, poll the snapshot while this tab is open.
  useEffect(() => {
    if (live) return
    let alive = true
    const tick = () =>
      CompanionTraffic(id)
        .then((v) => {
          if (alive && v) {
            setPolled((s) => [...s.slice(-(TRAFFIC_POINTS - 1)), v])
            setError('')
          }
        })
        .catch((e) => alive && setError(remoteErrorText(e, t)))
    void tick()
    const timer = window.setInterval(tick, 2000)
    return () => {
      alive = false
      window.clearInterval(timer)
    }
  }, [id, live, t])
  const points = live ? series : polled
  const last = points[points.length - 1]
  const max = Math.max(1, ...points.map((p) => Math.max(p.up, p.down)))
  const line = (pick: (p: companion.TrafficView) => number) =>
    points
      .map(
        (p, i) =>
          `${(i / Math.max(1, TRAFFIC_POINTS - 1)) * 100},${40 - (pick(p) / max) * 38}`,
      )
      .join(' ')
  return (
    <div className="deviceTraffic">
      {error ? <p className="error">{error}</p> : null}
      <div className="devicesActions">
        <span>
          ↑ {last ? `${formatBytesSmart(last.up)}/s` : '—'} · ↓{' '}
          {last ? `${formatBytesSmart(last.down)}/s` : '—'}
        </span>
        <span className="muted small">
          {t('ui.devices.remote.totals')}: ↑{' '}
          {last ? formatBytesSmart(last.upTotal) : '—'} · ↓{' '}
          {last ? formatBytesSmart(last.downTotal) : '—'}
        </span>
      </div>
      <svg
        className="deviceTrafficChart"
        viewBox="0 0 100 40"
        preserveAspectRatio="none"
        aria-hidden
      >
        <polyline className="deviceTrafficDown" points={line((p) => p.down)} />
        <polyline className="deviceTrafficUp" points={line((p) => p.up)} />
      </svg>
    </div>
  )
}

function LogsTab({
  id,
  version,
  streamed,
}: {
  id: string
  version: number
  streamed: companion.LogLineView[]
}) {
  const [level, setLevel] = useState('info')
  const { data, error, t } = useRemote(
    () => CompanionLogs(id, level, 200),
    `${version}:${level}`,
  )
  const lines = useMemo(() => {
    const max = severity(level)
    const seen = data ?? []
    const lastTime = seen.length ? seen[seen.length - 1].time : 0
    return [
      ...seen,
      ...streamed.filter((l) => l.time > lastTime && severity(l.level) <= max),
    ].slice(-STREAM_LOG_CAP)
  }, [data, streamed, level])
  return (
    <div className="deviceLogs">
      <div className="devicesActions">
        <select
          className="input"
          value={level}
          onChange={(e) => setLevel(e.target.value)}
        >
          {LOG_LEVELS.map((l) => (
            <option key={l} value={l}>
              {l}
            </option>
          ))}
        </select>
      </div>
      {error ? <p className="error">{error}</p> : null}
      <pre className="deviceLogLines">
        {lines
          .map((l) => `${formatTime(l.time)} [${l.level}] ${l.msg}`)
          .join('\n') || t('ui.devices.remote.empty')}
      </pre>
    </div>
  )
}

function SettingsTab({ id, version }: { id: string; version: number }) {
  const { data, error, reload, t } = useRemote(
    () => CompanionSettings(id),
    version,
  )
  const { busy, error: actErr, act } = useTabAction(t, reload)
  if (error) return <p className="error">{error}</p>
  if (!data) return <p className="muted small">…</p>
  return (
    <div className="deviceList">
      {/* An apply error on the device (no helper service, …) shows here. */}
      {actErr ? <p className="error">{actErr}</p> : null}
      {data.map((s) => {
        const label =
          s.label ||
          t(`ui.devices.remote.setting.${s.key}`, { defaultValue: s.key })
        if (s.type === 'bool') {
          return (
            <label key={s.key} className="deviceSetting">
              <span>{label}</span>
              <input
                type="checkbox"
                checked={s.value === true}
                disabled={busy}
                onChange={(e) =>
                  act(() => CompanionSetSetting(id, s.key, e.target.checked))
                }
              />
            </label>
          )
        }
        if (s.type === 'enum') {
          return (
            <label key={s.key} className="deviceSetting">
              <span>{label}</span>
              <select
                className="input"
                value={String(s.value ?? '')}
                disabled={busy}
                onChange={(e) =>
                  act(() => CompanionSetSetting(id, s.key, e.target.value))
                }
              >
                {(s.options ?? []).map((o) => (
                  <option key={o} value={o}>
                    {o}
                  </option>
                ))}
              </select>
            </label>
          )
        }
        // A setting type this app does not know is shown, not edited.
        return (
          <div key={s.key} className="deviceSetting">
            <span>{label}</span>
            <span className="muted small">{String(s.value ?? '')}</span>
          </div>
        )
      })}
    </div>
  )
}

function ActionsTab({ id }: { id: string }) {
  const { data, error, t } = useRemote(() => CompanionActions(id), 0)
  const [done, setDone] = useState('')
  const { busy, error: actErr, act } = useTabAction(t)
  if (error) return <p className="error">{error}</p>
  if (!data) return <p className="muted small">…</p>
  return (
    <div className="devicesActions">
      {actErr ? <p className="error">{actErr}</p> : null}
      {done ? <p className="muted small">{done}</p> : null}
      {data.map((a) => {
        const label =
          a.label ||
          t(`ui.devices.remote.action.${a.id}`, { defaultValue: a.id })
        return (
          <button
            key={a.id}
            type="button"
            className="btn subtle"
            disabled={busy}
            onClick={() =>
              act(async () => {
                setDone('')
                await CompanionRunAction(id, a.id)
                setDone(t('ui.devices.remote.actionDone', { action: label }))
              })
            }
          >
            {label}
          </button>
        )
      })}
    </div>
  )
}
