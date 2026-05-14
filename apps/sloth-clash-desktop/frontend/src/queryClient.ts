import { QueryClient } from '@tanstack/react-query'

/**
 * Shared QueryClient for the desktop frontend.
 *
 * Defaults:
 * - `refetchOnWindowFocus: false` — Wails windows don't get window focus events
 *   in the browser-tab sense; backend pushes are how we know to refetch
 *   (we'll invalidate from a Wails event listener).
 * - `gcTime: 5 min`, `staleTime: 0` — server state is live; treat everything
 *   as stale unless a query opts out. Cache is mainly for cross-screen reuse.
 * - Retries are off — Wails IPC errors are immediate user feedback, not
 *   transient network issues; let callers handle them explicitly.
 */
export const queryClient = new QueryClient({
  defaultOptions: {
    queries: {
      refetchOnWindowFocus: false,
      retry: false,
      staleTime: 0,
      gcTime: 5 * 60 * 1000,
    },
    mutations: {
      retry: false,
    },
  },
})
