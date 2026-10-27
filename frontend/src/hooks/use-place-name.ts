import { useApiQuery } from '@/lib/api-query'

interface PlaceNameResponse {
  name?: string
}

/**
 * The place the server names for the boat's position. Only *whether* there is
 * a fix matters, not where it is: /api/place-name takes no coordinates; the
 * backend reads position server-side. So the caller passes `hasFix`, and a
 * tile that shows the name does not have to re-render on every fix to ask
 * (ADR 0215). Keyed on the endpoint alone, so every caller shares one poll. A
 * failed poll is silent and the stale name stays.
 */
export function usePlaceName(hasFix: boolean, refreshInterval: number) {
  const { data } = useApiQuery<string | null>({
    queryKey: ['/api/place-name'],
    path: '/api/place-name',
    refetchInterval: refreshInterval * 1000,
    enabled: hasFix,
    select: (raw: PlaceNameResponse) => (raw.name && raw.name !== '' ? raw.name : null),
  })

  return data ?? null
}
