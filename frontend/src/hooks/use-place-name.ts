import { useApiQuery } from '@/lib/api-query'

interface PlaceNameResponse {
  name?: string
}

export function usePlaceName(latitude: number | null, longitude: number | null, refreshInterval: number) {
  // Only *whether* we have a fix matters, not where it is: /api/place-name
  // takes no coordinates; the backend reads position server-side. Keying on the
  // raw lat/lon would refetch on every GPS fix (~75 req/min) instead of one per
  // refreshInterval. A failed poll is silent and the stale name stays.
  const hasFix = latitude !== null && longitude !== null

  const { data } = useApiQuery<string | null>({
    queryKey: ['/api/place-name'],
    path: '/api/place-name',
    refetchInterval: refreshInterval * 1000,
    enabled: hasFix,
    select: (raw: PlaceNameResponse) => (raw.name && raw.name !== '' ? raw.name : null),
  })

  return data ?? null
}
