// Test helper only — deliberately not `*.test.ts` so Vitest doesn't collect it as a suite.
//
// jsdom has no layout engine, so there's no real viewport to resize. This stubs
// `window.matchMedia` well enough to drive `useMinWidth` (see lib/breakpoints.ts): it parses
// the two query shapes the app actually issues, `(min-width: Npx)` and `(max-width: Npx)`,
// and evaluates them against a module-level "current width". Any other query — notably
// `(prefers-color-scheme: dark)`, asserted against in use-dark-mode.test.ts:7-15 — always
// reports `matches: false`, matching that test's local stub precedent.
//
// The part that makes breakpoint *transitions* testable (impossible with the old always-false
// stub, whose `addEventListener` was a no-op): listeners registered via `addEventListener`
// or the legacy `addListener` are kept in a registry keyed by query string, and a later call
// to `setViewportWidth` re-evaluates every registered query and fires its listeners — exactly
// what a real `matchMedia` does when the viewport crosses a breakpoint.

type ChangeListener = (event: MediaQueryListEvent) => void

interface RegisteredQuery {
  query: string
  listeners: Set<ChangeListener>
}

const registry: RegisteredQuery[] = []

let currentWidth = 1024

function evaluateQuery(query: string, width: number): boolean {
  const min = query.match(/\(min-width:\s*(\d+(?:\.\d+)?)px\)/)
  if (min) return width >= parseFloat(min[1])

  const max = query.match(/\(max-width:\s*(\d+(?:\.\d+)?)px\)/)
  if (max) return width <= parseFloat(max[1])

  return false
}

function getOrCreateEntry(query: string): RegisteredQuery {
  let entry = registry.find((r) => r.query === query)
  if (!entry) {
    entry = { query, listeners: new Set() }
    registry.push(entry)
  }
  return entry
}

function matchMediaStub(query: string): MediaQueryList {
  const entry = getOrCreateEntry(query)

  return {
    get matches() {
      return evaluateQuery(query, currentWidth)
    },
    media: query,
    onchange: null,
    addEventListener: (type: string, listener: ChangeListener) => {
      if (type === 'change') entry.listeners.add(listener)
    },
    removeEventListener: (type: string, listener: ChangeListener) => {
      if (type === 'change') entry.listeners.delete(listener)
    },
    // Legacy surface — the stub this replaces exposed it, so keep parity for any
    // code still on the old API.
    addListener: (listener: ChangeListener) => entry.listeners.add(listener),
    removeListener: (listener: ChangeListener) => entry.listeners.delete(listener),
    dispatchEvent: () => false,
  } as unknown as MediaQueryList
}

export function setViewportWidth(px: number): void {
  currentWidth = px

  Object.defineProperty(window, 'innerWidth', { configurable: true, writable: true, value: px })
  Object.defineProperty(window, 'outerWidth', { configurable: true, writable: true, value: px })

  window.matchMedia = matchMediaStub

  for (const entry of registry) {
    const matches = evaluateQuery(entry.query, px)
    const event = { matches, media: entry.query } as MediaQueryListEvent
    for (const listener of entry.listeners) listener(event)
  }

  window.dispatchEvent(new Event('resize'))
}
