import { onTelemetryTestReset } from '@/lib/telemetry-store'

/**
 * The boat's clock, for the surfaces that show the time (ADR 0218).
 *
 * The server stamps every vessel-state event with its own `datetime`. The
 * vessel-state store does not keep it, because it moves on every tick and no
 * field reader shows it; the store hands it here as it parses the event, so
 * the event is parsed once. This module keeps the latest stamp and when it
 * arrived, and reads the time as that stamp plus the time since.
 *
 * "Since" is measured on the monotonic performance clock, never Date.now():
 * a kiosk whose wall clock is corrected by NTP must not move the boat's time.
 * A payload remembered by the stream and replayed late carries the moment it
 * really arrived, so the clock is aged by how long it was kept.
 *
 * It ticks only while a clock reader is mounted: a timer starts with the first
 * reader and stops with the last, aligned to the boat's whole seconds. Before
 * any event has carried a datetime the clock is unknown (null), never the
 * browser's own time: the browser's clock can be wrong or in another zone.
 *
 * This is a source, not a hook. The only React reader is useVesselClock
 * (hooks/use-vessel-clock.ts), which also starts the vessel-state store that
 * feeds this module, so a clock reader on its own still gets a time.
 */

/** A new stamp behind the shown time by less than this is network jitter and is ignored. */
const BACKWARD_JITTER_MS = 2000

interface Base {
  /** The server's datetime at the moment it arrived, in ms since the epoch. */
  serverMs: number
  /** performance.now() when it arrived. */
  receivedAt: number
}

let base: Base | null = null
let current: Date | null = null
let timer: ReturnType<typeof setTimeout> | null = null
const listeners = new Set<() => void>()

function serverNowAt(at: number): number | null {
  return base === null ? null : base.serverMs + (at - base.receivedAt)
}

/** The boat's time to the whole second, as one Date until the second changes. Null until known. */
export function readServerClock(): Date | null {
  const ms = serverNowAt(performance.now())
  if (ms === null) return null
  const second = Math.floor(ms / 1000) * 1000
  if (current === null || current.getTime() !== second) current = new Date(second)
  return current
}

function schedule(): void {
  const ms = serverNowAt(performance.now())
  if (ms === null) return
  timer = setTimeout(() => {
    timer = null
    if (listeners.size === 0) return
    for (const listener of [...listeners]) listener()
    schedule()
  }, 1000 - (((ms % 1000) + 1000) % 1000))
}

/**
 * Records the datetime a vessel-state event carried, and when it arrived
 * (performance.now(); now when omitted). An absent or unparseable stamp
 * changes nothing: the clock keeps counting from the last good one. A stamp
 * that would move the shown time back by less than two seconds is network
 * jitter and is ignored; a larger step back is a real change and is applied.
 */
export function feedServerTime(datetime: unknown, receivedAt: number = performance.now()): void {
  if (typeof datetime !== 'string') return
  const serverMs = new Date(datetime).getTime()
  if (Number.isNaN(serverMs)) return
  const shown = serverNowAt(receivedAt)
  if (shown !== null) {
    const behind = shown - serverMs
    if (behind > 0 && behind < BACKWARD_JITTER_MS) return
  }
  base = { serverMs, receivedAt }
  if (listeners.size === 0) return
  // The new stamp moves the second boundary the timer is aligned to.
  if (timer !== null) clearTimeout(timer)
  schedule()
  for (const listener of [...listeners]) listener()
}

export function subscribeServerClock(listener: () => void): () => void {
  listeners.add(listener)
  if (listeners.size === 1 && timer === null) schedule()
  return () => {
    listeners.delete(listener)
    if (listeners.size === 0 && timer !== null) {
      clearTimeout(timer)
      timer = null
    }
  }
}

onTelemetryTestReset(() => {
  if (timer !== null) clearTimeout(timer)
  timer = null
  base = null
  current = null
  listeners.clear()
})
