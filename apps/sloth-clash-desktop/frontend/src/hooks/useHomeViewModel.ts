import { useMemo } from 'react'
import { useTranslation } from 'react-i18next'

import { decodeUnicodeEscapes, extractNodeFlagIso } from '../utils/proxyNames'

export type HomeActiveNodeVisual = { iso: string; text: string }

export type HomeViewModel = {
  hasAnyProfile: boolean
  hasActiveProfile: boolean
  activeProfile: any
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
}: {
  state: any
  connectBusy: boolean
  optimisticMode: string | null
  optimisticTraffic: string | null
  updateSnap: any
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
  }, [state?.proxy?.activeGroup, state?.proxy?.groups])

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

  return {
    hasAnyProfile,
    hasActiveProfile,
    activeProfile,
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
  }
}
