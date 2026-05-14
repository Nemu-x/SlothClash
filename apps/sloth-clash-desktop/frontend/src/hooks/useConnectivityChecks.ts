import { useCallback, useState } from 'react'

/**
 * Browser-side connectivity probes for the Advanced screen.
 *
 * Each call to `check(id, url)` fetches the URL via `mode: 'no-cors'` with a 7s
 * timeout. The probe is "reachable" iff `fetch` resolves — `no-cors` makes the
 * response opaque, so we never read the body. `busy` holds the currently
 * running target id (one at a time), `results[id]` holds a human-readable
 * outcome string.
 */
export function useConnectivityChecks() {
  const [busy, setBusy] = useState<string | null>(null)
  const [results, setResults] = useState<Record<string, string>>({})

  const check = useCallback(async (id: string, url: string) => {
    setBusy(id)
    const started = performance.now()
    try {
      const ctrl = new AbortController()
      const timeout = window.setTimeout(() => ctrl.abort(), 7000)
      await fetch(url, {
        method: 'GET',
        mode: 'no-cors',
        cache: 'no-store',
        signal: ctrl.signal,
      })
      window.clearTimeout(timeout)
      const ms = Math.round(performance.now() - started)
      setResults((prev) => ({ ...prev, [id]: `Reachable (~${ms} ms)` }))
    } catch (e: any) {
      const ms = Math.round(performance.now() - started)
      const msg = String(e?.message ?? e ?? 'Failed')
      setResults((prev) => ({ ...prev, [id]: `Failed (~${ms} ms): ${msg}` }))
    } finally {
      setBusy((prev) => (prev === id ? null : prev))
    }
  }, [])

  return { busy, results, check }
}
