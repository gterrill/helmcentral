import { useEffect, useRef } from 'react'

export interface TrailPoint {
  lat: number
  lon: number
  timestampMs: number
}

const MAX_TRAIL_POINTS = 1000

/**
 * Accumulates vessel position fixes into a ring-buffer trail since the anchor was set.
 * The trail resets whenever resetKey changes (e.g. when a new anchor is dropped).
 * Returns a stable getTrail() getter to avoid GeoJSON re-render churn.
 */
export function useVesselTrail(
  lat: number | null,
  lon: number | null,
  resetKey: string | null,
): () => TrailPoint[] {
  const trailRef = useRef<TrailPoint[]>([])
  const prevResetKey = useRef<string | null>(resetKey)

  // Reset trail when anchor is re-dropped
  if (resetKey !== prevResetKey.current) {
    trailRef.current = []
    prevResetKey.current = resetKey
  }

  useEffect(() => {
    if (lat === null || lon === null) return
    const now = Date.now()
    const trail = trailRef.current
    const last = trail[trail.length - 1]
    // Skip duplicate fixes (same position)
    if (last && last.lat === lat && last.lon === lon) return
    if (trail.length >= MAX_TRAIL_POINTS) {
      trail.shift()
    }
    trail.push({ lat, lon, timestampMs: now })
  })

  return () => trailRef.current
}
