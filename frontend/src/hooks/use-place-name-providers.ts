import { useEffect, useState } from 'react'

export interface PlaceNameProviderInfo {
  id: string
  name: string
  description: string
}

// Mirrors use-poi-providers.ts exactly, against the place-names-only
// listing endpoint (backend/place_name_provider.go's
// placeNameProvidersHandler) - every installed POI plugin that also
// exports place_name_at/search_places (ADR 0101), not every POI plugin.
export function usePlaceNameProviders() {
  const [providers, setProviders] = useState<PlaceNameProviderInfo[]>([])
  const [loading, setLoading] = useState(true)

  useEffect(() => {
    const fetchProviders = async () => {
      try {
        const response = await fetch('/api/place-name-providers')
        if (!response.ok) {
          throw new Error(`HTTP error! status: ${response.status}`)
        }

        const data = await response.json()
        setProviders(Array.isArray(data) ? data : [])
      } catch (error) {
        console.error('Error fetching place-name providers:', error)
      } finally {
        setLoading(false)
      }
    }

    void fetchProviders()
  }, [])

  return { providers, loading }
}
