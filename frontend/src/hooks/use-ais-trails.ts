import { useEffect, useRef } from 'react'
import type { NearbyVessel } from './use-nearby-vessels'
import type { TrailPoint } from './use-vessel-trail'

const MAX_TRAIL_POINTS = 1000
const PRUNE_AFTER_MS = 5 * 60 * 1000 // 5 minutes

interface VesselTrailEntry {
  points: TrailPoint[]
  lastSeenMs: number
}

/**
 * Maintains per-AIS-vessel trailing trails keyed by vessel name.
 * Fetches historical trails from server on first load, then tracks new points client-side.
 * Vessels not seen for more than 5 minutes are pruned.
 * Note: vessel name is used as the key — duplicate names will share a trail.
 */
export function useAisTrails(vessels: NearbyVessel[], anchorWatchActive: boolean): () => Map<string, TrailPoint[]> {
  // Map from vessel name → { points, lastSeenMs }
  const entriesRef = useRef<Map<string, VesselTrailEntry>>(new Map())
  const hasLoadedServerRef = useRef(false)

  // Load trails from server on first anchor watch activation
  useEffect(() => {
    if (!anchorWatchActive || hasLoadedServerRef.current) return

    void (async () => {
      try {
        const res = await fetch('/api/anchor-watch/trails/ais')
        if (!res.ok) return
        const data = (await res.json()) as {
          trails?: Record<string, Array<{ lat: number; lon: number; timestamp: string }>>
        }
        if (!data.trails) return

        for (const [vesselName, serverPoints] of Object.entries(data.trails)) {
          const points = serverPoints
            .map(p => ({
              lat: p.lat,
              lon: p.lon,
              timestampMs: new Date(p.timestamp).getTime(),
            }))
            .sort((a, b) => (a.timestampMs || 0) - (b.timestampMs || 0))
            .slice(-MAX_TRAIL_POINTS)

          if (points.length > 0) {
            entriesRef.current.set(vesselName, {
              points,
              lastSeenMs: Date.now(),
            })
          }
        }
        hasLoadedServerRef.current = true
      } catch {
        hasLoadedServerRef.current = true
      }
    })()
  }, [anchorWatchActive])

  // Track new points from current vessels
  useEffect(() => {
    const now = Date.now()
    const entries = entriesRef.current

    for (const vessel of vessels) {
      if (vessel.lat === undefined || vessel.lon === undefined) continue
      const key = vessel.name
      const existing = entries.get(key)
      if (existing) {
        const last = existing.points[existing.points.length - 1]
        if (!last || last.lat !== vessel.lat || last.lon !== vessel.lon) {
          if (existing.points.length >= MAX_TRAIL_POINTS) {
            existing.points.shift()
          }
          existing.points.push({ lat: vessel.lat, lon: vessel.lon, timestampMs: now })
        }
        existing.lastSeenMs = now
      } else {
        entries.set(key, {
          points: [{ lat: vessel.lat, lon: vessel.lon, timestampMs: now }],
          lastSeenMs: now,
        })
      }
    }

    // Prune stale vessels
    for (const [key, entry] of entries) {
      if (now - entry.lastSeenMs > PRUNE_AFTER_MS) {
        entries.delete(key)
      }
    }
  })

  return () => {
    const result = new Map<string, TrailPoint[]>()
    for (const [key, entry] of entriesRef.current) {
      result.set(key, entry.points)
    }
    return result
  }
}
