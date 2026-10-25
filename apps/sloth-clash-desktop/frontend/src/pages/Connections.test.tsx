import { fireEvent, screen, waitFor, within } from '@testing-library/react'
import { useMemo, useState } from 'react'
import { describe, expect, it } from 'vitest'

import { useConnectionsOverview } from '../hooks/queries/useConnectionsOverview'
import { connectionsOverview } from '../test/fixtures'
import { renderWithProviders } from '../test/render'
import { wailsApp } from '../test/wailsMock'

import { ConnectionsPage } from './Connections'

const CONN_COUNT = 5_000

// Mirrors the Connections wiring in App.tsx (query hook + search filter).
function ConnectionsScreen() {
  const { overview, busy, refresh, closeAll } = useConnectionsOverview(true)
  const [search, setSearch] = useState('')
  const filtered = useMemo(() => {
    const rows = overview?.connections ?? []
    const q = search.trim().toLowerCase()
    if (!q) return rows
    return rows.filter((c) => {
      const meta = c.metadata ?? {}
      const hay =
        `${c.id} ${meta.host ?? ''} ${meta.destinationIP ?? ''} ${meta.process ?? ''} ${c.rule ?? ''} ${c.rulePayload ?? ''}`.toLowerCase()
      return hay.includes(q)
    })
  }, [overview?.connections, search])
  return (
    <ConnectionsPage
      overview={overview}
      filtered={filtered}
      busy={busy}
      search={search}
      onSearchChange={setSearch}
      onRefresh={refresh}
      onCloseAll={() => void closeAll()}
    />
  )
}

function bodyRows(container: HTMLElement) {
  return container.querySelectorAll<HTMLTableRowElement>('tbody > tr')
}

// Text queries scoped to the summary chips: screen-wide getByText walks every
// cell of a 20k-row table.
function summary(container: HTMLElement) {
  const row = container.querySelector<HTMLElement>('.rulesSummaryRow')
  if (!row) throw new Error('summary row not rendered')
  return within(row)
}

// Compare plain numbers: a failing toHaveLength on a 20k-node NodeList
// pretty-prints every node on each waitFor retry and stalls the run.
function rowCount(container: HTMLElement): number {
  return bodyRows(container).length
}

function cellTexts(row: HTMLTableRowElement | undefined): string[] {
  return [...(row?.cells ?? [])].map((c) => c.textContent ?? '')
}

describe('ConnectionsPage with a large connection list', () => {
  it('renders every connection, counts and search', async () => {
    const fetch = wailsApp.stub('FetchConnectionsOverview', () =>
      connectionsOverview(CONN_COUNT),
    )
    const { container } = renderWithProviders(<ConnectionsScreen />)

    await waitFor(() => expect(rowCount(container)).toBe(CONN_COUNT))
    expect(fetch).toHaveBeenCalledTimes(1)
    expect(
      summary(container).getByText(`Total: ${CONN_COUNT}/${CONN_COUNT}`),
    ).toBeVisible()

    const rows = bodyRows(container)
    // [id, host, process, rule, node, traffic]; index 0 is DIRECT (no chain).
    expect(cellTexts(rows[0]).slice(0, 5)).toEqual([
      'conn-000',
      'site-0.example.com:443',
      'chrome.exe',
      'private',
      'Direct',
    ])
    const last = cellTexts(rows[CONN_COUNT - 1])
    expect(last.slice(0, 4)).toEqual([
      'conn-000',
      `site-${CONN_COUNT - 1}.example.com:443`,
      'discord.exe',
      `site-${CONN_COUNT - 1}.example.com`,
    ])
    // Exit node is chains[0]; the tooltip reads entry group -> exit node.
    const chip = rows[CONN_COUNT - 1]?.querySelector('.connNodeChip')
    expect(chip).toHaveAttribute('title', 'Proxy → 🇩🇪 Germany 01')
    expect(chip).toHaveTextContent('Germany 01')

    const searchBox = screen.getByPlaceholderText(
      'Search host / process / rule',
    )
    fireEvent.change(searchBox, { target: { value: 'DISCORD.exe' } })
    await waitFor(() => expect(rowCount(container)).toBe(CONN_COUNT / 2))
    expect(
      summary(container).getByText(`Total: ${CONN_COUNT / 2}/${CONN_COUNT}`),
    ).toBeVisible()

    fireEvent.change(searchBox, { target: { value: 'site-4999.example' } })
    await waitFor(() => expect(rowCount(container)).toBe(1))

    fireEvent.change(searchBox, { target: { value: '' } })
    await waitFor(() => expect(rowCount(container)).toBe(CONN_COUNT))
  }, 60_000)

  it('closes all connections and refetches the snapshot', async () => {
    const fetch = wailsApp.stub('FetchConnectionsOverview', () =>
      connectionsOverview(3),
    )
    const closeAll = wailsApp.stub('CloseAllConnections', () => undefined)
    const { container } = renderWithProviders(<ConnectionsScreen />)
    await waitFor(() => expect(rowCount(container)).toBe(3))

    fireEvent.click(screen.getByRole('button', { name: 'Close all' }))
    await waitFor(() => expect(fetch).toHaveBeenCalledTimes(2))
    expect(closeAll).toHaveBeenCalledTimes(1)
  })
})
