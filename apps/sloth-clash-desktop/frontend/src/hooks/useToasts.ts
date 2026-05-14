import { useCallback, useEffect, useRef, useState } from 'react'

export type ToastKind = 'info' | 'success' | 'warn' | 'error'

export type Toast = {
  id: number
  kind: ToastKind
  message: string
  /** When set, an action button is rendered with this label. */
  actionLabel?: string
  /** Fires when the action button is clicked. The toast is dismissed after. */
  onAction?: () => void
  /** Override the default lifetime (ms). 0 keeps the toast until dismissed. */
  durationMs?: number
}

const DEFAULT_DURATION: Record<ToastKind, number> = {
  info: 3500,
  success: 3000,
  warn: 5000,
  error: 7000,
}

let nextId = 1

/**
 * Lightweight toast queue. Multiple toasts can be visible at once; each one
 * auto-dismisses after a kind-specific duration unless `durationMs: 0` is
 * passed to pin it until the user closes it.
 */
export function useToasts() {
  const [toasts, setToasts] = useState<Toast[]>([])
  const timersRef = useRef<Map<number, number>>(new Map())

  const dismiss = useCallback((id: number) => {
    const timer = timersRef.current.get(id)
    if (timer) {
      window.clearTimeout(timer)
      timersRef.current.delete(id)
    }
    setToasts((prev) => prev.filter((t) => t.id !== id))
  }, [])

  const push = useCallback(
    (toast: Omit<Toast, 'id'>) => {
      const id = nextId++
      const t: Toast = { id, ...toast }
      setToasts((prev) => [...prev, t])
      const ms =
        typeof t.durationMs === 'number'
          ? t.durationMs
          : DEFAULT_DURATION[t.kind]
      if (ms > 0) {
        const timer = window.setTimeout(() => dismiss(id), ms)
        timersRef.current.set(id, timer)
      }
      return id
    },
    [dismiss],
  )

  useEffect(() => {
    const timers = timersRef.current
    return () => {
      timers.forEach((timer) => window.clearTimeout(timer))
      timers.clear()
    }
  }, [])

  return { toasts, push, dismiss }
}
