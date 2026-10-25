import type { ConnectionsOverview } from '../types/app'

// Deterministic large fixtures shaped like real mihomo controller responses.

export const RULE_TYPES = [
  'DomainSuffix',
  'Domain',
  'IPCIDR',
  'GeoIP',
  'RuleSet',
  'ProcessName',
] as const
export const RULE_POLICIES = ['DIRECT', 'Proxy', 'REJECT'] as const

/**
 * GET /rules body with `count` object rules (mihomo's live shape:
 * {type, payload, proxy, size}) plus a trailing MATCH catch-all.
 */
export function mihomoRulesBody(count: number): string {
  const rules: Array<Record<string, unknown>> = []
  for (let i = 0; i < count; i++) {
    rules.push({
      type: RULE_TYPES[i % RULE_TYPES.length],
      payload: `host-${i}.example.com`,
      proxy: RULE_POLICIES[i % RULE_POLICIES.length],
      size: -1,
    })
  }
  rules.push({ type: 'Match', payload: '', proxy: 'Proxy', size: -1 })
  return JSON.stringify({ rules })
}

/** Same rules in the legacy string-line shape (TYPE,PAYLOAD,POLICY[,opts]). */
export function mihomoRuleLinesBody(count: number): string {
  const rules: string[] = []
  for (let i = 0; i < count; i++) {
    const type = RULE_TYPES[i % RULE_TYPES.length]
    const policy = RULE_POLICIES[i % RULE_POLICIES.length]
    rules.push(
      type === 'IPCIDR'
        ? `${type},10.${(i >> 8) & 255}.${i & 255}.0/24,${policy},no-resolve`
        : `${type},host-${i}.example.com,${policy}`,
    )
  }
  return JSON.stringify({ rules })
}

/** GET /connections-shaped overview with `count` connections. */
export function connectionsOverview(count: number): ConnectionsOverview {
  const connections: NonNullable<ConnectionsOverview['connections']> = []
  for (let i = 0; i < count; i++) {
    const direct = i % 4 === 0
    connections.push({
      id: `conn-${String(i).padStart(8, '0')}-uuid`,
      upload: i * 10,
      download: i * 100,
      rule: direct ? 'GeoIP' : 'DomainSuffix',
      rulePayload: direct ? 'private' : `site-${i}.example.com`,
      chains: direct ? [] : ['🇩🇪 Germany 01', 'Proxy'],
      metadata: {
        host: `site-${i}.example.com`,
        destinationIP: `10.0.${(i >> 8) & 255}.${i & 255}`,
        destinationPort: '443',
        process: i % 2 === 0 ? 'chrome.exe' : 'discord.exe',
        network: 'tcp',
        type: 'HTTP',
      },
    })
  }
  return {
    reachable: true,
    uploadTotal: 1024,
    downloadTotal: 2048,
    connections,
  }
}
