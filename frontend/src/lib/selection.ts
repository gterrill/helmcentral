import { useCallback, useRef, useSyncExternalStore } from 'react'

import { useWallPageActive } from '@/lib/wall-page-active'

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
  /**
   * The slice. Gets the value it returned last time (undefined on the first
   * call). Default: the snapshot itself.
   */
  select?: (snapshot: S, previous: T | undefined) => T
  /** Whether a new selection is the one already held. Default Object.is. */
  isEqual?: (a: T, b: T) => boolean
  /** Whether two snapshots are the same input to `select`. Default Object.is. */
  sameSnapshot?: (a: S, b: S) => boolean
  /**
   * What the reader does while its wall page rests (ADR 0217). 'hold' (the
   * default) keeps the value last shown, so a hidden tile does not re-render.
   * 'live' follows the store. An alarm reader passes 'live': an alarm must
   * sound and show on whatever page is up, not wait for its own page to come
   * round.
   */
  whileResting?: RestingPolicy
}

/** How a reader treats change while its wall page rests. */
export type RestingPolicy = 'hold' | 'live'

// One stable function, so a reader that selects nothing never recomputes.
const identity = <S>(snapshot: S) => snapshot

// Without `select` the result is the snapshot; with it, the selection. Two
// signatures, so a caller cannot name a selection type and leave `select` out.
export function useSelection<S>(options: Omit<SelectionOptions<S, S>, 'select'> & { select?: undefined }): S
export function useSelection<S, T>(options: SelectionOptions<S, T> & { select: NonNullable<SelectionOptions<S, T>['select']> }): T
export function useSelection<S, T>({
  subscribe,
  getSnapshot,
  select = identity as unknown as (snapshot: S, previous: T | undefined) => T,
  isEqual = Object.is,
  sameSnapshot = Object.is,
  whileResting = 'hold',
}: SelectionOptions<S, T>): T {
  const pageActive = useWallPageActive() || whileResting === 'live'
  const cache = useRef<{ snapshot: S; getSnapshot: typeof getSnapshot; select: typeof select; value: T } | null>(null)
  const getSelection = useCallback((): T => {
    // A resting wall page keeps what it last showed, for as long as the reader
    // is asking the same question. Only a reader that has shown nothing yet, or
    // has changed its key (another path, another query), reads the snapshot.
    const held = cache.current
    if (!pageActive && held && held.select === select && held.getSnapshot === getSnapshot) return held.value
    const snapshot = getSnapshot()
    const prev = cache.current
    // Another source whose snapshot compares equal still gets its own object,
    // and the cache records it, so the hold above applies to the new source.
    if (prev && prev.select === select && prev.getSnapshot === getSnapshot && sameSnapshot(prev.snapshot, snapshot)) return prev.value
    const next = select(snapshot, prev?.value)
    const value = prev && isEqual(prev.value, next) ? prev.value : next
    cache.current = { snapshot, getSnapshot, select, value }
    return value
  }, [pageActive, getSnapshot, select, isEqual, sameSnapshot])
  // The subscription stays while the page rests, because a poll or a stream
  // lives as long as it has a reader (ADR 0217). The store still notifies;
  // React calls getSelection, gets the held value and skips the render. When
  // the page wakes the context change re-renders the tile, and getSelection
  // reads the current value.
  return useSyncExternalStore(subscribe, getSelection, getSelection)
}
