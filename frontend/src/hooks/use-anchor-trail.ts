import { appendCapped, type TrailPoint } from '@/hooks/use-server-trails'
import { apiRequest } from '@/lib/api-query'
import { createSharedPoller } from '@/lib/shared-poller'

/** Matches the backend's response bound (the newest 10000 points). */
export const ANCHOR_TRAIL_MAX_POINTS = 10000

interface AnchorTrailResponse {
  points?: Array<{ lat: number; lon: number; timestamp: string }>
  /** Changes whenever the server restarts the trail (drop, moved anchor); null with no watch. */
  trail_started_at?: string | null
}

export interface AnchorTrailResult {
  /** Stable getter for the swing trail since the anchor was set or last moved. */
  getAnchorTrail: () => TrailPoint[]
}

// The swing trail at module level, shared by the anchor watch tile and page.
let points: TrailPoint[] = []
let since = ''
let started: string | null = null

async function pollAnchorTrail(signal: AbortSignal): Promise<void> {
  try {
    // The second pass is the full refetch after a restarted trail.
    for (let pass = 0; pass < 2; pass++) {
      const url = since
        ? `/api/anchor-watch/trails/self?since=${encodeURIComponent(since)}`
        : '/api/anchor-watch/trails/self'
      const data = await apiRequest<AnchorTrailResponse>(url, { signal })
      if (signal.aborted) return

      const startedAt = data.trail_started_at ?? null
      if (startedAt !== started) {
        const hadSince = since !== ''
        started = startedAt
        points = []
        since = ''
        if (hadSince && startedAt !== null) continue
      }

      const incoming = (data.points ?? [])
        .map(p => ({ lat: p.lat, lon: p.lon, timestampMs: new Date(p.timestamp).getTime() }))
        .sort((a, b) => a.timestampMs - b.timestampMs)
      if (incoming.length > 0) {
        points = appendCapped(points, incoming, ANCHOR_TRAIL_MAX_POINTS)
        since = new Date(incoming[incoming.length - 1].timestampMs).toISOString()
      }
      return
    }
  } catch {
    // Network or server error: keep what is drawn, retry next interval.
  }
}

const poller = createSharedPoller(pollAnchorTrail, () => {
  points = []
  since = ''
  started = null
})

const TRAIL: AnchorTrailResult = { getAnchorTrail: () => points }

/**
 * Polls /api/anchor-watch/trails/self?since=X for the anchor swing trail.
 * The server keeps the trail, so it survives a reload and a restart. When
 * `trail_started_at` changes the server restarted the trail: the points held
 * here belong to the old one and are dropped, and the new trail is fetched in
 * full rather than from `since`.
 *
 * The anchor watch tile and page share one poll and one buffer
 * (lib/shared-poller.ts), so moving between them keeps the trail.
 */
export function useAnchorTrail(pollIntervalMs = 5000, enabled = true): AnchorTrailResult {
  poller.usePoll(pollIntervalMs, enabled)
  return TRAIL
}
