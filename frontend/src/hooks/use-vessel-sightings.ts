import { useEffect } from 'react'

import { useApiQuery } from '@/lib/api-query'

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
// vessel identity key (the vessel's MMSI - see backend vesselContactKey).
// It only fetches when key is non-null, so the popover
// in nearby-vessels-tile.tsx can pass null while closed and the actual key
// only once opened, instead of fetching on every poll tick.
export function useVesselSightings(key: string | null) {
  const path = `/api/nearby-vessels/${encodeURIComponent(key ?? '')}/sightings`
  const { data, error, isLoading } = useApiQuery<VesselSighting[]>({
    queryKey: [path],
    path,
    enabled: !!key,
    select: (body: VesselSightingsResponse) => (Array.isArray(body.sightings) ? body.sightings : []),
  })

  useEffect(() => {
    if (error) console.error('Failed to fetch vessel sightings:', error)
  }, [error])

  return { sightings: error ? [] : (data ?? []), loading: isLoading }
}
