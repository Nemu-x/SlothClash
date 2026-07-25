import { describe, expect, it } from 'vitest'

import { buildBaselineIndex, normalizeRuleKey } from './useRuleToggles'

describe('normalizeRuleKey', () => {
  it('makes live PascalCase types match config UPPER-KEBAB types', () => {
    // The whole point: a live /rules row ("DomainSuffix") must key-match the
    // config baseline line ("DOMAIN-SUFFIX").
    expect(normalizeRuleKey('DomainSuffix', 'wazzup24.com', 'DIRECT')).toBe(
      normalizeRuleKey('DOMAIN-SUFFIX', 'wazzup24.com', 'DIRECT'),
    )
    expect(normalizeRuleKey('ProcessName', 'momentum.exe', 'Auto EU')).toBe(
      normalizeRuleKey('PROCESS-NAME', 'momentum.exe', 'Auto EU'),
    )
    expect(normalizeRuleKey('GeoIP', 'CN', 'DIRECT')).toBe(
      normalizeRuleKey('GEOIP', 'CN', 'DIRECT'),
    )
    expect(normalizeRuleKey('RuleSet', 'ads', 'REJECT')).toBe(
      normalizeRuleKey('RULE-SET', 'ads', 'REJECT'),
    )
  })

  it('distinguishes different rules', () => {
    expect(normalizeRuleKey('DOMAIN-SUFFIX', 'a.com', 'DIRECT')).not.toBe(
      normalizeRuleKey('DOMAIN-SUFFIX', 'b.com', 'DIRECT'),
    )
    expect(normalizeRuleKey('DOMAIN-SUFFIX', 'a.com', 'DIRECT')).not.toBe(
      normalizeRuleKey('DOMAIN', 'a.com', 'DIRECT'),
    )
  })
})

describe('buildBaselineIndex', () => {
  it('maps a unique rule to its exact line', () => {
    const idx = buildBaselineIndex(['DOMAIN-SUFFIX,wazzup24.com,DIRECT'])
    const key = normalizeRuleKey('DomainSuffix', 'wazzup24.com', 'DIRECT')
    expect(idx.get(key)).toBe('DOMAIN-SUFFIX,wazzup24.com,DIRECT')
  })

  it('marks a duplicated key ambiguous (null) so it is never toggled', () => {
    // Two lines that normalize to the same key must NOT be toggleable — we can't
    // know which one the user meant, so deleting either would be wrong.
    const idx = buildBaselineIndex([
      'DOMAIN-SUFFIX,dup.com,DIRECT',
      'domain-suffix,dup.com,direct',
    ])
    const key = normalizeRuleKey('DomainSuffix', 'dup.com', 'DIRECT')
    expect(idx.has(key)).toBe(true)
    expect(idx.get(key)).toBeNull()
  })

  it('ignores blank and non-string lines', () => {
    const idx = buildBaselineIndex([
      '',
      '   ',
      'IP-CIDR,10.0.0.0/8,DIRECT',
      undefined as unknown as string,
    ])
    expect(idx.get(normalizeRuleKey('IPCIDR', '10.0.0.0/8', 'DIRECT'))).toBe(
      'IP-CIDR,10.0.0.0/8,DIRECT',
    )
    expect(idx.size).toBe(1)
  })
})
