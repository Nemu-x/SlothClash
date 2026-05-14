import { useCallback, useState } from 'react'

import { TestProxyDelay } from '../api/proxy'

/**
 * Per-node delay state and ping actions for the Proxies screen.
 *
 * `busy[id]` is `true` while a probe is in flight. `delays[id]` holds the last
 * successful round-trip in ms. `errors[id]` holds the last failure message.
 *
 * `__all_<group>` batch keys are exposed on `busy` so the Proxies group toolbar
 * can disable the "Ping all" button while a sweep is running.
 */
export function useProxyDelay() {
  const [busy, setBusy] = useState<Record<string, boolean>>({})
  const [delays, setDelays] = useState<Record<string, number>>({})
  const [errors, setErrors] = useState<Record<string, string>>({})

  const ping = useCallback(async (name: string) => {
    const id = String(name ?? '').trim()
    if (!id) return
    setBusy((prev) => ({ ...prev, [id]: true }))
    setErrors((prev) => {
      if (!prev[id]) return prev
      const next = { ...prev }
      delete next[id]
      return next
    })
    try {
      const ms = await TestProxyDelay(id)
      setDelays((prev) => ({ ...prev, [id]: Number(ms) || 0 }))
    } catch (e: any) {
      setErrors((prev) => ({ ...prev, [id]: String(e) }))
    } finally {
      setBusy((prev) => ({ ...prev, [id]: false }))
    }
  }, [])

  /** Ping every node in the group (chunked parallel — like Clash Verge "ping all"). */
  const pingAll = useCallback(
    async (groupName: string, proxies: string[]) => {
      const list = proxies.map((p) => String(p).trim()).filter(Boolean)
      if (list.length === 0) return
      const batchKey = `__all_${groupName}`
      setBusy((prev) => ({ ...prev, [batchKey]: true }))
      try {
        const chunkSize = 8
        for (let i = 0; i < list.length; i += chunkSize) {
          const chunk = list.slice(i, i + chunkSize)
          await Promise.all(chunk.map((id) => ping(id)))
        }
      } finally {
        setBusy((prev) => ({ ...prev, [batchKey]: false }))
      }
    },
    [ping],
  )

  return { busy, delays, errors, ping, pingAll }
}
