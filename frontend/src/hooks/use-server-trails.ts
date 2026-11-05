import { apiRequest } from '@/lib/api-query'
import { createSharedPoller, type PollStore } from '@/lib/shared-poller'

export interface TrailPoint {
  lat: number
  lon: number
  timestampMs: number
}

interface ServerTracksResponse {
  self?: Array<{ lat: number; lon: number; timestamp: string }>
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

const EMPTY: TrailPoint[] = []

// The cursor sits at module level beside the poller's snapshot, so a remount
// asks only for what is new.
let since = ''

async function pollTracks(signal: AbortSignal, store: PollStore<TrailPoint[]>): Promise<void> {
  try {
    const url = since ? `/api/tracks?since=${encodeURIComponent(since)}` : '/api/tracks'
    const data = await apiRequest<ServerTracksResponse>(url, { signal })
    // The last reader left while this was in flight; its answer belongs to a
    // poll nobody owns now.
    if (signal.aborted) return

    const held = store.get()
    if (!data.self) return
    const incoming = data.self
      .map(p => ({ lat: p.lat, lon: p.lon, timestampMs: toMs(p.timestamp) }))
      .sort((a, b) => a.timestampMs - b.timestampMs)
    if (incoming.length === 0) return
    since = new Date(incoming[incoming.length - 1].timestampMs).toISOString()
    store.publish(appendCapped(held, incoming))
  } catch {
    // Network error or abort: keep existing data, retry next interval
  }
}

const poller = createSharedPoller<TrailPoint[]>(EMPTY, pollTracks, () => {
  since = ''
})

/**
 * Polls /api/tracks?since=X and keeps your own vessel's trail. Each successful
 * poll advances `since` to the latest timestamp received, so only new fixes
 * are transferred after the first. No tracking is done client-side: all
 * sampling happens on the server.
 *
 * Every caller shares one poll and one buffer (lib/shared-poller.ts): the poll
 * runs while any caller is mounted, two maps on one page make one request,
 * and a map that comes back after a page switch keeps the trail and asks only
 * for what is new. `enabled: false` holds nothing. The snapshot changes
 * identity only when a point arrives: a poll that brings nothing new leaves it
 * alone.
 */
export function useSelfTrail(pollIntervalMs = 5000, enabled = true): TrailPoint[] {
  return poller.useSnapshot(pollIntervalMs, enabled)
}
