import { useCallback, useEffect, useRef } from 'react'

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

function appendCapped(existing: TrailPoint[], incoming: TrailPoint[]): TrailPoint[] {
  if (incoming.length === 0) return existing
  const merged = existing.concat(incoming)
  return merged.length > MAX_POINTS ? merged.slice(merged.length - MAX_POINTS) : merged
}

export interface ServerTrailsResult {
  /** Stable getter for self post-anchor swing trail (ring-buffer). */
  getSelfTrail: () => TrailPoint[]
  /** Stable getter for all AIS vessel trails keyed by vessel name. */
  getAisTrails: () => Map<string, TrailPoint[]>
}

/**
 * Polls /api/tracks?since=X and maintains client-side trail state for
 * self vessel and all nearby AIS vessels. Uses incremental fetching:
 * each successful poll advances `since` to the latest timestamp received,
 * so only new fixes are transferred on subsequent calls.
 *
 * No tracking is done client-side — all sampling happens on the server.
 */
export function useServerTrails(pollIntervalMs = 5000): ServerTrailsResult {
  const selfRef = useRef<TrailPoint[]>([])
  const aisRef = useRef<Map<string, TrailPoint[]>>(new Map())
  const sinceRef = useRef<string>('')
  const inFlightRef = useRef(false)

  const poll = useCallback(async (signal: AbortSignal) => {
    if (inFlightRef.current) return
    inFlightRef.current = true
    try {
      const url = sinceRef.current
        ? `/api/tracks?since=${encodeURIComponent(sinceRef.current)}`
        : '/api/tracks'
      const res = await fetch(url, { signal })
      if (!res.ok) return
      const data = (await res.json()) as ServerTracksResponse
      // The effect cleanup aborts the controller on unmount/StrictMode
      // remount; a response that lands after that point belongs to a poll
      // this hook instance no longer owns, so it must not touch the refs.
      if (signal.aborted) return

      let latestMs = sinceRef.current ? new Date(sinceRef.current).getTime() : 0

      if (data.self) {
        const incoming = data.self
          .map(p => ({ lat: p.lat, lon: p.lon, timestampMs: toMs(p.timestamp) }))
          .sort((a, b) => a.timestampMs - b.timestampMs)
        selfRef.current = appendCapped(selfRef.current, incoming)
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
        aisRef.current = nextAis
      }

      if (latestMs > 0) {
        sinceRef.current = new Date(latestMs).toISOString()
      }
    } catch {
      // Network error or abort: keep existing data, retry next interval
    } finally {
      // If the signal is already aborted, cleanup already cleared this flag
      // synchronously for the next effect run (StrictMode remount). Doing
      // it again here would clobber that run's own in-flight poll.
      if (!signal.aborted) {
        inFlightRef.current = false
      }
    }
  }, [])

  useEffect(() => {
    const controller = new AbortController()
    void poll(controller.signal)
    const id = setInterval(() => void poll(controller.signal), pollIntervalMs)
    return () => {
      controller.abort()
      clearInterval(id)
      inFlightRef.current = false
    }
  }, [poll, pollIntervalMs])

  return {
    getSelfTrail: useCallback(() => selfRef.current, []),
    getAisTrails: useCallback(() => aisRef.current, []),
  }
}
