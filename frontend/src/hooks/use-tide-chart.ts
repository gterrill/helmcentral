import { useEffect } from 'react'

import { useApiQuery } from '@/lib/api-query'
import type { TideStation } from '@/hooks/use-tide-stations'
import { mapTideChart, type MappedTideChart } from '@/lib/tide-chart-map'
import type { TideDatum } from '@/lib/tide-datum'

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
  /** What heightM is measured from; chart datum when the server says nothing. */
  heightDatum?: TideDatum
  // Optional: populated by the backend's centralized classifyTidalPhase/
  // hasDoubleTide (see backend/tide_providers.go), so every provider gets
  // identical values. Absent/empty when the backend can't resolve a phase
  // (e.g. too few extremes) - callers should fall back to the local
  // classifyTidePhase() heuristic (frontend/src/lib/tide-phase.ts) in that case.
  tidalPhase?: string
  doubleHighToday?: boolean
  doubleLowToday?: boolean
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
