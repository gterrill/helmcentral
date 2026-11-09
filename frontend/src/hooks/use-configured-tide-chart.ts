import { useEffect } from 'react'

import { ApiError, useApiQuery } from '@/lib/api-query'
import { mapTideChart, type MappedTideChart } from '@/lib/tide-chart-map'

/**
 * The tide chart the server picks (ADR 0273): the provider in force (blank
 * means the default), with its saved station, or for a provider that follows a
 * position the anchor's or boat's point. With a place it is the tide at the
 * place, from the station nearest it or the place's point. The screen sends no
 * provider and no station, so it never handles a station for a provider that
 * follows the boat.
 *
 * `cacheKey` stands for what the server reads to pick the tide: the saved
 * provider and station, and the anchor's point while a watch is set. The chart
 * is fetched again when any of them changes, and every ten minutes, as the tide
 * tile is, so the two do not disagree for long.
 */
export function useConfiguredTideChart(cacheKey: string, enabled: boolean, place?: { lat: number; lon: number }, refreshIntervalSeconds = 600) {
  const path = place ? `/api/tide-chart?lat=${place.lat}&lon=${place.lon}` : '/api/tide-chart'
  const { data, error, isLoading, refetch } = useApiQuery<MappedTideChart>({
    queryKey: ['/api/tide-chart', 'configured', { cacheKey, place: place ?? null }],
    path,
    refetchInterval: refreshIntervalSeconds * 1000,
    enabled,
    select: mapTideChart,
  })

  useEffect(() => {
    if (error) console.error('Error fetching tide chart:', error)
  }, [error])

  const reason = error instanceof ApiError && typeof (error.body as { reason?: unknown } | undefined)?.reason === 'string'
    ? (error.body as { reason: string }).reason
    : ''
  return {
    chart: data?.chart ?? null,
    loading: isLoading,
    error: error ? error.message || 'Failed to load tide chart' : null,
    /** True when the server says the tide plugin isn't installed. */
    notSetUp: reason === 'no_plugin',
    /** True when the provider has stations and none is chosen yet. */
    noStation: reason === 'no_station',
    isCached: data?.isCached ?? false,
    updatedAt: data?.updatedAt ?? null,
    ttlSeconds: data?.ttlSeconds ?? null,
    providerName: data?.providerName ?? '',
    positionBased: data?.positionBased ?? false,
    refetch,
  }
}
