import { useEffect, useRef } from 'react'

export interface TrailPoint {
  lat: number
  lon: number
  timestampMs?: number
  timestamp?: string
}

const MAX_TRAIL_POINTS = 1000

/**
 * Accumulates vessel position fixes into a ring-buffer trail since the anchor was set.
 * Fetches historical trail from server on first load, then tracks new points client-side.
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
  const hasLoadedServer = useRef(false)

  // Reset trail when anchor is re-dropped
  if (resetKey !== prevResetKey.current) {
    trailRef.current = []
    hasLoadedServer.current = false
    prevResetKey.current = resetKey
  }

  // Load trail from server on first activation
  useEffect(() => {
    if (!resetKey || hasLoadedServer.current) return

    void (async () => {
      try {
        const res = await fetch('/api/anchor-watch/trails/self')
        if (!res.ok) return
        const data = (await res.json()) as { points?: Array<{ lat: number; lon: number; timestamp: string }> }
        if (!data.points) return

        // Convert server timestamps to client timestamps and sort
        const serverPoints = data.points
          .map(p => ({
            lat: p.lat,
            lon: p.lon,
            timestampMs: new Date(p.timestamp).getTime(),
          }))
          .sort((a, b) => (a.timestampMs || 0) - (b.timestampMs || 0))

        trailRef.current = serverPoints.slice(-MAX_TRAIL_POINTS)
        hasLoadedServer.current = true
      } catch {
        hasLoadedServer.current = true
      }
    })()
  }, [resetKey])

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
