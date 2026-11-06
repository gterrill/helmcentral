import { useCallback } from 'react'

import { apiRequest, invalidateQueries, useApiQuery } from '@/lib/api-query'
import type { OfflineCoastlineStatus, RequestPreview } from '@/lib/offline-coastline'

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

  // What a download request would do, counted by the server and stored
  // nowhere, so the confirm can say how many all-land squares it skips.
  const previewRequest = useCallback(
    (squares: string[]) => apiRequest<RequestPreview>(`${PATH}/request`, { method: 'POST', json: { squares, dry_run: true } }),
    [],
  )

  const requestSquares = useCallback(async (squares: string[]) => {
    const result = await apiRequest<RequestPreview>(`${PATH}/request`, { method: 'POST', json: { squares } })
    await invalidateQueries([PATH])
    return result
  }, [])

  const removeSquares = useCallback(async (squares: string[]) => {
    await apiRequest(`${PATH}/remove`, { method: 'POST', json: { squares } })
    await invalidateQueries([PATH])
  }, [])

  return {
    status: data,
    loading: isLoading,
    error: error?.message ?? null,
    update,
    clearCoverage,
    previewRequest,
    requestSquares,
    removeSquares,
  }
}
