import { useEffect } from 'react'
import { useApiQuery } from '@/lib/api-query'

export interface WeatherProviderInfo {
  id: string
  name: string
  description: string
}

const NONE: WeatherProviderInfo[] = []

export function useWeatherProviders() {
  const path = '/api/weather-providers'
  const { data, isLoading, error } = useApiQuery<WeatherProviderInfo[]>({
    queryKey: [path],
    path,
    select: (raw: unknown) => (Array.isArray(raw) ? raw : NONE),
  })

  useEffect(() => {
    if (error) console.error('Error fetching weather providers:', error)
  }, [error])

  return { providers: data ?? NONE, loading: isLoading, error: error?.message ?? null }
}
