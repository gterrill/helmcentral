import { useEffect } from 'react'
import { useApiQuery } from '@/lib/api-query'

export interface PlaceNameProviderInfo {
  id: string
  name: string
  description: string
}

// Mirrors use-poi-providers.ts exactly, against the place-names-only
// listing endpoint (backend/place_name_provider.go's
// placeNameProvidersHandler) - every installed POI plugin that also
// exports place_name_at/search_places (ADR 0101), not every POI plugin.
const NONE: PlaceNameProviderInfo[] = []

export function usePlaceNameProviders() {
  const path = '/api/place-name-providers'
  const { data, isLoading, error } = useApiQuery<PlaceNameProviderInfo[]>({
    queryKey: [path],
    path,
    select: (raw: unknown) => (Array.isArray(raw) ? raw : NONE),
  })

  useEffect(() => {
    if (error) console.error('Error fetching place-name providers:', error)
  }, [error])

  return { providers: data ?? NONE, loading: isLoading, error: error?.message ?? null }
}
