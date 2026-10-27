import { useCallback } from 'react'

import { subscribeTelemetry } from '@/hooks/use-telemetry-stream'
import { sameShallow } from '@/lib/same-value'
import { useSelection } from '@/lib/selection'
import { onTelemetryTestReset } from '@/lib/test-reset'

export { sameJSON, sameShallow } from '@/lib/same-value'
export { onTelemetryTestReset, resetTelemetryStoresForTest } from '@/lib/test-reset'

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
 * "unknown", and the store knows whether it has heard the server yet
 * (`useHeardSelector`), so no store needs a `received` field or a null that means
 * "not yet".
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
  /**
   * `useSelector` that reads null until the store has had its first event
   * (since it started or since the last test reset). Before that `initial` is
   * a placeholder, not a reading: the reader shows dashes or a loading state,
   * never a value. The flag turns true on any event that decodes to an
   * object, including one that changes nothing, and every listener, keyed
   * ones included, hears the flip. One subscription; the null and the
   * selection share one cache, so the result keeps its reference while the
   * state holds.
   */
  useHeardSelector: <T>(select: (state: S) => T, isEqual?: (a: T, b: T) => boolean) => T | null
}

interface HeardView<S> {
  state: S
  heard: boolean
}

/**
 * A store for one event. The factory owns everything the nine stores used to
 * repeat: it finds the stream, decodes each frame with JSON.parse, and tracks
 * whether the first event has arrived. A frame that is not JSON, or is JSON
 * but not an object (null, a number, a string, an array), is logged with the
 * event name, once per frame, and changes nothing, the heard flag included: no
 * default stands in for it.
 *
 * `apply` gets the decoded value (untyped on the wire, so the store casts it
 * to the shape it expects, as it did after its own JSON.parse), the previous
 * state and `receivedAt`, and returns the update or null for no change.
 *
 * `subscribeTelemetry` is imported here, not passed in. A test that spies on
 * it with vi.mock sees every store's call. That holds because nothing
 * src/test/setup.ts loads reaches this module: the reset registry is in
 * test-reset.ts and the equality helpers in same-value.ts, neither of which
 * imports the stream, so the real stream is not pulled in ahead of a test
 * file's mock of it.
 */
export function createTelemetryStore<S>(
  event: string,
  initial: S,
  apply: (previous: S, data: unknown, receivedAt: number) => TelemetryUpdate<S> | null,
): TelemetryStore<S> {
  let snapshot = initial
  let heard = false
  // The state and the heard flag as one value that changes reference when
  // either does, so a heard reader caches on one snapshot.
  let view: HeardView<S> = { state: initial, heard: false }
  let unsubscribeStream: (() => void) | null = null
  const listeners = new Set<() => void>()
  const keyListeners = new Map<string, Set<() => void>>()

  // receivedAt is performance.now() when the payload reached the browser: a
  // listener that is replayed an older payload is told when it really came.
  // A stream double that passes none means now.
  function onEvent(raw: string, receivedAt: number = performance.now()): void {
    let data: unknown
    try {
      data = JSON.parse(raw)
    } catch (err) {
      console.error(`Failed to parse ${event} event:`, err)
      return
    }
    if (typeof data !== 'object' || data === null || Array.isArray(data)) {
      console.error(`Ignored ${event} event: expected a JSON object, got ${data === null ? 'null' : Array.isArray(data) ? 'an array' : typeof data}`)
      return
    }
    const update = apply(snapshot, data, receivedAt)
    const firstEvent = !heard
    heard = true
    if (update === null && !firstEvent) return
    const toCall = new Set(listeners)
    if (update !== null) {
      snapshot = update.state
      for (const key of update.changed ?? []) {
        const set = keyListeners.get(key)
        if (set) for (const listener of set) toCall.add(listener)
      }
    }
    if (view.state !== snapshot || view.heard !== heard) view = { state: snapshot, heard }
    // Hearing the server for the first time is news to every reader, keyed
    // ones included, whatever the event changed.
    if (firstEvent) for (const set of keyListeners.values()) for (const listener of set) toCall.add(listener)
    for (const listener of toCall) listener()
  }

  function start(): void {
    if (unsubscribeStream === null) unsubscribeStream = subscribeTelemetry(event, onEvent)
  }

  onTelemetryTestReset(() => {
    unsubscribeStream?.()
    unsubscribeStream = null
    snapshot = initial
    heard = false
    view = { state: initial, heard: false }
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
    return useSelection({ subscribe, getSnapshot, select, isEqual })
  }

  const getView = () => view

  function useHeardSelector<T>(select: (state: S) => T, isEqual: (a: T, b: T) => boolean = Object.is): T | null {
    const selectHeard = useCallback((v: HeardView<S>): T | null => (v.heard ? select(v.state) : null), [select])
    const equal = useCallback((a: T | null, b: T | null) => a !== null && b !== null && isEqual(a, b), [isEqual])
    return useSelection({ subscribe, getSnapshot: getView, select: selectHeard, isEqual: equal })
  }

  return { getSnapshot, subscribe, subscribeKeys, useSelector, useHeardSelector }
}

// ── helpers the store modules share ─────────────────────────────────────────

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
