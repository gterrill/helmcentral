import { useEffect, useState } from 'react'

export type VesselSighting = {
  seen_at: string
  lat: number
  lon: number
  geoname: string
  nav_context: string
}

type VesselSightingsResponse = {
  sightings?: VesselSighting[]
}

// useVesselSightings lazily fetches the sighting history for a single
// vessel identity key (MMSI, or "name:..." fallback - see backend
// vesselContactKey). It only fetches when key is non-null, so the popover
// in nearby-vessels-tile.tsx can pass null while closed and the actual key
// only once opened, instead of fetching on every poll tick.
export function useVesselSightings(key: string | null) {
  const [sightings, setSightings] = useState<VesselSighting[]>([])
  const [loading, setLoading] = useState(false)

  useEffect(() => {
    if (!key) {
      setSightings([])
      setLoading(false)
      return
    }

    let cancelled = false
    setLoading(true)

    const fetchSightings = async () => {
      try {
        const response = await fetch(`/api/nearby-vessels/${encodeURIComponent(key)}/sightings`)
        if (!response.ok) {
          throw new Error('Failed to fetch vessel sightings')
        }

        const data = (await response.json()) as VesselSightingsResponse
        if (!cancelled) {
          setSightings(Array.isArray(data.sightings) ? data.sightings : [])
        }
      } catch (err) {
        console.error('Failed to fetch vessel sightings:', err)
        if (!cancelled) {
          setSightings([])
        }
      } finally {
        if (!cancelled) {
          setLoading(false)
        }
      }
    }

    void fetchSightings()

    return () => {
      cancelled = true
    }
  }, [key])

  return { sightings, loading }
}
