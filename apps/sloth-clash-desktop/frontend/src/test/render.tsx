import { QueryClient } from '@tanstack/react-query'
import {
  type RenderOptions,
  render,
  renderHook,
  type RenderHookOptions,
} from '@testing-library/react'
import type { ReactElement, ReactNode } from 'react'

import { TestProviders } from './TestProviders'

/**
 * A fresh QueryClient per test with the production defaults that matter for
 * determinism (no retries, no focus refetch) and no cache carry-over.
 */
export function createTestQueryClient(): QueryClient {
  return new QueryClient({
    defaultOptions: {
      queries: {
        refetchOnWindowFocus: false,
        retry: false,
        staleTime: 0,
        gcTime: Infinity,
      },
      mutations: { retry: false },
    },
  })
}

function makeWrapper(client: QueryClient) {
  return ({ children }: { children: ReactNode }) => (
    <TestProviders client={client}>{children}</TestProviders>
  )
}

/** render() wrapped in the providers the app's screens expect (main.tsx). */
export function renderWithProviders(
  ui: ReactElement,
  {
    queryClient = createTestQueryClient(),
    ...options
  }: Omit<RenderOptions, 'wrapper'> & { queryClient?: QueryClient } = {},
) {
  return {
    queryClient,
    ...render(ui, { wrapper: makeWrapper(queryClient), ...options }),
  }
}

/** renderHook() with the same providers as renderWithProviders. */
export function renderHookWithProviders<Result, Props>(
  hook: (props: Props) => Result,
  {
    queryClient = createTestQueryClient(),
    ...options
  }: Omit<RenderHookOptions<Props>, 'wrapper'> & {
    queryClient?: QueryClient
  } = {},
) {
  return {
    queryClient,
    ...renderHook(hook, { wrapper: makeWrapper(queryClient), ...options }),
  }
}
