import { useEffect, useRef } from 'react'

import { useApiQuery } from '@/lib/api-query'
import { emptyVesselCandidates, type VesselCandidatesResponse } from '@/lib/vessel-settings'

/**
 * What the boat is publishing right now that Settings -> Vessel can offer as
 * an engine or a house bank (GET /api/vessel/candidates), plus each
 * detector's setup status. The engines and house bank themselves are part of
 * the settings payload and save with the Save bar; this is the read-only
 * picker data beside them.
 *
 * The detector status is computed from the saved block, so the caller passes
 * `refreshKey` (anything that changes when a save lands) to re-read it.
 * Plain fetch-and-throw: a failure is shown, not masked.
 */
export function useVesselCandidates(refreshKey?: unknown) {
  const { data, isLoading, error, refetch } = useApiQuery<VesselCandidatesResponse>({
    queryKey: ['/api/vessel/candidates'],
    path: '/api/vessel/candidates',
    select: (body: VesselCandidatesResponse) => ({
      engines: Array.isArray(body.engines) ? body.engines : [],
      batteries: Array.isArray(body.batteries) ? body.batteries : [],
      alternators: Array.isArray(body.alternators) ? body.alternators : [],
      generators: Array.isArray(body.generators) ? body.generators : [],
      ac: Array.isArray(body.ac) ? body.ac : [],
      chargers: Array.isArray(body.chargers) ? body.chargers : [],
      inverters: Array.isArray(body.inverters) ? body.inverters : [],
      solar: Array.isArray(body.solar) ? body.solar : [],
      detectors: body.detectors ?? {},
    }),
  })

  // A mount already revalidates, so only a change of key reads again.
  const seenKey = useRef(refreshKey)
  useEffect(() => {
    if (Object.is(seenKey.current, refreshKey)) return
    seenKey.current = refreshKey
    void refetch()
  }, [refetch, refreshKey])

  return {
    candidates: data ?? emptyVesselCandidates,
    loading: isLoading,
    error: error?.message ?? null,
    refresh: refetch,
  }
}
