import { useCallback } from 'react'

import { apiRequest, invalidateQueries, useApiQuery } from '@/lib/api-query'
import type { OfflineCoastlineStatus } from '@/lib/offline-coastline'

const PATH = '/api/offline-coastline'

// The worker downloads one square every few seconds while it has work, so the
// page asks again at about that pace. It does not poll for a page that is not
// mounted: the hook is only used by the Offline coastline section.
const POLL_MS = 5_000

export function useOfflineCoastline() {
  const { data, error, isLoading } = useApiQuery<OfflineCoastlineStatus>({
    queryKey: [PATH],
    path: PATH,
    refetchInterval: POLL_MS,
  })

  // Both actions answer from the server's own words on failure (ApiError), so
  // the page shows what the server said rather than a guess.
  const update = useCallback(async () => {
    await apiRequest(`${PATH}/update`, { method: 'POST' })
    await invalidateQueries([PATH])
  }, [])

  const clearCoverage = useCallback(async () => {
    await apiRequest(`${PATH}/coverage`, { method: 'DELETE' })
    await invalidateQueries([PATH])
  }, [])

  return { status: data, loading: isLoading, error: error?.message ?? null, update, clearCoverage }
}
