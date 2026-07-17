import { useCallback, useEffect, useRef, useState } from 'react'

import type { TideStation } from '@/hooks/use-tide-stations'

export interface TideExtremePoint {
  time: string
  heightM: number
  high: boolean
}

export interface TideChart {
  station: TideStation
  extremes: TideExtremePoint[]
  currentHeightM: number
  direction: string
  // Optional: populated by the backend's centralized classifyTidalPhase/
  // hasDoubleTide (see backend/tide_providers.go), so every provider gets
  // identical values. Absent/empty when the backend can't resolve a phase
  // (e.g. too few extremes) - callers should fall back to the local
  // classifyTidePhase() heuristic (frontend/src/lib/tide-phase.ts) in that case.
  tidalPhase?: string
  doubleHighToday?: boolean
  doubleLowToday?: boolean
}

interface TideChartApi {
  station?: {
    station_id?: string
    name?: string
    state?: string
    lat?: number
    lon?: number
    timezone?: string
  }
  extremes?: Array<{
    time?: string
    height_m?: number
    high?: boolean
  }>
  current_height_m?: number
  direction?: string
  cached?: boolean
  updated_at?: string
  ttl_seconds?: number
  tidal_phase?: string
  double_high_today?: boolean
  double_low_today?: boolean
}

export function useTideChart(provider: string, stationId: string, refreshIntervalSeconds = 3600) {
  const [chart, setChart] = useState<TideChart | null>(null)
  const [loading, setLoading] = useState(true)
  const [error, setError] = useState<string | null>(null)
  const [isCached, setIsCached] = useState(false)
  const [updatedAt, setUpdatedAt] = useState<string | null>(null)
  const [ttlSeconds, setTtlSeconds] = useState<number | null>(null)
  const hasLoadedDataRef = useRef(false)

  const fetchChart = useCallback(async () => {
    if (!provider || !stationId) {
      setChart(null)
      setLoading(false)
      return
    }

    try {
      if (!hasLoadedDataRef.current) {
        setLoading(true)
      }

      const params = new URLSearchParams({ provider, station_id: stationId })
      const response = await fetch(`/api/tide-chart?${params.toString()}`)
      if (!response.ok) {
        const errorPayload = (await response.json().catch(() => null)) as { error?: string } | null
        throw new Error(errorPayload?.error ?? `HTTP error! status: ${response.status}`)
      }

      const data = (await response.json()) as TideChartApi

      const mapped: TideChart = {
        station: {
          stationId: data.station?.station_id ?? '',
          name: data.station?.name ?? '',
          state: data.station?.state,
          lat: typeof data.station?.lat === 'number' ? data.station.lat : 0,
          lon: typeof data.station?.lon === 'number' ? data.station.lon : 0,
          timezone: data.station?.timezone,
        },
        extremes: Array.isArray(data.extremes)
          ? data.extremes.map((extreme) => ({
              time: extreme.time ?? '',
              heightM: typeof extreme.height_m === 'number' ? extreme.height_m : 0,
              high: Boolean(extreme.high),
            }))
          : [],
        currentHeightM: typeof data.current_height_m === 'number' ? data.current_height_m : 0,
        direction: data.direction || '—',
        tidalPhase: typeof data.tidal_phase === 'string' && data.tidal_phase !== '' ? data.tidal_phase : undefined,
        doubleHighToday: typeof data.double_high_today === 'boolean' ? data.double_high_today : undefined,
        doubleLowToday: typeof data.double_low_today === 'boolean' ? data.double_low_today : undefined,
      }

      hasLoadedDataRef.current = true
      setChart(mapped)
      setIsCached(Boolean(data.cached))
      setUpdatedAt(typeof data.updated_at === 'string' ? data.updated_at : null)
      setTtlSeconds(typeof data.ttl_seconds === 'number' ? data.ttl_seconds : null)
      setError(null)
    } catch (err) {
      console.error('Error fetching tide chart:', err)
      setError(err instanceof Error ? err.message : 'Failed to load tide chart')
    } finally {
      setLoading(false)
    }
  }, [provider, stationId])

  useEffect(() => {
    hasLoadedDataRef.current = false
    setChart(null)
    void fetchChart()
    const interval = setInterval(fetchChart, refreshIntervalSeconds * 1000)
    return () => clearInterval(interval)
  }, [fetchChart, refreshIntervalSeconds])

  return { chart, loading, error, isCached, updatedAt, ttlSeconds, refetch: fetchChart }
}
