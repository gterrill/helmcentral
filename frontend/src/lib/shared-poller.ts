import { useEffect } from 'react'

import { onTelemetryTestReset } from '@/lib/telemetry-store'

/**
 * One module-level poll shared by every reader of the same data (ADR 0215):
 * it runs while at least one reader is mounted and stops when the last one
 * leaves. Whatever the poll accumulates (a trail buffer, its `since` cursor)
 * lives beside it at module level, so it outlasts a page switch or a wall
 * rotation, and a reader that comes back asks only for what is new.
 *
 * A poll is skipped while one is in flight. Stopping aborts the one in
 * flight, and `poll` must not touch its state once its signal has aborted.
 */
export interface SharedPoller {
  /** Holds the poll open while the caller is mounted. `enabled: false` holds nothing. */
  usePoll: (intervalMs: number, enabled?: boolean) => void
}

export function createSharedPoller(poll: (signal: AbortSignal) => Promise<void>, reset: () => void): SharedPoller {
  let holders = 0
  let controller: AbortController | null = null
  let timer: ReturnType<typeof setInterval> | null = null
  let inFlight = false

  function run(signal: AbortSignal): void {
    if (inFlight) return
    inFlight = true
    void poll(signal).finally(() => {
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
    reset()
  })

  function usePoll(intervalMs: number, enabled = true): void {
    useEffect(() => (enabled ? acquire(intervalMs) : undefined), [intervalMs, enabled])
  }

  return { usePoll }
}
