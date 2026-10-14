import { useEffect } from 'react'
import { useApiQuery } from '@/lib/api-query'

export interface WaveProviderInfo {
  id: string
  name: string
  description: string
}

const NONE: WaveProviderInfo[] = []

export function useWaveProviders() {
  const path = '/api/wave-providers'
  const { data, isLoading, error } = useApiQuery<WaveProviderInfo[]>({
    queryKey: [path],
    path,
    select: (raw: unknown) => (Array.isArray(raw) ? raw : NONE),
  })

  useEffect(() => {
    if (error) console.error('Error fetching wave providers:', error)
  }, [error])

  return { providers: data ?? NONE, loading: isLoading, error: error?.message ?? null }
}
