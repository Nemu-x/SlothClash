import { describe, expect, it, vi } from 'vitest'

import { activateProfileThenOfferDesync } from './profileKind'

const DESYNC_ONLY = { id: 'p2', kind: 'desync-only' }
const SUBSCRIPTION = { id: 'p1', url: 'https://example.com/sub' }

describe('activateProfileThenOfferDesync', () => {
  it('offers Desync after a Desync-only profile is activated', async () => {
    const offer = vi.fn(async () => {})
    await activateProfileThenOfferDesync(DESYNC_ONLY, async () => true, offer)
    expect(offer).toHaveBeenCalledTimes(1)
  })

  it('offers nothing when the activation failed', async () => {
    const offer = vi.fn(async () => {})
    await activateProfileThenOfferDesync(DESYNC_ONLY, async () => false, offer)
    expect(offer).not.toHaveBeenCalled()
  })

  it('offers nothing for a subscription profile', async () => {
    const offer = vi.fn(async () => {})
    await activateProfileThenOfferDesync(SUBSCRIPTION, async () => true, offer)
    expect(offer).not.toHaveBeenCalled()
  })
})
