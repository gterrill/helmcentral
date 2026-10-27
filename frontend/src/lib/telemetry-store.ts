import { useCallback, useRef, useSyncExternalStore } from 'react'

/**
 * One live store per telemetry event (ADR 0215): one subscription and one
 * parse per tick, however many components read it, with selectors so each
 * reader re-renders only when its own slice changes.
 *
 * A store starts on its first reader and then stays subscribed for the life
 * of the page. It never empties itself when its last reader leaves, so the
 * snapshot is always the last thing the server said. The stream remembers the
 * current payload of every event and hands it to a listener that subscribes
 * late (ADR 0218), so a store whose first reader mounts after the server's
 * seed starts from the seed, not from silence. Before the first event the
 * snapshot is the store's `initial`, which each store makes an honest
 * "unknown".
 */

/**
 * What one event did to the store: the next state, and the keys whose readers
 * should hear of it. Return null for an event that changed nothing a reader
 * shows; the snapshot then keeps its reference and nobody is notified.
 */
export interface TelemetryUpdate<S> {
  state: S
  /** Keys that changed, for readers that listen by key (the gauge store's paths). */
  changed?: readonly string[]
}

export interface TelemetryStore<S> {
  /** The current state, for an event handler that reads it once. */
  getSnapshot: () => S
  /** Listens to every change. Starts the store. */
  subscribe: (listener: () => void) => () => void
  /** Listens only to changes on the given keys. Starts the store. */
  subscribeKeys: (keys: readonly string[], listener: () => void) => () => void
  /**
   * One slice of the state. Re-renders only when `select` returns something
   * `isEqual` calls different from what it returned before. `select` may be
   * a fresh function on every render.
   */
  useSelector: <T>(select: (state: S) => T, isEqual?: (a: T, b: T) => boolean) => T
}

const resets = new Set<() => void>()

/**
 * Returns every store to its initial state and drops its subscription, so the
 * next reader starts it afresh. Tests only: src/test/setup.ts calls it after
 * each test, because a store otherwise lives as long as the module.
 */
export function resetTelemetryStoresForTest(): void {
  for (const reset of resets) reset()
}

/**
 * Registers other module-level live state (the autopilot's pending commands,
 * the shared trail pollers) for the same reset after each test.
 */
export function onTelemetryTestReset(reset: () => void): void {
  resets.add(reset)
}

/** use-telemetry-stream.ts's subscribeTelemetry, passed in by each store. */
export type SubscribeTelemetry = (event: string, onData: (raw: string, receivedAt: number) => void) => () => void

/**
 * A store for one event. `subscribeTelemetry` is passed in rather than
 * imported here, so this module carries no stream of its own and the test
 * setup can reset stores without loading the real stream ahead of a test's
 * mock of it.
 */
export function createTelemetryStore<S>(
  subscribeTelemetry: SubscribeTelemetry,
  event: string,
  initial: S,
  apply: (previous: S, raw: string, receivedAt: number) => TelemetryUpdate<S> | null,
): TelemetryStore<S> {
  let snapshot = initial
  let unsubscribeStream: (() => void) | null = null
  const listeners = new Set<() => void>()
  const keyListeners = new Map<string, Set<() => void>>()

  // receivedAt is performance.now() when the payload reached the browser: a
  // listener that is replayed an older payload is told when it really came.
  // A stream double that passes none means now.
  function onEvent(raw: string, receivedAt: number = performance.now()): void {
    const update = apply(snapshot, raw, receivedAt)
    if (update === null) return
    snapshot = update.state
    const toCall = new Set(listeners)
    for (const key of update.changed ?? []) {
      const set = keyListeners.get(key)
      if (set) for (const listener of set) toCall.add(listener)
    }
    for (const listener of toCall) listener()
  }

  function start(): void {
    if (unsubscribeStream === null) unsubscribeStream = subscribeTelemetry(event, onEvent)
  }

  resets.add(() => {
    unsubscribeStream?.()
    unsubscribeStream = null
    snapshot = initial
  })

  function subscribe(listener: () => void): () => void {
    listeners.add(listener)
    start()
    return () => { listeners.delete(listener) }
  }

  function subscribeKeys(keys: readonly string[], listener: () => void): () => void {
    for (const key of keys) {
      let set = keyListeners.get(key)
      if (!set) {
        set = new Set()
        keyListeners.set(key, set)
      }
      set.add(listener)
    }
    start()
    return () => {
      for (const key of keys) {
        const set = keyListeners.get(key)
        if (!set) continue
        set.delete(listener)
        if (set.size === 0) keyListeners.delete(key)
      }
    }
  }

  const getSnapshot = () => snapshot

  function useSelector<T>(select: (state: S) => T, isEqual: (a: T, b: T) => boolean = Object.is): T {
    const cache = useRef<{ source: S; select: (state: S) => T; value: T } | null>(null)
    const getSelection = useCallback((): T => {
      const prev = cache.current
      if (prev && prev.source === snapshot && prev.select === select) return prev.value
      const next = select(snapshot)
      const value = prev && isEqual(prev.value, next) ? prev.value : next
      cache.current = { source: snapshot, select, value }
      return value
    }, [select, isEqual])
    return useSyncExternalStore(subscribe, getSelection)
  }

  return { getSnapshot, subscribe, subscribeKeys, useSelector }
}

