import { useMemo } from 'react'
import { useTranslation } from 'react-i18next'

import type { DesyncService } from '../api/desync'
import {
  type DesyncSnapshot,
  type DesyncTone,
  desyncCustomTone,
  desyncCustomViews,
  desyncServiceViews,
  desyncBuiltinTone,
  desyncBuiltinViews,
} from '../utils/desyncStatus'
import { isDesyncOnlyProfile } from '../utils/profileKind'
import { decodeUnicodeEscapes, extractNodeFlagIso } from '../utils/proxyNames'

const NO_SERVICES: DesyncService[] = []

export type HomeActiveNodeVisual = { iso: string; text: string }

/** Compact Desync summary on Home (only while Desync is switched on). */
export type HomeDesyncChip = {
  services: { key: string; name: string; tone: DesyncTone }[]
  /** Full per-service status, one line each (tooltip / accessible name). */
  tooltip: string
}

export type HomeViewModel = {
  hasAnyProfile: boolean
  hasActiveProfile: boolean
  activeProfile: any
  /** The active profile is Desync-only: no nodes, Rule mode only. */
  isDesyncOnly: boolean
  /** Desync-only profile with Desync switched off: everything goes direct. */
  desyncOnlyOff: boolean
  /** Mode / traffic as shown: the optimistic pick while a switch is in flight. */
  displayMode: string
  displayTraffic: string
  connectionLabel: string
  /** 'connecting' | 'error' | 'broken' | 'connected' | 'idle' */
  connectVisual: string
  showProtectedBadge: boolean
  homeTrafficHealthSubtitle: string
  /** Selected node of the active group ("GROUP -> leaf" under GLOBAL). */
  activeNode: string
  activeNodeVisual: HomeActiveNodeVisual
  /** Group whose `proxies` list we let the user change from Home (GLOBAL → nested group). */
  nodePickerGroup: any
  homeAlertTooltip: string
  homeUpdateTooltip: string
  /** null = hide the chip (Desync off, no service on, or not loaded yet). */
  desyncChip: HomeDesyncChip | null
}

/**
 * Everything the Home screen derives from the app state snapshot. App also
 * reads a few of these (profile presence for Connect / auto-connect, the
 * active profile for the operator dialog, the display mode for Proxies).
 */
