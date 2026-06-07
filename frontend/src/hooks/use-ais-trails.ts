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
 * Vessels not seen for more than 5 minutes are pruned.
 * Note: vessel name is used as the key — duplicate names will share a trail.
 */
export function useAisTrails(vessels: NearbyVessel[]): () => Map<string, TrailPoint[]> {
  // Map from vessel name → { points, lastSeenMs }
  const entriesRef = useRef<Map<string, VesselTrailEntry>>(new Map())

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