// ── helpers the store modules share ─────────────────────────────────────────

/** Structural equality for parsed JSON: objects, arrays and primitives. */
export function sameJSON(a: unknown, b: unknown): boolean {
  if (a === b) return true
  if (typeof a !== 'object' || typeof b !== 'object' || a === null || b === null) return false
  if (Array.isArray(a) !== Array.isArray(b)) return false
  if (Array.isArray(a)) {
    const other = b as unknown[]
    return a.length === other.length && a.every((item, i) => sameJSON(item, other[i]))
  }
  const aRecord = a as Record<string, unknown>
  const bRecord = b as Record<string, unknown>
  const aKeys = Object.keys(aRecord)
  if (aKeys.length !== Object.keys(bRecord).length) return false
  return aKeys.every((key) => key in bRecord && sameJSON(aRecord[key], bRecord[key]))
}

/** Every own key of two flat objects holds the same value (Object.is). */
export function sameShallow<T extends object>(a: T, b: T): boolean {
  const keys = Object.keys(a) as Array<keyof T>
  return keys.length === Object.keys(b).length && keys.every((key) => Object.is(a[key], b[key]))
}

/**
 * The parsed state with every field that shows the same as before put back to
 * its previous value, or null when every field does, so the store keeps its
 * snapshot and notifies nobody. `same` decides per field; a field it calls
 * the same keeps its previous object, so a reader holding it sees no change.
 */
export function keepUnchanged<S extends object>(
  previous: S,
  parsed: S,
  same: (field: keyof S, a: S[keyof S], b: S[keyof S]) => boolean = (_field, a, b) => Object.is(a, b),
): S | null {
  const next = { ...parsed }
  let changed = false
  for (const field of Object.keys(parsed) as Array<keyof S>) {
    if (same(field, previous[field], parsed[field])) {
      next[field] = previous[field]
    } else {
      changed = true
    }
  }
  return changed ? next : null
}

/**
 * Each item of the new list replaced by the previous item with the same key
 * when `same` calls them equal, and the previous list itself when every item
 * is the one it had and the order holds. `key` gets the item's index too, for
 * a list whose items may share an id or have none.
 */
export function keepUnchangedItems<T>(
  previous: readonly T[] | null,
  incoming: T[],
  key: (item: T, index: number) => string,
  same: (a: T, b: T) => boolean,
): T[] {
  if (previous === null) return incoming
  const byKey = new Map(previous.map((item, index) => [key(item, index), item]))
  const items = incoming.map((item, index) => {
    const prev = byKey.get(key(item, index))
    return prev !== undefined && same(prev, item) ? prev : item
  })
  const unchanged = items.length === previous.length && items.every((item, i) => item === previous[i])
  return unchanged ? (previous as T[]) : items
}

/**
 * The named fields of an object-shaped store as one object, kept while none of
 * them changed. Pass a module constant: `fields` keys the selection.
 */
export function useStoreFields<S extends object, K extends keyof S>(store: Pick<TelemetryStore<S>, 'useSelector'>, fields: readonly K[]): Pick<S, K> {
  const select = useCallback((state: S) => {
    const out = {} as Pick<S, K>
    for (const field of fields) out[field] = state[field]
    return out
  }, [fields])
  return store.useSelector(select, sameShallow)
}
