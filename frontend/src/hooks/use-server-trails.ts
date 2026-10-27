import { apiRequest } from '@/lib/api-query'
import { createSharedPoller } from '@/lib/shared-poller'

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

export interface ServerTrailsResult {
  /** Stable getter for self post-anchor swing trail (ring-buffer). */
  getSelfTrail: () => TrailPoint[]
  /** Stable getter for all AIS vessel trails keyed by vessel name. */
  getAisTrails: () => Map<string, TrailPoint[]>
}

// The trails at module level, shared by every map that draws them.
let selfTrail: TrailPoint[] = []
let aisTrails = new Map<string, TrailPoint[]>()
let since = ''

async function pollTracks(signal: AbortSignal): Promise<void> {
  try {
    const url = since ? `/api/tracks?since=${encodeURIComponent(since)}` : '/api/tracks'
    const data = await apiRequest<ServerTracksResponse>(url, { signal })
    // The last reader left while this was in flight; its answer belongs to a
    // poll nobody owns now.
    if (signal.aborted) return

    let latestMs = since ? new Date(since).getTime() : 0

    if (data.self) {
      const incoming = data.self
        .map(p => ({ lat: p.lat, lon: p.lon, timestampMs: toMs(p.timestamp) }))
        .sort((a, b) => a.timestampMs - b.timestampMs)
      selfTrail = appendCapped(selfTrail, incoming)
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
      aisTrails = nextAis
    }

    if (latestMs > 0) {
      since = new Date(latestMs).toISOString()
    }
  } catch {
    // Network error or abort: keep existing data, retry next interval
  }
}

const poller = createSharedPoller(pollTracks, () => {
  selfTrail = []
  aisTrails = new Map()
  since = ''
})

const getSelfTrail = () => selfTrail
const getAisTrails = () => aisTrails
const TRAILS: ServerTrailsResult = { getSelfTrail, getAisTrails }

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
 */
export function useServerTrails(pollIntervalMs = 5000, enabled = true): ServerTrailsResult {
  poller.usePoll(pollIntervalMs, enabled)
  return TRAILS
}
