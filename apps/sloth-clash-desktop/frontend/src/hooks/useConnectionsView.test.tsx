import { act, waitFor } from '@testing-library/react'
import { describe, expect, it } from 'vitest'

import { connectionsOverview } from '../test/fixtures'
import { renderHookWithProviders } from '../test/render'
import { wailsApp } from '../test/wailsMock'

import {
  type ConnectionRow,
  filterConnections,
  useConnectionsView,
} from './useConnectionsView'

const ROWS: ConnectionRow[] = [
  {
    id: 'aaaa1111-0000',
    rule: 'DomainSuffix',
    rulePayload: 'example.com',
    metadata: {
      host: 'www.example.com',
      destinationIP: '93.184.216.34',
      process: 'chrome.exe',
    },
  },
  {
    id: 'bbbb2222-0000',
    rule: 'GeoIP',
    rulePayload: 'private',
    metadata: { host: '', destinationIP: '192.168.1.10', process: 'Steam.exe' },
  },
  // No metadata / rule at all: must not throw or match "undefined".
  { id: 'cccc3333-0000' },
]

const ids = (rows: ConnectionRow[]) => rows.map((c) => c.id)

describe('filterConnections', () => {
  it('returns the same rows for an empty or blank search', () => {
    expect(filterConnections(ROWS, '')).toBe(ROWS)
    expect(filterConnections(ROWS, '   ')).toBe(ROWS)
  })

  it('matches id, host, destination IP, process, rule and payload', () => {
    expect(ids(filterConnections(ROWS, 'bbbb2222'))).toEqual(['bbbb2222-0000'])
    expect(ids(filterConnections(ROWS, 'www.example'))).toEqual([
      'aaaa1111-0000',
    ])
    expect(ids(filterConnections(ROWS, '192.168.'))).toEqual(['bbbb2222-0000'])
    expect(ids(filterConnections(ROWS, 'steam'))).toEqual(['bbbb2222-0000'])
    expect(ids(filterConnections(ROWS, 'geoip'))).toEqual(['bbbb2222-0000'])
    expect(ids(filterConnections(ROWS, 'private'))).toEqual(['bbbb2222-0000'])
  })

  it('is case-insensitive and trims the query', () => {
    expect(ids(filterConnections(ROWS, '  CHROME.EXE '))).toEqual([
      'aaaa1111-0000',
    ])
  })

  it('does not match missing fields as text', () => {
    expect(filterConnections(ROWS, 'undefined')).toEqual([])
    expect(filterConnections(ROWS, 'null')).toEqual([])
  })
})

describe('useConnectionsView', () => {
  it('filters the polled snapshot through the search setter', async () => {
    wailsApp.stub('FetchConnectionsOverview', () => connectionsOverview(8))
    const { result } = renderHookWithProviders(() => useConnectionsView(true))
    await waitFor(() => expect(result.current.filtered).toHaveLength(8))
    expect(result.current.filtered).toBe(result.current.overview?.connections)
    expect(result.current.search).toBe('')

    act(() => result.current.onSearchChange('DISCORD'))
    expect(result.current.search).toBe('DISCORD')
    // Odd rows are discord.exe in the fixture.
    expect(result.current.filtered).toHaveLength(4)

    act(() => result.current.onSearchChange('site-7.example.com'))
    expect(result.current.filtered.map((c) => c.metadata?.host)).toEqual([
      'site-7.example.com',
    ])

    act(() => result.current.onSearchChange(''))
    expect(result.current.filtered).toHaveLength(8)
  })

  it('is empty and does not fetch while the screen is closed', () => {
    const fetch = wailsApp.stub('FetchConnectionsOverview', () =>
      connectionsOverview(3),
    )
    const { result } = renderHookWithProviders(() => useConnectionsView(false))
    expect(fetch).not.toHaveBeenCalled()
    expect(result.current.overview).toBeNull()
    expect(result.current.filtered).toEqual([])
  })

  it('closeAll closes on the backend and refetches', async () => {
    const fetch = wailsApp.stub('FetchConnectionsOverview', () =>
      connectionsOverview(2),
    )
    const close = wailsApp.stub('CloseAllConnections', () => undefined)
    const { result } = renderHookWithProviders(() => useConnectionsView(true))
    await waitFor(() => expect(result.current.filtered).toHaveLength(2))
    await act(() => result.current.closeAll())
    expect(close).toHaveBeenCalledTimes(1)
    await waitFor(() => expect(fetch).toHaveBeenCalledTimes(2))
  })
})
