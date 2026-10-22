import { useCallback, useEffect, useRef } from 'react'

import { appendCapped, type TrailPoint } from '@/hooks/use-server-trails'
import { apiRequest } from '@/lib/api-query'

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

/**
 * Polls /api/anchor-watch/trails/self?since=X for the anchor swing trail.
 * The server keeps the trail, so it survives a reload and a restart. When
 * `trail_started_at` changes the server restarted the trail: the points held
 * here belong to the old one and are dropped, and the new trail is fetched in
 * full rather than from `since`.
 */
export function useAnchorTrail(pollIntervalMs = 5000, enabled = true): AnchorTrailResult {
  const pointsRef = useRef<TrailPoint[]>([])
  const sinceRef = useRef('')
  const startedRef = useRef<string | null>(null)
  const inFlightRef = useRef(false)

  const poll = useCallback(async (signal: AbortSignal) => {
    if (inFlightRef.current) return
    inFlightRef.current = true
    try {
      // The second pass is the full refetch after a restarted trail.
      for (let pass = 0; pass < 2; pass++) {
        const url = sinceRef.current
          ? `/api/anchor-watch/trails/self?since=${encodeURIComponent(sinceRef.current)}`
          : '/api/anchor-watch/trails/self'
        const data = await apiRequest<AnchorTrailResponse>(url, { signal })
        if (signal.aborted) return

        const started = data.trail_started_at ?? null
        if (started !== startedRef.current) {
          const hadSince = sinceRef.current !== ''
          startedRef.current = started
          pointsRef.current = []
          sinceRef.current = ''
          if (hadSince && started !== null) continue
        }

        const incoming = (data.points ?? [])
          .map(p => ({ lat: p.lat, lon: p.lon, timestampMs: new Date(p.timestamp).getTime() }))
          .sort((a, b) => a.timestampMs - b.timestampMs)
        if (incoming.length > 0) {
          pointsRef.current = appendCapped(pointsRef.current, incoming, ANCHOR_TRAIL_MAX_POINTS)
          sinceRef.current = new Date(incoming[incoming.length - 1].timestampMs).toISOString()
        }
        return
      }
    } catch {
      // Network or server error: keep what is drawn, retry next interval.
    } finally {
      if (!signal.aborted) inFlightRef.current = false
    }
  }, [])

  useEffect(() => {
    if (!enabled) return
    const controller = new AbortController()
    void poll(controller.signal)
    const id = setInterval(() => void poll(controller.signal), pollIntervalMs)
    return () => {
      controller.abort()
      clearInterval(id)
      inFlightRef.current = false
    }
  }, [poll, pollIntervalMs, enabled])

  return { getAnchorTrail: useCallback(() => pointsRef.current, []) }
}
