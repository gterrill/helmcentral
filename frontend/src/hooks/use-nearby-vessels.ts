import { useEffect, useState } from 'react'

export type NearbyVessel = {
  name: string
  range_ft: number
  age_seconds: number
  sog_knots?: number
  lat?: number
  lon?: number
}

type NearbyVesselsResponse = {
  vessels?: NearbyVessel[]
}

export function useNearbyVessels(refreshInterval: number) {
  const [vessels, setVessels] = useState<NearbyVessel[]>([])
  const [loading, setLoading] = useState(true)

  useEffect(() => {
    const fetchNearbyVessels = async () => {
      try {
        const response = await fetch('/api/nearby-vessels')
        if (!response.ok) {
          throw new Error('Failed to fetch nearby vessels')
        }

        const data = (await response.json()) as NearbyVesselsResponse
        const list = Array.isArray(data.vessels) ? data.vessels : []
        setVessels(
          list.filter(
            (item) =>
              typeof item.name === 'string' &&
              typeof item.range_ft === 'number' &&
              Number.isFinite(item.range_ft) &&
              typeof item.age_seconds === 'number' &&
              Number.isFinite(item.age_seconds),
          ).map((item) => ({
            ...item,
            lat: typeof item.lat === 'number' && Number.isFinite(item.lat) ? item.lat : undefined,
            lon: typeof item.lon === 'number' && Number.isFinite(item.lon) ? item.lon : undefined,
          })),
        )
      } catch (err) {
        console.error('Failed to fetch nearby vessels:', err)
      } finally {
        setLoading(false)
      }
    }

    void fetchNearbyVessels()
    const timer = window.setInterval(() => {
      void fetchNearbyVessels()
    }, refreshInterval * 1000)

    return () => {
      window.clearInterval(timer)
    }
  }, [refreshInterval])

  return { vessels, loading }
}
