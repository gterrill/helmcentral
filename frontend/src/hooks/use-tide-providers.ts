import { useEffect } from 'react'
import { useApiQuery } from '@/lib/api-query'

export interface TideProviderInfo {
  id: string
  name: string
  description: string
}

const NONE: TideProviderInfo[] = []

export function useTideProviders() {
  const path = '/api/tide-providers'
  const { data, isLoading, error } = useApiQuery<TideProviderInfo[]>({
    queryKey: [path],
    path,
    select: (raw: unknown) => (Array.isArray(raw) ? raw : NONE),
  })

  useEffect(() => {
    if (error) console.error('Error fetching tide providers:', error)
  }, [error])

  return { providers: data ?? NONE, loading: isLoading, error: error?.message ?? null }
}
