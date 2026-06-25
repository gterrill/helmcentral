import { useEffect, useState } from 'react'

export interface GshhgCoastlineState {
  data: GeoJSON.FeatureCollection | null
  loading: boolean
  error: string | null
}

let cachedCoastline: GeoJSON.FeatureCollection | null = null
let inFlight: Promise<GeoJSON.FeatureCollection> | null = null

async function fetchCoastline(): Promise<GeoJSON.FeatureCollection> {
  const res = await fetch('/api/gshhg-coastline')
  if (!res.ok) {
    throw new Error(`HTTP error! status: ${res.status}`)
  }
  return (await res.json()) as GeoJSON.FeatureCollection
}

/**
 * Fetches the embedded GSHHG global coastline GeoJSON once per page
 * session. The data is static (compiled into the backend binary), so this
 * does not poll, and the result is cached at module scope so multiple
 * mounts of RoutePlannerMap don't re-fetch a multi-MB payload.
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
