import { useEffect, useState } from 'react'

interface PlaceNameResponse {
  name?: string
}

export function usePlaceName(latitude: number | null, longitude: number | null, refreshInterval: number) {
  const [placeName, setPlaceName] = useState<string | null>(null)

  // Only *whether* we have a fix matters, not where it is: /api/place-name
  // takes no coordinates — the backend reads position server-side. Depending on
  // the raw lat/lon re-ran the effect on every GPS fix, and each re-run fetched
  // immediately before arming a fresh interval, so the interval never got to
  // elapse and the endpoint saw ~75 req/min instead of one per refreshInterval.
  const hasFix = latitude !== null && longitude !== null

  useEffect(() => {
    if (!hasFix) return

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
  }, [hasFix, refreshInterval])

  return placeName
}