export function useHomeViewModel({
  state,
  connectBusy,
  optimisticMode,
  optimisticTraffic,
  updateSnap,
  desync = null,
  desyncServices = NO_SERVICES,
}: {
  state: any
  connectBusy: boolean
  optimisticMode: string | null
  optimisticTraffic: string | null
  updateSnap: any
  /** Shared Desync snapshot (useDesyncStatus), the same one the card shows. */
  desync?: DesyncSnapshot | null
  /** Desync service catalog (useDesyncServices): names the service items. */
  desyncServices?: DesyncService[]
}): HomeViewModel {
  const { t } = useTranslation()

  const hasAnyProfile = useMemo(
    () => (state?.profile?.profiles?.length ?? 0) > 0,
    [state],
  )

  const hasActiveProfile = useMemo(
    () => Boolean(state?.profile?.activeProfileId),
    [state],
  )

  const activeProfile = useMemo(() => {
    const id = state?.profile?.activeProfileId
    return state?.profile?.profiles?.find((p: any) => p.id === id)
  }, [state])

  const isDesyncOnly = isDesyncOnlyProfile(activeProfile)
  const desyncOnlyOff =
    isDesyncOnly && desync?.status.settings.enabled === false

  const displayMode = optimisticMode ?? String(state?.mode?.current ?? 'rule')
  const displayTraffic = optimisticTraffic ?? String(state?.traffic ?? 'proxy')

  const connectionLabel = useMemo(() => {
    const s = state?.connection?.status
    const health = String(state?.connection?.health ?? '').trim()
    if (connectBusy || s === 'connecting') return t('ui.home.connecting')
    if (s === 'connected') {
      // Only "broken" is shown as a problem; warming/degraded are still connected for users.
      if (health === 'broken') return t('ui.home.brokenConnection')
      return t('ui.home.protected')
    }
    if (s === 'disconnecting') return t('ui.home.disconnecting')
    if (s === 'error') return t('ui.home.problem')
    return t('ui.home.notConnected')
  }, [state, connectBusy, t])

  const connectVisual = useMemo(() => {
    if (connectBusy) return 'connecting'
    const s = state?.connection?.status
    const health = String(state?.connection?.health ?? '').trim()
    if (s === 'error') return 'error'
    if (s === 'connected') {
      if (health === 'broken') return 'broken'
      return 'connected'
    }
    return 'idle'
  }, [connectBusy, state?.connection?.status, state?.connection?.health])

  const showProtectedBadge = useMemo(() => {
    const health = String(state?.connection?.health ?? '').trim()
    return state?.connection?.status === 'connected' && health !== 'broken'
  }, [state?.connection?.status, state?.connection?.health])

  const homeTrafficHealthSubtitle = useMemo(() => {
    if (state?.connection?.status !== 'connected') return ''
    const tr = displayTraffic === 'tun' ? 'TUN' : t('ui.common.proxy')
    const h = String(state?.connection?.health ?? '').trim()
    const hk =
      h === 'broken' ? t('ui.home.healthBroken') : t('ui.home.healthReady')
    return t('ui.home.trafficHealthLine', { traffic: tr, health: hk })
  }, [state?.connection?.status, state?.connection?.health, displayTraffic, t])

  const activeNode = useMemo(() => {
    const groups = (state?.proxy?.groups ?? []) as any[]
    const activeGroup = String(state?.proxy?.activeGroup ?? '').trim()
    if (!activeGroup || groups.length === 0) return ''
    const g = groups.find((x) => String(x?.name ?? '') === activeGroup)
    if (!g) return ''
    const selected = String(g?.selected ?? '').trim()
    if (!selected) return ''
    if (activeGroup === 'GLOBAL') {
      const sub = groups.find((x) => String(x?.name ?? '') === selected)
      const leaf = String(sub?.selected ?? '').trim()
      return leaf ? `${selected} -> ${leaf}` : selected
    }
    return selected
  }, [state?.proxy?.activeGroup, state?.proxy?.groups])

  const activeNodeVisual = useMemo(() => {
    const raw = decodeUnicodeEscapes(String(activeNode ?? '')).trim()
    if (!raw) return { iso: '', text: '' as string }
    const tail = raw.includes(' -> ')
      ? (raw.split(' -> ').pop() ?? '').trim()
      : raw
    const piece = tail || raw
    return {
      iso: extractNodeFlagIso(piece),
      text: piece,
    }
  }, [activeNode])

  const nodePickerGroup = useMemo(() => {
    // No nodes to pick: only hidden Desync route groups exist.
    if (isDesyncOnly) return null
    const groups = (state?.proxy?.groups ?? []) as any[]
    const activeGroup = String(state?.proxy?.activeGroup ?? '').trim()
    if (!activeGroup || groups.length === 0) return null
    // A chain's hops are its definition, not a choice (core patch 0006).
    const isChain = (x: any) => String(x?.type ?? '').toLowerCase() === 'chain'
    const g = groups.find((x) => String(x?.name ?? '') === activeGroup)
    if (!g || isChain(g)) return null
    if (activeGroup === 'GLOBAL') {
      const subName = String(g?.selected ?? '').trim()
      if (!subName) return null
      const sub = groups.find((x) => String(x?.name ?? '') === subName)
      if (
        !sub ||
        isChain(sub) ||
        !Array.isArray(sub.proxies) ||
        (sub.proxies as string[]).length === 0
      ) {
        return null
      }
      return sub
    }
    if (!Array.isArray(g.proxies) || (g.proxies as string[]).length === 0) {
      return null
    }
    return g
  }, [isDesyncOnly, state?.proxy?.activeGroup, state?.proxy?.groups])

  const homeAlertTooltip = useMemo(() => {
    const parts: string[] = []
    // tunBanner is a general status line ("URL copied", "service installed",
    // "cache cleared" …) — informational, not an alert. Feeding it here made
    // routine successes raise the warning badge on Home, which read as
    // "something is wrong". Only genuine connection problems belong below.
    if (state?.connection?.status === 'connected') {
      const health = String(state?.connection?.health ?? '').trim()
      if (health === 'degraded') {
        parts.push(t('ui.home.healthDegradedHint'))
      }
      if (health === 'broken') {
        parts.push(t('ui.home.healthBrokenHint'))
      }
      if (state?.connection?.lastWarning) {
        parts.push(String(state.connection.lastWarning).trim())
      }
    }
    return parts.join('\n\n')
  }, [
    t,
    state?.connection?.status,
    state?.connection?.health,
    state?.connection?.lastWarning,
  ])

  const homeUpdateTooltip = useMemo(() => {
    if (!updateSnap?.hasUpdate) return ''
    const version = String(updateSnap?.latestVersion ?? '').trim()
    return version
      ? t('ui.home.updateAvailableVersion', { version })
      : t('ui.home.updateAvailable')
  }, [t, updateSnap?.hasUpdate, updateSnap?.latestVersion])

  const connectionStatus = state?.connection?.status
  const modeCurrent = String(state?.mode?.current ?? '')
  const trafficCurrent = String(state?.traffic ?? '')
  const desyncChip = useMemo((): HomeDesyncChip | null => {
    if (!desync || desync.status.settings.enabled !== true) return null
    const ctx = {
      connected: connectionStatus === 'connected',
      mode: modeCurrent,
      traffic: trafficCurrent,
    }
    const views = desyncBuiltinViews(desync, ctx).filter((v) => v.checked)
    const popular = desyncServiceViews(desync, ctx, desyncServices).filter(
      (v) => v.checked,
    )
    const custom = desyncCustomViews(desync, ctx).filter((v) => v.checked)
    if (views.length === 0 && popular.length === 0 && custom.length === 0)
      return null
    const phaseText = (v: { phase: string; attempt: number; total: number }) =>
      t(`desync.phase.${v.phase}`, { attempt: v.attempt, total: v.total })
    const lines: string[] = []
    for (const v of views) {
      lines.push(`${v.name}: ${phaseText(v.status)}`)
      if (v.voice) lines.push(`${t('desync.voice')}: ${phaseText(v.voice)}`)
    }
    for (const v of popular) lines.push(`${v.name}: ${phaseText(v.status)}`)
    for (const v of custom) lines.push(`${v.domain}: ${phaseText(v.status)}`)
    const services: HomeDesyncChip['services'] = views.map((v) => ({
      key: v.key,
      name: v.name,
      tone: desyncBuiltinTone(v),
    }))
    // Each popular service is named like a built-in one ("Facebook"), in
    // catalog order.
    for (const v of popular) {
      services.push({
        key: `service:${v.id}`,
        name: v.name,
        tone: v.status.tone,
      })
    }
    // All custom domains share one item: "Sites 3" with their combined tone.
    if (custom.length > 0) {
      services.push({
        key: 'custom',
        name: t('desync.custom.homeItem', { count: custom.length }),
        tone: desyncCustomTone(custom),
      })
    }
    return { services, tooltip: lines.join('\n') }
  }, [desync, desyncServices, connectionStatus, modeCurrent, trafficCurrent, t])

  return {
    hasAnyProfile,
    hasActiveProfile,
    activeProfile,
    isDesyncOnly,
    desyncOnlyOff,
    displayMode,
    displayTraffic,
    connectionLabel,
    connectVisual,
    showProtectedBadge,
    homeTrafficHealthSubtitle,
    activeNode,
    activeNodeVisual,
    nodePickerGroup,
    homeAlertTooltip,
    homeUpdateTooltip,
    desyncChip,
  }
}
