import { useCallback, useEffect, useState } from 'react'

import { apiBaseUrl } from '@/config/api'
import { readErrorMessage } from '@/lib/api-error'
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
  const [candidates, setCandidates] = useState<VesselCandidatesResponse>(emptyVesselCandidates)
  const [loading, setLoading] = useState(true)
  const [error, setError] = useState<string | null>(null)

  const refresh = useCallback(async () => {
    try {
      const res = await fetch(`${apiBaseUrl}/api/vessel/candidates`)
      if (!res.ok) throw new Error(await readErrorMessage(res))
      const body = (await res.json()) as VesselCandidatesResponse
      setCandidates({
        engines: Array.isArray(body.engines) ? body.engines : [],
        batteries: Array.isArray(body.batteries) ? body.batteries : [],
        detectors: body.detectors ?? {},
      })
      setError(null)
    } catch (err) {
      setError(err instanceof Error ? err.message : String(err))
    } finally {
      setLoading(false)
    }
  }, [])

  useEffect(() => { void refresh() }, [refresh, refreshKey])

  return { candidates, loading, error, refresh }
}
