import { describe, expect, it } from 'vitest'

import {
  parseSubscriptionUsage,
  profileTrafficPair,
  subscriptionDaysLeft,
} from './subscription'

const p = (subscriptionInfo: string) => ({ subscriptionInfo })

describe('parseSubscriptionUsage', () => {
  it('parses the standard flat header', () => {
    const u = parseSubscriptionUsage(
      p('upload=100; download=900; total=10000; expire=1893456000'),
    )
    expect(u).toEqual({
      usedBytes: 1000,
      totalBytes: 10000,
      expiresAt: 1893456000,
    })
  })

  it('parses the JSON shape some panels emit', () => {
    const u = parseSubscriptionUsage(
      p('{"upload":1,"download":2,"total":50,"expire":1893456000}'),
    )
    expect(u?.usedBytes).toBe(3)
    expect(u?.totalBytes).toBe(50)
    expect(u?.expiresAt).toBe(1893456000)
  })

  it('parses a nested usage object', () => {
    const u = parseSubscriptionUsage(
      p('{"usage":{"upload":5,"download":5,"total":100}}'),
    )
    expect(u?.usedBytes).toBe(10)
    expect(u?.totalBytes).toBe(100)
  })

  it('falls back to a single used field', () => {
    expect(parseSubscriptionUsage(p('used=42; total=100'))?.usedBytes).toBe(42)
  })

  // Providers routinely omit fields — a partial header must still be usable.
  it('keeps expiry when traffic counters are absent', () => {
    const u = parseSubscriptionUsage(p('expire=1893456000'))
    expect(u?.expiresAt).toBe(1893456000)
    expect(u?.usedBytes).toBe(0)
  })

  it('keeps traffic when expiry is absent', () => {
    const u = parseSubscriptionUsage(p('upload=1; download=1; total=10'))
    expect(u?.expiresAt).toBe(0)
    expect(u?.usedBytes).toBe(2)
  })

  it('returns null for empty / unusable input', () => {
    expect(parseSubscriptionUsage(p(''))).toBeNull()
    expect(parseSubscriptionUsage({})).toBeNull()
    expect(parseSubscriptionUsage(p('nonsense'))).toBeNull()
  })
})

describe('profileTrafficPair (unchanged behaviour)', () => {
  it('formats used / total', () => {
    expect(profileTrafficPair(p('upload=0; download=0; total=0'))).toBe('')
    expect(
      profileTrafficPair(p('upload=1048576; download=0; total=2097152')),
    ).toContain('/')
  })
})

describe('subscriptionDaysLeft', () => {
  it('returns null without an expiry', () => {
    expect(subscriptionDaysLeft(null)).toBeNull()
    expect(
      subscriptionDaysLeft({ usedBytes: 1, totalBytes: 2, expiresAt: 0 }),
    ).toBeNull()
  })

  it('counts whole days ahead', () => {
    const inTenDays = Math.floor(Date.now() / 1000) + 10 * 86_400
    const d = subscriptionDaysLeft({
      usedBytes: 0,
      totalBytes: 0,
      expiresAt: inTenDays,
    })
    expect(d).toBeGreaterThanOrEqual(9)
    expect(d).toBeLessThanOrEqual(10)
  })

  it('goes negative once lapsed', () => {
    const twoDaysAgo = Math.floor(Date.now() / 1000) - 2 * 86_400
    expect(
      subscriptionDaysLeft({
        usedBytes: 0,
        totalBytes: 0,
        expiresAt: twoDaysAgo,
      }),
    ).toBeLessThan(0)
  })
})
