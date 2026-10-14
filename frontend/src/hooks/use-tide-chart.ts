import { useEffect } from 'react'

import { useApiQuery } from '@/lib/api-query'
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

interface MappedTideChart {
  chart: TideChart
  isCached: boolean
  updatedAt: string | null
  ttlSeconds: number | null
}

function mapTideChart(data: TideChartApi): MappedTideChart {
  const chart: TideChart = {
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
  return {
    chart,
    isCached: Boolean(data.cached),
    updatedAt: typeof data.updated_at === 'string' ? data.updated_at : null,
    ttlSeconds: typeof data.ttl_seconds === 'number' ? data.ttl_seconds : null,
  }
}

export function useTideChart(provider: string, stationId: string, refreshIntervalSeconds = 3600) {
  const params = new URLSearchParams({ provider, station_id: stationId })
  const { data, error, isLoading, refetch } = useApiQuery<MappedTideChart>({
    queryKey: ['/api/tide-chart', { provider, stationId }],
    path: `/api/tide-chart?${params.toString()}`,
    refetchInterval: refreshIntervalSeconds * 1000,
    enabled: !!provider && !!stationId,
    select: mapTideChart,
  })

  useEffect(() => {
    if (error) console.error('Error fetching tide chart:', error)
  }, [error])

  return {
    chart: data?.chart ?? null,
    loading: isLoading,
    error: error ? error.message || 'Failed to load tide chart' : null,
    isCached: data?.isCached ?? false,
    updatedAt: data?.updatedAt ?? null,
    ttlSeconds: data?.ttlSeconds ?? null,
    refetch,
  }
}
