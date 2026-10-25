import { describe, expect, it } from 'vitest'

import { parseMihomoRulesJson, parseRuleProvidersJson } from './rulesTable'
import { mihomoRuleLinesBody, mihomoRulesBody } from './test/fixtures'

// Regression guard for "Rules screen empty on big profiles": the whole body
// the backend relays must turn into one row per rule, however large it is.
describe('parseMihomoRulesJson on large bodies', () => {
  it('parses 50k object rules (live /rules shape) in order', () => {
    const body = mihomoRulesBody(50_000)
    expect(body.length).toBeGreaterThan(256 * 1024)
    const rows = parseMihomoRulesJson(body)
    expect(rows).toHaveLength(50_001)
    expect(rows[0]).toEqual({
      idx: 0,
      type: 'DomainSuffix',
      payload: 'host-0.example.com',
      proxy: 'DIRECT',
    })
    expect(rows[49_999]).toEqual({
      idx: 49_999,
      type: 'Domain',
      payload: 'host-49999.example.com',
      proxy: 'Proxy',
    })
    expect(rows[50_000]).toMatchObject({ type: 'Match', proxy: 'Proxy' })
    expect(rows.every((r, i) => r.idx === i)).toBe(true)
  })

  it('parses 50k string rule lines and strips trailing options', () => {
    const rows = parseMihomoRulesJson(mihomoRuleLinesBody(50_000))
    expect(rows).toHaveLength(50_000)
    expect(rows[2]).toEqual({
      idx: 2,
      type: 'IPCIDR',
      payload: '10.0.2.0/24',
      proxy: 'REJECT',
    })
    expect(rows[49_999]).toEqual({
      idx: 49_999,
      type: 'Domain',
      payload: 'host-49999.example.com',
      proxy: 'Proxy',
    })
  })

  it('prefers the controller-supplied index over the array position', () => {
    const body = JSON.stringify({
      rules: [{ index: 7, type: 'Domain', payload: 'a.com', proxy: 'DIRECT' }],
    })
    expect(parseMihomoRulesJson(body)[0]?.idx).toBe(7)
  })

  it('returns no rows for an empty, truncated or non-rules body', () => {
    const body = mihomoRulesBody(20_000)
    expect(parseMihomoRulesJson(undefined)).toEqual([])
    expect(parseMihomoRulesJson('   ')).toEqual([])
    // A body cut mid-stream is invalid JSON: the parser must not throw.
    expect(parseMihomoRulesJson(body.slice(0, body.length >> 1))).toEqual([])
    expect(parseMihomoRulesJson('{"rules":{}}')).toEqual([])
  })
})

describe('parseRuleProvidersJson', () => {
  it('parses a large provider map sorted by name', () => {
    const providers: Record<string, unknown> = {}
    for (let i = 999; i >= 0; i--) {
      providers[`p-${String(i).padStart(4, '0')}`] = {
        behavior: 'domain',
        ruleCount: i,
        vehicleType: 'HTTP',
        updatedAt: '2026-01-01T00:00:00Z',
      }
    }
    const rows = parseRuleProvidersJson(JSON.stringify({ providers }))
    expect(rows).toHaveLength(1000)
    expect(rows[0]).toEqual({
      name: 'p-0000',
      behavior: 'domain',
      ruleCount: 0,
      vehicleType: 'HTTP',
      updatedAt: '2026-01-01T00:00:00Z',
    })
    expect(rows[999]?.name).toBe('p-0999')
  })
})
