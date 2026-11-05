// The server's clock for the anchor map's trail ages (ADR 0263). Trail points
// carry the box's own receive times, and the box's clock comes from GPS, while
// a display such as the wall kiosk may have no internet and a clock of its own
// that is wrong. Comparing the two would shift the age bands and could drop
// every AIS point as older than 24 hours. Each answer from either trail
// endpoint carries the box's now (`server_time_ms`), and this module keeps its
// offset from the browser's clock.
//
// This is not lib/server-clock.ts (ADR 0218), and must not be merged into it.
// That module keeps the GNSS fix time the vessel-state stream stamps, which can
// lag the box's clock by seconds, and it ticks every second for the clock
// readers. Feeding both into one clock would make them overwrite each other.
// This one is box time, moves only when an answer says so, and wakes a reader
// only when the offset moves by a second or more.
//
// The offset ignores the request's transit time, a second or two at worst and
// small beside bands that are hours wide.

/** An offset that moves less than this is the request's jitter: readers are not told. */
const NOTIFY_THRESHOLD_MS = 1000

let offsetMs = 0
const listeners = new Set<() => void>()

/** The box's time now, in Unix ms: the browser's clock plus the last offset. The offset is 0 before any answer. */
export function serverNow(): number {
  return Date.now() + offsetMs
}

/** Sets the offset from a trail response's `server_time_ms`. Anything but a finite number changes nothing. */
export function setServerTimeMs(serverMs: unknown): void {
  if (typeof serverMs !== 'number' || !Number.isFinite(serverMs)) return
  const next = serverMs - Date.now()
  const moved = Math.abs(next - offsetMs) >= NOTIFY_THRESHOLD_MS
  offsetMs = next
  if (moved) for (const listener of [...listeners]) listener()
}

/** Calls the listener when the offset moves by a second or more, so a reader can work out its ages again. */
export function subscribeTrailClock(listener: () => void): () => void {
  listeners.add(listener)
  return () => {
    listeners.delete(listener)
  }
}

/** Back to the browser's own clock, for tests. */
export function resetTrailClock(): void {
  offsetMs = 0
  listeners.clear()
}
