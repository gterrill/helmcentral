import { useEffect, useState } from 'react'

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
  const [stations, setStations] = useState<TideStation[]>([])
  const [loading, setLoading] = useState(false)

  useEffect(() => {
    if (!provider || query.trim().length < 2) {
      setStations([])
      setLoading(false)
      return
    }

    setLoading(true)
    const timeout = setTimeout(() => {
      const fetchStations = async () => {
        try {
          const params = new URLSearchParams({ provider, q: query.trim() })
          const response = await fetch(`/api/tide-stations?${params.toString()}`)
          if (!response.ok) {
            throw new Error(`HTTP error! status: ${response.status}`)
          }

          const data = (await response.json()) as TideStationApi[]
          setStations(
            Array.isArray(data)
              ? data.map((station) => ({
                  stationId: station.station_id ?? '',
                  name: station.name ?? '',
                  state: station.state,
                  lat: typeof station.lat === 'number' ? station.lat : 0,
                  lon: typeof station.lon === 'number' ? station.lon : 0,
                  timezone: station.timezone,
                }))
              : [],
          )
        } catch (error) {
          console.error('Error fetching tide stations:', error)
          setStations([])
        } finally {
          setLoading(false)
        }
      }

      void fetchStations()
    }, 300)

    return () => clearTimeout(timeout)
  }, [provider, query])

  return { stations, loading }
}
