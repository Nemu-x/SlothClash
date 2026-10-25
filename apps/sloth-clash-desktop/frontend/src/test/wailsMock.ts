import { type Mock, vi } from 'vitest'

/**
 * In-memory stand-in for the Wails bridge used by the generated bindings.
 *
 * wailsjs/go/main/App.js calls `window.go.main.App.<Method>(...)` and
 * wailsjs/runtime/runtime.js calls `window.runtime.<Fn>(...)`. Instead of
 * module-mocking every binding, the DOM test setup installs this object on
 * `window` so the real generated modules (and the src/api barrels on top of
 * them) run unchanged.
 *
 * App methods are created lazily as `vi.fn`s. A method a test did not stub
 * rejects with a descriptive error, so an unexpected backend call fails loudly
 * instead of silently resolving `undefined`. Runtime functions are no-ops,
 * except the event bus: EventsOn* register the listener with `wailsEvents`
 * (and return a working unsubscribe) so a test can emit backend events.
 */
export type WailsAppMock = {
  /** Stub `App.<name>` with `impl` (sync return values are wrapped in a Promise). */
  stub: (name: string, impl: (...args: any[]) => unknown) => Mock
  /** The vi.fn behind `App.<name>` (created on first access). */
  fn: (name: string) => Mock
  /** Forget all stubs and recorded calls. */
  reset: () => void
}

const appFns = new Map<string, Mock>()

function unstubbed(name: string): Mock {
  return vi.fn(() =>
    Promise.reject(new Error(`Wails binding App.${name} is not stubbed`)),
  )
}

function appFn(name: string): Mock {
  let fn = appFns.get(name)
  if (!fn) {
    fn = unstubbed(name)
    appFns.set(name, fn)
  }
  return fn
}

const appProxy = new Proxy(
  {},
  {
    get: (_target, prop) =>
      typeof prop === 'string' ? appFn(prop) : undefined,
  },
)

type EventListener = (...data: unknown[]) => void

const eventListeners = new Map<string, Set<EventListener>>()

// EventsOn / EventsOnce / EventsOnMultiple(name, callback, maxCallbacks).
function eventsOn(name: string, callback: EventListener): () => void {
  let set = eventListeners.get(name)
  if (!set) {
    set = new Set()
    eventListeners.set(name, set)
  }
  set.add(callback)
  return () => {
    eventListeners.get(name)?.delete(callback)
  }
}

const runtimeProxy = new Proxy(
  {},
  {
    get: (_target, prop) => {
      if (typeof prop !== 'string') return undefined
      if (prop.startsWith('EventsOn')) return eventsOn
      return () => undefined
    },
  },
)

/** The Wails event bus as the runtime mock sees it. */
export const wailsEvents = {
  /** Deliver `data` to every current listener of `name`. */
  emit: (name: string, ...data: unknown[]) => {
    for (const cb of [...(eventListeners.get(name) ?? [])]) cb(...data)
  },
  /** Number of live subscriptions for `name`. */
  listenerCount: (name: string) => eventListeners.get(name)?.size ?? 0,
  /** Drop every listener (between tests). */
  reset: () => eventListeners.clear(),
}

export const wailsApp: WailsAppMock = {
  stub: (name, impl) => {
    const fn = vi.fn((...args: any[]) => Promise.resolve(impl(...args)))
    appFns.set(name, fn)
    return fn
  },
  fn: appFn,
  reset: () => appFns.clear(),
}

/** Install the bridge on `window` (called once from the DOM test setup). */
export function installWailsMock(): void {
  const w = window as unknown as Record<string, unknown>
  w.go = { main: { App: appProxy } }
  w.runtime = runtimeProxy
}
