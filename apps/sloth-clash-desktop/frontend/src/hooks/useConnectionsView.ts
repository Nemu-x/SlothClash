import { useMemo, useState } from 'react'

import type { ConnectionsOverview } from '../types/app'

import { useConnectionsOverview } from './queries/useConnectionsOverview'

export type ConnectionRow = NonNullable<
  ConnectionsOverview['connections']
>[number]

/** Case-insensitive search over id, host, destination IP, process and rule. */
export function filterConnections(
  rows: ConnectionRow[],
  search: string,
): ConnectionRow[] {
  const q = search.trim().toLowerCase()
  if (!q) return rows
  return rows.filter((c) => {
    const meta = c.metadata ?? {}
    const hay =
      `${c.id} ${meta.host ?? ''} ${meta.destinationIP ?? ''} ${meta.process ?? ''} ${c.rule ?? ''} ${c.rulePayload ?? ''}`.toLowerCase()
    return hay.includes(q)
  })
}

export type ConnectionsView = {
  overview: ConnectionsOverview | null
  filtered: ConnectionRow[]
  busy: boolean
  search: string
  onSearchChange: (next: string) => void
  onRefresh: () => void
  closeAll: () => Promise<void>
}

/**
 * The Connections screen: the polled snapshot plus the search filter.
 *
 * Called from App, not from ConnectionsPage: the page unmounts on every
 * screen switch and the search must survive leaving and re-opening it.
 */
export function useConnectionsView(enabled: boolean): ConnectionsView {
  const { overview, busy, refresh, closeAll } = useConnectionsOverview(enabled)
  const [search, setSearch] = useState('')
  const filtered = useMemo(
    () => filterConnections(overview?.connections ?? [], search),
    [overview?.connections, search],
  )
  return {
    overview,
    filtered,
    busy,
    search,
    onSearchChange: setSearch,
    onRefresh: refresh,
    closeAll,
  }
}
