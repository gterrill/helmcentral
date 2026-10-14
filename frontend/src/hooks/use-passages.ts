import { useCallback, useEffect, useRef, useState } from 'react'

import { passagesApi } from '@/lib/passages-api'
import type { CruiseRecord, CruiseStats, PassageDetail, PassageRecord, PassageSummary } from '@/lib/passages'

function messageOf(err: unknown): string {
  return err instanceof Error ? err.message : String(err)
}

/** Every visible passage and every Cruise, for the index and its pane. */
export function usePassageList() {
  const [passages, setPassages] = useState<PassageSummary[]>([])
  const [cruises, setCruises] = useState<CruiseRecord[]>([])
  const [loading, setLoading] = useState(true)
  const [error, setError] = useState<string | null>(null)
  const seq = useRef(0)

  const refresh = useCallback(async () => {
    const mine = (seq.current += 1)
    try {
      const [p, c] = await Promise.all([passagesApi.list(), passagesApi.listCruises()])
      if (mine !== seq.current) return
      setPassages(p)
      setCruises(c)
      setError(null)
    } catch (err) {
      if (mine === seq.current) setError(messageOf(err))
    } finally {
      if (mine === seq.current) setLoading(false)
    }
  }, [])

  useEffect(() => { void refresh() }, [refresh])
  return { passages, cruises, loading, error, refresh }
}

/** Full records (with tracks) for the given passage ids. */
export function usePassageDetails(ids: string[], reloadKey: number) {
  const [passages, setPassages] = useState<PassageDetail[]>([])
  const [loading, setLoading] = useState(true)
  const [error, setError] = useState<string | null>(null)
  const idsKey = ids.join(',')

  useEffect(() => {
    let cancelled = false
    setLoading(true)
    Promise.all(idsKey ? idsKey.split(',').map((id) => passagesApi.get(id)) : [])
      .then((list) => { if (!cancelled) { setPassages(list); setError(null) } })
      .catch((err) => { if (!cancelled) setError(messageOf(err)) })
      .finally(() => { if (!cancelled) setLoading(false) })
    return () => { cancelled = true }
  }, [idsKey, reloadKey])

  return { passages, loading, error }
}

/** One Cruise with its passages and figures. */
export function useCruiseDetails(id: string, reloadKey: number) {
  const [data, setData] = useState<{ cruise: CruiseRecord; passages: PassageRecord[]; stats: CruiseStats } | null>(null)
  const [loading, setLoading] = useState(true)
  const [error, setError] = useState<string | null>(null)

  useEffect(() => {
    let cancelled = false
    setLoading(true)
    passagesApi.getCruise(id)
      .then((d) => { if (!cancelled) { setData(d); setError(null) } })
      .catch((err) => { if (!cancelled) setError(messageOf(err)) })
      .finally(() => { if (!cancelled) setLoading(false) })
    return () => { cancelled = true }
  }, [id, reloadKey])

  return { data, loading, error }
}
