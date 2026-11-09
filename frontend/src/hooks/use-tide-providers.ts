import { useEffect } from 'react'
import { useApiQuery } from '@/lib/api-query'

export interface TideProviderInfo {
  id: string
  name: string
  description: string
  /** True for a provider with no stations to choose: it follows the boat's position (ADR 0273). */
  position_based?: boolean
  /** What its heights are measured from; chart datum when left out. */
  height_datum?: string
  /** True for the provider a blank setting means; the server marks it, so no screen holds its id. */
  default?: boolean
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
