import { useEffect } from 'react'
import { useApiQuery } from '@/lib/api-query'

export interface POIProviderInfo {
  id: string
  name: string
  description: string
}

const NONE: POIProviderInfo[] = []

export function usePOIProviders() {
  const path = '/api/poi-providers'
  const { data, isLoading, error } = useApiQuery<POIProviderInfo[]>({
    queryKey: [path],
    path,
    select: (raw: unknown) => (Array.isArray(raw) ? raw : NONE),
  })

  useEffect(() => {
    if (error) console.error('Error fetching POI providers:', error)
  }, [error])

  return { providers: data ?? NONE, loading: isLoading, error: error?.message ?? null }
}
