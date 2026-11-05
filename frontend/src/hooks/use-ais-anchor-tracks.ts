import type { TrailPoint } from '@/hooks/use-server-trails'
import { apiRequest } from '@/lib/api-query'
import { createSharedPoller, type PollStore } from '@/lib/shared-poller'
import { feedServerTimeMs } from '@/lib/server-clock'
import { AIS_TRACK_WINDOW_MS, trailNow } from '@/lib/trail-age-bands'

/** One stored AIS report; `newSegment` marks where the line must not join the point before. */
export interface AisTrailPoint extends TrailPoint {
  newSegment: boolean
}

interface AisTrailsResponse {
  /** Changes whenever the server restarts the tracks (a new drop; a moved anchor keeps them); null with no watch. */
  trail_started_at?: string | null
  /** Pass back as `since` to get only what was stored after this answer. */
  cursor?: number
  /** The server's clock in Unix ms, for aging the points (lib/server-clock.ts). */
  server_time_ms?: number
  tracks?: Array<{
    mmsi: string
    points: Array<{ lat: number; lon: number; t_ms: number; new_segment?: boolean }>
  }>
}

type AisTracks = Map<string, AisTrailPoint[]>

const EMPTY: AisTracks = new Map()

// The cursor and the tracks' identity sit at module level beside the poller's
// snapshot, shared by the anchor watch tile and page.
let cursor = 0
let started: string | null = null

async function pollAisTracks(signal: AbortSignal, store: PollStore<AisTracks>): Promise<void> {
  try {
    // The second pass is the full refetch after restarted tracks.
    for (let pass = 0; pass < 2; pass++) {
      const url = cursor
        ? `/api/anchor-watch/trails/ais?since=${cursor}`
        : '/api/anchor-watch/trails/ais'
      const data = await apiRequest<AisTrailsResponse>(url, { signal })
      if (signal.aborted) return
      feedServerTimeMs(data.server_time_ms)

      let tracks = store.get()
      const startedAt = data.trail_started_at ?? null
      if (startedAt !== started) {
        const hadCursor = cursor !== 0
        started = startedAt
        // The new drop has no tracks yet. Showing it empty until the refetch
        // lands is true; holding the old drop's tracks would mask the restart.
        tracks = EMPTY
        cursor = 0
        if (hadCursor && startedAt !== null) {
          store.publish(tracks)
          continue
        }
      }

      // Only the targets with new points get a new array, and the Map is new
      // only when one did.
      const cutoffMs = trailNow() - AIS_TRACK_WINDOW_MS
      let next: AisTracks | null = null
      for (const track of data.tracks ?? []) {
        if (track.points.length === 0) continue
        next ??= new Map(tracks)
        const merged = (next.get(track.mmsi) ?? []).concat(
          track.points.map(p => ({ lat: p.lat, lon: p.lon, timestampMs: p.t_ms, newSegment: p.new_segment === true })),
        )
        let drop = 0
        while (drop < merged.length && merged[drop].timestampMs < cutoffMs) drop++
        next.set(track.mmsi, drop > 0 ? merged.slice(drop) : merged)
      }
      // A target whose newest point is past the cutoff is dropped at the next
      // poll that brings anything new, so a long watch does not keep every boat
      // that ever came by. A quiet watch brings nothing new and grows nothing.
      if (next) {
        for (const [mmsi, points] of next) {
          if (points.length === 0 || points[points.length - 1].timestampMs < cutoffMs) next.delete(mmsi)
        }
      }
      cursor = data.cursor ?? cursor
      store.publish(next ?? tracks)
      return
    }
  } catch {
    // Network or server error: keep what is drawn, retry next interval.
  }
}

const poller = createSharedPoller<AisTracks>(EMPTY, pollAisTracks, () => {
  cursor = 0
  started = null
})

/**
 * Polls /api/anchor-watch/trails/ais?since=<cursor> for the AIS tracks
 * Helmcentral records around the anchor, by MMSI. Each answer carries only what
 * was stored after the cursor. When `trail_started_at` changes the server
 * restarted the tracks: what is held belongs to the old anchor and is dropped,
 * and the new tracks are fetched in full. Empty with no watch.
 *
 * The anchor watch tile and page share one poll and one buffer
 * (lib/shared-poller.ts). The result is a new Map only when a point arrives or
 * the tracks restart, and only the targets that got a point have a new array.
 */
export function useAisTrails(pollIntervalMs = 5000, enabled = true): Map<string, AisTrailPoint[]> {
  return poller.useSnapshot(pollIntervalMs, enabled)
}
