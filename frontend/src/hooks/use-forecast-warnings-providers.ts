import { useEffect } from 'react'
import { useApiQuery } from '@/lib/api-query'

export interface ForecastWarningsProviderInfo {
  id: string
  name: string
  description: string
}

const NONE: ForecastWarningsProviderInfo[] = []

export function useForecastWarningsProviders() {
  const path = '/api/forecast-warnings-providers'
  const { data, isLoading, error } = useApiQuery<ForecastWarningsProviderInfo[]>({
    queryKey: [path],
    path,
    select: (raw: unknown) => (Array.isArray(raw) ? raw : NONE),
  })

  useEffect(() => {
    if (error) console.error('Error fetching forecast warnings providers:', error)
  }, [error])

  return { providers: data ?? NONE, loading: isLoading, error: error?.message ?? null }
}
