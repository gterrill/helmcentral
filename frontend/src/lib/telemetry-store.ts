import { useCallback, useEffect, useRef, useSyncExternalStore } from 'react'

/**
 * One live store per telemetry event (ADR 0215): one subscription and one
 * parse per tick, however many components read it, with selectors so each
 * reader re-renders only when its own slice changes.
 *
 * A store starts on its first reader and then stays subscribed for the life
 * of the page. It never empties itself when its last reader leaves. The
 * server seeds a new connection with every event's current payload, but the
 * browser shares one connection between all the stores, so a store that
 * dropped its subscription and came back to a connection already open would
 * hear nothing until that event next changed: vessel-state and gauge-values
 * are change-gated, so that can be a long time. Staying subscribed means the
 * snapshot is always the last thing the server said. Before the first event
 * it is the store's `initial`, which each store makes an honest "unknown".
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
  /** Starts the store, if it has not started, without listening to it. */
  start: () => void
  /** Listens to every change. Starts the store. */
  subscribe: (listener: () => void) => () => void
  /** Listens only to changes on the given keys. Starts the store. */
  subscribeKeys: (keys: readonly string[], listener: () => void) => () => void
  /** Starts the store on mount without re-rendering the caller on any tick. */
  useRetain: () => void
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

/** use-telemetry-stream.ts's subscribeTelemetry, passed in by each store. */
export type SubscribeTelemetry = (event: string, onData: (raw: string) => void) => () => void

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
  apply: (previous: S, raw: string) => TelemetryUpdate<S> | null,
): TelemetryStore<S> {
  let snapshot = initial
  let unsubscribeStream: (() => void) | null = null
  const listeners = new Set<() => void>()
  const keyListeners = new Map<string, Set<() => void>>()

  function onEvent(raw: string): void {
    const update = apply(snapshot, raw)
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

  function useRetain(): void {
    useEffect(start, [])
  }

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

  return { getSnapshot, start, subscribe, subscribeKeys, useRetain, useSelector }
}
