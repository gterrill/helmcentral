import { useEffect, useSyncExternalStore } from 'react'

import { onTelemetryTestReset } from '@/lib/telemetry-store'

/**
 * One module-level poll shared by every reader of the same data (ADR 0215):
 * it runs while at least one reader is mounted and stops when the last one
 * leaves. Whatever the poll accumulates (a trail buffer, its `since` cursor)
 * lives beside it at module level, so it outlasts a page switch or a wall
 * rotation, and a reader that comes back asks only for what is new.
 *
 * The poller is also a store, like lib/telemetry-store.ts: it holds a
 * snapshot, and a reader gets it through `useSyncExternalStore`. A poll
 * publishes with `publish(next)`; a `next` that is the snapshot already held
 * (same reference) changes nothing and wakes nobody, so a poll that brought
 * nothing new does not re-render a map. The poll decides what "nothing new"
 * is by handing back the same reference.
 *
 * A poll is skipped while one is in flight. Stopping aborts the one in
 * flight, and `poll` must not touch its state once its signal has aborted.
 */
export interface SharedPoller<S> {
  /** Holds the poll open while the caller is mounted and returns the snapshot. `enabled: false` holds no poll but still reads. */
  useSnapshot: (intervalMs: number, enabled?: boolean) => S
  /**
   * The same, for one slice of the snapshot: the reader re-renders only when
   * `select` returns a different value. `select` must be a stable function,
   * and must return the same reference for a part that did not change.
   */
  useSlice: <T>(select: (snapshot: S) => T, intervalMs: number, enabled?: boolean) => T
  /** The current snapshot, for code that reads it once. */
  getSnapshot: () => S
  /** Listens to changes of the snapshot. Does not hold the poll open. */
  subscribe: (listener: () => void) => () => void
}

/** What a poll sees of the snapshot. */
export interface PollStore<S> {
  get: () => S
  /** Publishes the next snapshot. The same reference as `get()` is a no-op. */
  publish: (next: S) => void
}

export function createSharedPoller<S>(
  initial: S,
  poll: (signal: AbortSignal, store: PollStore<S>) => Promise<void>,
  reset: () => void,
): SharedPoller<S> {
  let snapshot = initial
  const listeners = new Set<() => void>()
  const store: PollStore<S> = {
    get: () => snapshot,
    publish: next => {
      if (Object.is(next, snapshot)) return
      snapshot = next
      for (const listener of [...listeners]) listener()
    },
  }
  let holders = 0
  let controller: AbortController | null = null
  let timer: ReturnType<typeof setInterval> | null = null
  let inFlight = false

  function run(signal: AbortSignal): void {
    if (inFlight) return
    inFlight = true
    void poll(signal, store).finally(() => {
      // An aborted poll already had its flag cleared by stop(), and a poll
      // started after that may own it now.
      if (!signal.aborted) inFlight = false
    })
  }

  function stop(): void {
    controller?.abort()
    if (timer !== null) clearInterval(timer)
    controller = null
    timer = null
    inFlight = false
  }

  function acquire(intervalMs: number): () => void {
    holders += 1
    if (holders === 1) {
      const current = new AbortController()
      controller = current
      run(current.signal)
      timer = setInterval(() => run(current.signal), intervalMs)
    }
    return () => {
      holders -= 1
      if (holders === 0) stop()
    }
  }

  onTelemetryTestReset(() => {
    stop()
    holders = 0
    snapshot = initial
    reset()
    for (const listener of [...listeners]) listener()
  })

  function subscribe(listener: () => void): () => void {
    listeners.add(listener)
    return () => {
      listeners.delete(listener)
    }
  }

  function getSnapshot(): S {
    return snapshot
  }

  function useSnapshot(intervalMs: number, enabled = true): S {
    useEffect(() => (enabled ? acquire(intervalMs) : undefined), [intervalMs, enabled])
    return useSyncExternalStore(subscribe, getSnapshot)
  }

  function useSlice<T>(select: (snapshot: S) => T, intervalMs: number, enabled = true): T {
    useEffect(() => (enabled ? acquire(intervalMs) : undefined), [intervalMs, enabled])
    return useSyncExternalStore(subscribe, () => select(snapshot))
  }

  return { useSnapshot, useSlice, getSnapshot, subscribe }
}
