import { useEffect, useState } from 'react'

import { useApiQuery } from '@/lib/api-query'

export interface TideStation {
  stationId: string
  name: string
  state?: string
  lat: number
  lon: number
  timezone?: string
}

interface TideStationApi {
  station_id?: string
  name?: string
  state?: string
  lat?: number
  lon?: number
  timezone?: string
}

export function useTideStations(provider: string, query: string) {
  const trimmed = query.trim()
  const eligible = !!provider && trimmed.length >= 2
  const wanted = `${provider}\n${trimmed}`

  // The search waits 300 ms after the last keystroke before it asks.
  const [settled, setSettled] = useState('')
  useEffect(() => {
    if (!eligible) {
      setSettled('')
      return
    }
    const timeout = setTimeout(() => setSettled(wanted), 300)
    return () => clearTimeout(timeout)
  }, [eligible, wanted])

  const ready = eligible && settled === wanted
  const params = new URLSearchParams({ provider, q: trimmed })
  const { data, error, isLoading } = useApiQuery<TideStation[]>({
    queryKey: ['/api/tide-stations', { provider, q: trimmed }],
    path: `/api/tide-stations?${params.toString()}`,
    enabled: ready,
    select: (raw: TideStationApi[]) =>
      Array.isArray(raw)
        ? raw.map((station) => ({
            stationId: station.station_id ?? '',
            name: station.name ?? '',
            state: station.state,
            lat: typeof station.lat === 'number' ? station.lat : 0,
            lon: typeof station.lon === 'number' ? station.lon : 0,
            timezone: station.timezone,
          }))
        : [],
  })

  useEffect(() => {
    if (error) console.error('Error fetching tide stations:', error)
  }, [error])

  return {
    stations: ready && !error ? (data ?? []) : [],
    loading: eligible && (!ready || isLoading),
  }
}
