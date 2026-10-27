import { useCallback, useRef, useSyncExternalStore } from 'react'

/**
 * The reader side of every live store (ADR 0215), written once. A store
 * (telemetry, the query cache, a shared poller) differs in how data reaches
 * it and how long it lives. A reader of any of them does the same thing: take
 * a slice of the current snapshot and re-render only when the slice changes.
 *
 * The selection is cached between calls. It is recomputed when the snapshot
 * changes (by `sameSnapshot`) or when `select` is a different function, and
 * the previous value is kept whenever `isEqual` calls the new one equal, so
 * the reference holds and React sees no change. `select` may be a fresh
 * closure on every render: that costs one recompute on the next read and no
 * extra render, provided `isEqual` holds.
 */
export interface SelectionOptions<S, T> {
  /** Listens to changes of the snapshot (or of the keys the reader cares about). */
  subscribe: (onChange: () => void) => () => void
  /** The current snapshot. Must return the same reference while nothing changed. */
  getSnapshot: () => S
  /** The slice. Gets the value it returned last time (undefined on the first call). */
  select: (snapshot: S, previous: T | undefined) => T
  /** Whether a new selection is the one already held. Default Object.is. */
  isEqual?: (a: T, b: T) => boolean
  /** Whether two snapshots are the same input to `select`. Default Object.is. */
  sameSnapshot?: (a: S, b: S) => boolean
}

export function useSelection<S, T>({
  subscribe,
  getSnapshot,
  select,
  isEqual = Object.is,
  sameSnapshot = Object.is,
}: SelectionOptions<S, T>): T {
  const cache = useRef<{ snapshot: S; select: typeof select; value: T } | null>(null)
  const getSelection = useCallback((): T => {
    const snapshot = getSnapshot()
    const prev = cache.current
    if (prev && prev.select === select && sameSnapshot(prev.snapshot, snapshot)) return prev.value
    const next = select(snapshot, prev?.value)
    const value = prev && isEqual(prev.value, next) ? prev.value : next
    cache.current = { snapshot, select, value }
    return value
  }, [getSnapshot, select, isEqual, sameSnapshot])
  return useSyncExternalStore(subscribe, getSelection, getSelection)
}
