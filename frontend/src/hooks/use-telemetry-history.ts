import { useEffect, useState } from 'react'

import { apiBaseUrl } from '@/config/api'

export interface TelemetryHistoryPoint {
  time: string
  value: number
}

export interface TelemetryHistoryResult {
  points: TelemetryHistoryPoint[]
  loading: boolean
  /**
   * The server's own reason, shown verbatim. History needs InfluxDB, and the
   * endpoint refuses rather than returning an empty series (ADR 0051) — a
   * trend gauge must say why it has no line, not draw a flat one.
   */
  error: string | null
}

/** How often to re-read, by window. Well below the 1s gauge tick: a trend over
 *  hours does not change meaningfully between frames. */
function pollIntervalMs(window: string): number {
  switch (window) {
    case '1h':
      return 60_000
    case '3h':
    case '6h':
      return 300_000
    default:
      return 900_000
  }
}

export function useTelemetryHistory(path: string, window: string, enabled: boolean): TelemetryHistoryResult {
  const [points, setPoints] = useState<TelemetryHistoryPoint[]>([])
  const [loading, setLoading] = useState(false)
  const [error, setError] = useState<string | null>(null)

  useEffect(() => {
    if (!enabled || path.trim() === '') {
      setPoints([])
      setError(null)
      return
    }

    let cancelled = false

    const load = async () => {
      setLoading(true)
      try {
        const query = new URLSearchParams({ path, window })
        const response = await fetch(`${apiBaseUrl}/api/telemetry/history?${query}`)
        const body = await response.json()
        if (cancelled) return

        if (!response.ok) {
          setError(typeof body?.error === 'string' ? body.error : 'History is unavailable')
          setPoints([])
          return
        }
        setError(null)
        setPoints(Array.isArray(body?.points) ? body.points : [])
      } catch {
        if (!cancelled) {
          setError('History is unavailable')
          setPoints([])
        }
      } finally {
        if (!cancelled) setLoading(false)
      }
    }

    void load()
    const timer = setInterval(() => void load(), pollIntervalMs(window))
    return () => {
      cancelled = true
      clearInterval(timer)
    }
  }, [path, window, enabled])

  return { points, loading, error }
}
