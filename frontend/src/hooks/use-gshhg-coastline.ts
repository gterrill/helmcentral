import { useEffect, useState } from 'react'

import { apiRequest } from '@/lib/api-query'

export interface GshhgCoastlineState {
  data: GeoJSON.FeatureCollection | null
  loading: boolean
  error: string | null
}

let cachedCoastline: GeoJSON.FeatureCollection | null = null
let inFlight: Promise<GeoJSON.FeatureCollection> | null = null

async function fetchCoastline(): Promise<GeoJSON.FeatureCollection> {
  return apiRequest<GeoJSON.FeatureCollection>('/api/gshhg-coastline')
}

/**
 * Fetches the embedded GSHHG global coastline GeoJSON once per page
 * session. The data is static (compiled into the backend binary), so this
 * does not poll, and the result is cached at module scope so multiple
 * mounts of RoutePlannerMap don't re-fetch a multi-MB payload. It stays on
 * apiRequest rather than the query store, whose five-minute eviction would
 * bring the payload back more than once a session.
 */
export function useGshhgCoastline(): GshhgCoastlineState {
  const [data, setData] = useState<GeoJSON.FeatureCollection | null>(cachedCoastline)
  const [loading, setLoading] = useState(cachedCoastline === null)
  const [error, setError] = useState<string | null>(null)

  useEffect(() => {
    if (cachedCoastline !== null) {
      setData(cachedCoastline)
      setLoading(false)
      return
    }
    if (!inFlight) {
      inFlight = fetchCoastline()
    }
    inFlight
      .then((result) => {
        cachedCoastline = result
        setData(result)
        setError(null)
      })
      .catch((err) => {
        setError(err instanceof Error ? err.message : 'Failed to load coastline data')
      })
      .finally(() => {
        setLoading(false)
        inFlight = null
      })
  }, [])

  return { data, loading, error }
}
