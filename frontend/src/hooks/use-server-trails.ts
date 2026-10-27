import { apiRequest } from '@/lib/api-query'
import { createSharedPoller, type PollStore } from '@/lib/shared-poller'

export interface TrailPoint {
  lat: number
  lon: number
  timestampMs: number
}

interface ServerTracksResponse {
  self?: Array<{ lat: number; lon: number; timestamp: string }>
  ais?: Record<string, Array<{ lat: number; lon: number; timestamp: string }>>
}

const MAX_POINTS = 1000

function toMs(timestamp: string): number {
  return new Date(timestamp).getTime()
}

export function appendCapped(existing: TrailPoint[], incoming: TrailPoint[], max = MAX_POINTS): TrailPoint[] {
  if (incoming.length === 0) return existing
  const merged = existing.concat(incoming)
  return merged.length > max ? merged.slice(merged.length - max) : merged
}

export interface ServerTrails {
  /** Your own vessel's swing trail (ring buffer), oldest point first. */
  self: TrailPoint[]
  /** Every nearby AIS vessel's trail keyed by vessel name. */
  ais: Map<string, TrailPoint[]>
}

const EMPTY: ServerTrails = { self: [], ais: new Map() }

// The cursor sits at module level beside the poller's snapshot, so a remount
// asks only for what is new.
let since = ''

function sameTrail(a: TrailPoint[], b: TrailPoint[]): boolean {
  if (a.length !== b.length) return false
  for (let i = 0; i < a.length; i++) {
    if (a[i].lat !== b[i].lat || a[i].lon !== b[i].lon || a[i].timestampMs !== b[i].timestampMs) return false
  }
  return true
}

function sameAis(a: Map<string, TrailPoint[]>, b: Map<string, TrailPoint[]>): boolean {
  if (a.size !== b.size) return false
  for (const [name, points] of b) {
    const held = a.get(name)
    if (!held || !sameTrail(held, points)) return false
  }
  return true
}

async function pollTracks(signal: AbortSignal, store: PollStore<ServerTrails>): Promise<void> {
  try {
    const url = since ? `/api/tracks?since=${encodeURIComponent(since)}` : '/api/tracks'
    const data = await apiRequest<ServerTracksResponse>(url, { signal })
    // The last reader left while this was in flight; its answer belongs to a
    // poll nobody owns now.
    if (signal.aborted) return

    const held = store.get()
    let latestMs = since ? new Date(since).getTime() : 0
    let self = held.self
    let ais = held.ais

    if (data.self) {
      const incoming = data.self
        .map(p => ({ lat: p.lat, lon: p.lon, timestampMs: toMs(p.timestamp) }))
        .sort((a, b) => a.timestampMs - b.timestampMs)
      self = appendCapped(self, incoming)
      if (incoming.length > 0) {
        latestMs = Math.max(latestMs, incoming[incoming.length - 1].timestampMs)
      }
    }

    if (data.ais) {
      const nextAis = new Map<string, TrailPoint[]>()
      for (const [name, rawPoints] of Object.entries(data.ais)) {
        const incoming = rawPoints
          .map(p => ({ lat: p.lat, lon: p.lon, timestampMs: toMs(p.timestamp) }))
          .sort((a, b) => a.timestampMs - b.timestampMs)
        nextAis.set(name, incoming.slice(-MAX_POINTS))
        if (incoming.length > 0) {
          latestMs = Math.max(latestMs, incoming[incoming.length - 1].timestampMs)
        }
      }
      // A poll that repeats the trails already held is not a change.
      if (!sameAis(held.ais, nextAis)) ais = nextAis
    }

    if (latestMs > 0) {
      since = new Date(latestMs).toISOString()
    }
    if (self !== held.self || ais !== held.ais) store.publish({ self, ais })
  } catch {
    // Network error or abort: keep existing data, retry next interval
  }
}

const poller = createSharedPoller<ServerTrails>(EMPTY, pollTracks, () => {
  since = ''
})

const selectSelf = (trails: ServerTrails) => trails.self
const selectAis = (trails: ServerTrails) => trails.ais

/**
 * Polls /api/tracks?since=X and keeps the trails for your own vessel and every
 * nearby AIS vessel. Each successful poll advances `since` to the latest
 * timestamp received, so only new fixes are transferred after the first.
 * No tracking is done client-side: all sampling happens on the server.
 *
 * Every caller shares one poll and one buffer (lib/shared-poller.ts): the poll
 * runs while any caller is mounted, two maps on one page make one request,
 * and a map that comes back after a page switch keeps the trail and asks only
 * for what is new. `enabled: false` holds nothing.
 *
 * A reader takes the slice it draws, so a map that draws one is not woken by
 * the other. Each slice changes identity only when its trail does: a poll that
 * brings nothing new leaves both alone.
 */
export function useSelfTrail(pollIntervalMs = 5000, enabled = true): TrailPoint[] {
  return poller.useSlice(selectSelf, pollIntervalMs, enabled)
}

/** Every nearby AIS vessel's trail by name; see {@link useSelfTrail}. */
export function useAisTrails(pollIntervalMs = 5000, enabled = true): Map<string, TrailPoint[]> {
  return poller.useSlice(selectAis, pollIntervalMs, enabled)
}
