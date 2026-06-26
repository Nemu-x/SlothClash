import { useCallback, useEffect, useMemo, useState } from 'react'
import { useTranslation } from 'react-i18next'

import {
  CompanionDiscover,
  CompanionListAgents,
  CompanionPairByPin,
  CompanionPairByString,
  CompanionPower,
  CompanionRename,
  CompanionShareSubscription,
  CompanionStartDiscovery,
  CompanionStatus,
  CompanionStopDiscovery,
  CompanionUnpair,
} from '../api/companion'
import type { companion } from '../api/models'
import { EventsOn } from '../api/runtime'

type Agent = companion.AgentInfo

function errText(e: unknown): string {
  const m = (e as { message?: string })?.message
  return String(m ?? e ?? '').trim() || 'unknown error'
}

export function DevicesPage() {
  const { t } = useTranslation()
  const [agents, setAgents] = useState<Agent[]>([])
  const [selectedId, setSelectedId] = useState('')
  const [status, setStatus] = useState<companion.StatusView | null>(null)
  const [pasteStr, setPasteStr] = useState('')
  const [pinTarget, setPinTarget] = useState('')
  const [pin, setPin] = useState('')
  const [renameVal, setRenameVal] = useState('')
  const [busy, setBusy] = useState(false)
  const [error, setError] = useState('')
  const [notice, setNotice] = useState('')

  // Live discovery: seed from the store, start the background watcher, and
  // subscribe to the "companion:agents" event so the list refreshes itself.
  useEffect(() => {
    let mounted = true
    CompanionListAgents()
      .then((a) => mounted && setAgents(a ?? []))
      .catch(() => {})
    CompanionStartDiscovery()
    const off = EventsOn('companion:agents', (payload: unknown) => {
      if (Array.isArray(payload)) setAgents(payload as Agent[])
    })
    return () => {
      mounted = false
      off()
      CompanionStopDiscovery()
    }
  }, [])

  const paired = useMemo(() => agents.filter((a) => a.paired), [agents])
  const discovered = useMemo(() => agents.filter((a) => !a.paired), [agents])
  const selected = useMemo(
    () => agents.find((a) => a.deviceId === selectedId) ?? null,
    [agents, selectedId],
  )

  const refreshStatus = useCallback(async (id: string, paired: boolean) => {
    if (!id || !paired) {
      setStatus(null)
      return
    }
    try {
      setStatus(await CompanionStatus(id))
    } catch (e) {
      setStatus(null)
      setError(errText(e))
    }
  }, [])

  // Only paired agents have a status endpoint; selecting a freshly discovered
  // (unpaired) device must not fire a control call (it would error "not paired").
  useEffect(() => {
    void refreshStatus(selectedId, !!selected?.paired)
  }, [selectedId, selected?.paired, refreshStatus])

  const run = useCallback(async (fn: () => Promise<void>) => {
    setBusy(true)
    setError('')
    setNotice('')
    try {
      await fn()
    } catch (e) {
      setError(errText(e))
    } finally {
      setBusy(false)
    }
  }, [])

  const onDiscover = () =>
    run(async () => {
      setAgents((await CompanionDiscover()) ?? [])
    })

  const onPairString = () =>
    run(async () => {
      const info = await CompanionPairByString(pasteStr)
      setPasteStr('')
      setAgents((await CompanionListAgents()) ?? [])
      setSelectedId(info.deviceId)
      setNotice(t('ui.devices.paired'))
    })

  const onPairPin = () =>
    run(async () => {
      const info = await CompanionPairByPin(pinTarget, pin)
      setPin('')
      setPinTarget('')
      setAgents((await CompanionListAgents()) ?? [])
      setSelectedId(info.deviceId)
      setNotice(t('ui.devices.paired'))
    })

  const onPower = (action: 'on' | 'off' | 'toggle') =>
    run(async () => {
      await CompanionPower(selectedId, action)
      await refreshStatus(selectedId, true)
    })

  const onShare = () =>
    run(async () => {
      await CompanionShareSubscription(selectedId)
      setNotice(t('ui.devices.shared'))
    })

  const onRename = () =>
    run(async () => {
      await CompanionRename(selectedId, renameVal)
      setRenameVal('')
      setAgents((await CompanionListAgents()) ?? [])
      setNotice(t('ui.devices.renamed'))
    })

  const onUnpair = () =>
    run(async () => {
      await CompanionUnpair(selectedId)
      setSelectedId('')
      setStatus(null)
      setAgents((await CompanionListAgents()) ?? [])
    })

  return (
    <div className="devices panel">
      <header className="devicesHeader">
        <h2>{t('ui.devices.title')}</h2>
        <button
          type="button"
          className="btn subtle"
          disabled={busy}
          onClick={onDiscover}
        >
          {t('ui.devices.discover')}
        </button>
      </header>
      <p className="muted small">{t('ui.devices.subtitle')}</p>

      {error ? <p className="error">{error}</p> : null}
      {notice ? <p className="muted small">{notice}</p> : null}

      {/* Pairing */}
      <section className="devicesPair">
        <h3>{t('ui.devices.pairTitle')}</h3>
        <div className="devicesPairRow">
          <input
            className="input"
            placeholder="clashctl-pair://…"
            value={pasteStr}
            onChange={(e) => setPasteStr(e.target.value)}
          />
          <button
            type="button"
            className="btn"
            disabled={busy || !pasteStr.trim()}
            onClick={onPairString}
          >
            {t('ui.devices.pairPaste')}
          </button>
        </div>
        <div className="devicesPairRow">
          <select
            className="input"
            value={pinTarget}
            onChange={(e) => setPinTarget(e.target.value)}
          >
            <option value="">{t('ui.devices.pickAgent')}</option>
            {discovered.map((a) => (
              <option key={a.deviceId} value={a.deviceId}>
                {a.name || a.deviceId}
              </option>
            ))}
          </select>
          <input
            className="input"
            inputMode="numeric"
            maxLength={6}
            placeholder={t('ui.devices.pinPlaceholder')}
            value={pin}
            onChange={(e) => setPin(e.target.value.replace(/\D/g, ''))}
          />
          <button
            type="button"
            className="btn"
            disabled={busy || !pinTarget || pin.length < 4}
            onClick={onPairPin}
          >
            {t('ui.devices.pairPin')}
          </button>
        </div>
      </section>

      <div className="devicesBody">
        {/* Device list */}
        <section className="devicesList">
          <h3>{t('ui.devices.listTitle')}</h3>
          {agents.length === 0 ? (
            <p className="muted small">{t('ui.devices.empty')}</p>
          ) : null}
          {paired.map((a) => (
            <DeviceRow
              key={a.deviceId}
              agent={a}
              selected={a.deviceId === selectedId}
              onSelect={() => setSelectedId(a.deviceId)}
              pairedLabel={t('ui.devices.paired')}
              unsupportedLabel={t('ui.devices.unsupported')}
              offlineLabel={t('ui.devices.offline')}
            />
          ))}
          {discovered.length ? (
            <p className="muted small devicesGroupLabel">
              {t('ui.devices.discoveredGroup')}
            </p>
          ) : null}
          {discovered.map((a) => (
            <DeviceRow
              key={a.deviceId}
              agent={a}
              selected={a.deviceId === selectedId}
              onSelect={() => {
                setSelectedId(a.deviceId)
                setPinTarget(a.deviceId)
              }}
              pairedLabel={t('ui.devices.paired')}
              unsupportedLabel={t('ui.devices.unsupported')}
              offlineLabel={t('ui.devices.offline')}
            />
          ))}
        </section>

        {/* Control panel */}
        <section className="devicesControl">
          {selected && selected.paired ? (
            <>
              <h3>{selected.name || selected.deviceId}</h3>
              <div className="devicesStatusRow">
                <span className="muted">{t('ui.devices.power')}</span>
                <strong>{status?.power ?? '—'}</strong>
              </div>
              {status?.capabilities?.length ? (
                <div className="devicesCaps">
                  {status.capabilities.map((c) => (
                    <span key={c} className="pill">
                      {c}
                    </span>
                  ))}
                </div>
              ) : null}
              <div className="devicesActions">
                <button
                  type="button"
                  className="btn"
                  disabled={busy}
                  onClick={() => onPower('on')}
                >
                  {t('ui.devices.powerOn')}
                </button>
                <button
                  type="button"
                  className="btn subtle"
                  disabled={busy}
                  onClick={() => onPower('off')}
                >
                  {t('ui.devices.powerOff')}
                </button>
                <button
                  type="button"
                  className="btn subtle"
                  disabled={busy}
                  onClick={() => onPower('toggle')}
                >
                  {t('ui.devices.powerToggle')}
                </button>
              </div>
              <div className="devicesActions">
                <button
                  type="button"
                  className="btn subtle"
                  disabled={busy}
                  onClick={onShare}
                >
                  {t('ui.devices.share')}
                </button>
                <button
                  type="button"
                  className="btn subtle"
                  disabled={busy}
                  onClick={() => void refreshStatus(selectedId, true)}
                >
                  {t('ui.devices.refresh')}
                </button>
              </div>
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
                  onClick={onRename}
                >
                  {t('ui.devices.rename')}
                </button>
              </div>
              <button
                type="button"
                className="btn subtle devicesUnpair"
                disabled={busy}
                onClick={onUnpair}
              >
                {t('ui.devices.unpair')}
              </button>
            </>
          ) : (
            <p className="muted small">{t('ui.devices.selectHint')}</p>
          )}
        </section>
      </div>
    </div>
  )
}

function DeviceRow({
  agent,
  selected,
  onSelect,
  pairedLabel,
  unsupportedLabel,
  offlineLabel,
}: {
  agent: Agent
  selected: boolean
  onSelect: () => void
  pairedLabel: string
  unsupportedLabel: string
  offlineLabel: string
}) {
  return (
    <button
      type="button"
      className={`devicesRow${selected ? ' devicesRowSelected' : ''}`}
      onClick={onSelect}
    >
      <span className="devicesRowName">{agent.name || agent.deviceId}</span>
      <span className="devicesRowMeta">
        {agent.app ? <span className="pill">{agent.app}</span> : null}
        {agent.paired && !agent.reachable ? (
          <span className="muted small">{offlineLabel}</span>
        ) : null}
        {!agent.supported ? (
          <span className="muted small">{unsupportedLabel}</span>
        ) : null}
        {agent.paired ? (
          <span className="muted small">{pairedLabel}</span>
        ) : null}
      </span>
    </button>
  )
}
