import { useEffect, useState } from 'react'

interface PlaceNameResponse {
  name?: string
}

export function usePlaceName(latitude: number | null, longitude: number | null, refreshInterval: number) {
  const [placeName, setPlaceName] = useState<string | null>(null)

  useEffect(() => {
    if (latitude === null || longitude === null) return

    const fetchPlaceName = async () => {
      try {
        const response = await fetch('/api/place-name')
        if (!response.ok) return
        const data = (await response.json()) as PlaceNameResponse
        setPlaceName(data.name && data.name !== '' ? data.name : null)
      } catch {
        // ignore — stale value stays
      }
    }

    fetchPlaceName()
    const interval = window.setInterval(fetchPlaceName, refreshInterval * 1000)
    return () => window.clearInterval(interval)
  }, [latitude, longitude, refreshInterval])

  return placeName
}
