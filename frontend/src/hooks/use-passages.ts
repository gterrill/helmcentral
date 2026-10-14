import { useEffect, useState } from 'react'

import { invalidateQueries, useApiQuery } from '@/lib/api-query'
import {
  CRUISES_KEY,
  PASSAGES_KEY,
  cruisesPath,
  passagesApi,
  passagesPath,
  type CruiseDetailResponse,
} from '@/lib/passages-api'
import type { CruiseRecord, PassageDetail, PassageSummary } from '@/lib/passages'

const NO_PASSAGES: PassageSummary[] = []
const NO_CRUISES: CruiseRecord[] = []

/** Every visible passage and every Cruise, for the index and its pane. */
export function usePassageList() {
  const passages = useApiQuery({
    queryKey: PASSAGES_KEY,
    path: passagesPath(),
    select: (raw: { passages?: PassageSummary[] }) => raw.passages ?? NO_PASSAGES,
  })
  const cruises = useApiQuery({
    queryKey: CRUISES_KEY,
    path: cruisesPath(),
    select: (raw: { cruises?: CruiseRecord[] }) => raw.cruises ?? NO_CRUISES,
  })
  return {
    passages: passages.data ?? NO_PASSAGES,
    cruises: cruises.data ?? NO_CRUISES,
    loading: passages.isLoading || cruises.isLoading,
    error: (passages.error ?? cruises.error)?.message ?? null,
    refresh: async () => {
      await Promise.all([invalidateQueries(PASSAGES_KEY), invalidateQueries(CRUISES_KEY)])
    },
  }
}

/** One passage with its tracks. */
export function usePassageDetail(id: string) {
  const query = useApiQuery({
    queryKey: [...PASSAGES_KEY, id],
    path: passagesPath(id),
    select: (raw: { passage: PassageDetail }) => raw.passage,
  })
  return { passage: query.data, loading: query.isLoading, error: query.error?.message ?? null }
}

/** One Cruise with its passages and figures. */
export function useCruiseDetails(id: string) {
  const query = useApiQuery({
    queryKey: [...CRUISES_KEY, id],
    path: cruisesPath(id),
    select: (raw: CruiseDetailResponse) => raw,
  })
  return { data: query.data ?? null, loading: query.isLoading, error: query.error?.message ?? null }
}

/**
 * Full records (with tracks) for a set of passage ids, for the Cruise and
 * combined views. A set of ids cannot be a fixed number of keyed reads, so
 * this keeps its own state over the request helper (ADR 0173: not every
 * request is a cached read). The page remounts it on each visit, which reads
 * the records again.
 */
export function usePassageDetails(ids: string[]) {
  const [state, setState] = useState<{ key: string; passages: PassageDetail[]; error: string | null } | null>(null)
  const key = ids.join(',')

  useEffect(() => {
    let cancelled = false
    Promise.all(key ? key.split(',').map((id) => passagesApi.getPassage(id)) : [])
      .then((passages) => { if (!cancelled) setState({ key, passages, error: null }) })
      .catch((err) => {
        if (!cancelled) setState({ key, passages: [], error: err instanceof Error ? err.message : String(err) })
      })
    return () => { cancelled = true }
  }, [key])

  const current = state?.key === key ? state : null
  return { passages: current?.passages ?? [], loading: current === null, error: current?.error ?? null }
}
