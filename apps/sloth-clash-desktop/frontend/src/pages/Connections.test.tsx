import { fireEvent, screen, waitFor, within } from '@testing-library/react'
import { describe, expect, it } from 'vitest'

import { useConnectionsView } from '../hooks/useConnectionsView'
import { connectionsOverview } from '../test/fixtures'
import { renderWithProviders } from '../test/render'
import { wailsApp } from '../test/wailsMock'

import { ConnectionsPage } from './Connections'

const CONN_COUNT = 5_000

// The Connections wiring from App.tsx: the real useConnectionsView.
function ConnectionsScreen() {
  const view = useConnectionsView(true)
  return <ConnectionsPage view={view} onCloseAll={() => void view.closeAll()} />
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
    // The overview polls every 3.5s; a slow runner can render 5k rows past
    // one interval, so only require that the snapshot was fetched.
    expect(fetch).toHaveBeenCalled()
    expect(
      summary(container).getByText(`Total: ${CONN_COUNT}/${CONN_COUNT}`),
    ).toBeVisible()

    const rows = bodyRows(container)
    // [id, host, process, rule, node, traffic]; index 0 is DIRECT (no chain).
    expect(cellTexts(rows[0]).slice(0, 5)).toEqual([
      '00000000',
      'site-0.example.com:443',
      'chrome.exe',
      'private',
      'Direct',
    ])
    const last = cellTexts(rows[CONN_COUNT - 1])
    expect(last.slice(0, 4)).toEqual([
      String(CONN_COUNT - 1).padStart(8, '0'),
      `site-${CONN_COUNT - 1}.example.com:443`,
      'discord.exe',
      `site-${CONN_COUNT - 1}.example.com`,
    ])
    // The id cell shows the first 8 chars; each row must render its own id.
    const ids = new Set([...rows].map((r) => r.cells[0]?.textContent))
    expect(ids.size).toBe(CONN_COUNT)
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

    const fetchesBeforeClose = fetch.mock.calls.length
    fireEvent.click(screen.getByRole('button', { name: 'Close all' }))
    await waitFor(() =>
      expect(fetch.mock.calls.length).toBeGreaterThan(fetchesBeforeClose),
    )
    expect(closeAll).toHaveBeenCalledTimes(1)
  })
})
