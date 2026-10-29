import { useCallback, useEffect, useRef, useState } from 'react'

import { apiRequest, declareQuery, invalidateQueries, useApiQuery } from '@/lib/api-query'
import type {
  WatchDetail,
  WatchDraft,
  WatchList,
  WatchRecord,
  WatchRun,
  WatchSchedulePreview,
} from '@/lib/watches'

// The Watches pages' data (ADR 0227). One module-level prefix, so a change to
// a watch, a run now, or Mate's applied proposal refreshes the index and every
// open details page with one call.

export const WATCHES_PATH = '/api/watches'
export const WATCHES_KEY = [WATCHES_PATH] as const

declareQuery([WATCHES_PATH], { structuralSharing: true })

/** Reads the watches again: the index and every details page open. */
export function invalidateWatches(): Promise<void> {
  return invalidateQueries(WATCHES_KEY)
}

const NO_WATCHES: WatchRecord[] = []

export function useWatchList(enabled = true) {
  const query = useApiQuery<WatchList>({
    queryKey: WATCHES_KEY,
    path: WATCHES_PATH,
    enabled,
    // A one-off watch ends and a scheduled one runs without the page asking.
    refetchInterval: 30_000,
  })
  const data = query.data
  return {
    watches: data?.watches ?? NO_WATCHES,
    /** The most scheduled watches allowed, null until the server has said. */
    max: data?.max ?? null,
    cost24h: data?.cost_24h ?? null,
    storageBytes: data?.storage_bytes ?? null,
    problem: data?.problem ?? null,
    loading: query.isLoading,
    error: query.error?.message ?? null,
    refresh: query.refetch,
  }
}

const PAGE = 20
const NO_RUNS: WatchRun[] = []

function newestFirst(a: WatchRun, b: WatchRun): number {
  const d = Date.parse(b.started_at) - Date.parse(a.started_at)
  if (d !== 0) return d
  return a.id < b.id ? 1 : a.id > b.id ? -1 : 0
}

interface RunPool {
  id: string | null
  /** Every run seen for this watch, by id: the first page as it was refreshed, and each older page. */
  runs: Map<string, WatchRun>
  deleted: Set<string>
  /** The refreshed first page already merged in. */
  merged: WatchDetail | undefined
  /** Whether runs older than the oldest shown remain, once an older page has been read. */
  olderMore: boolean | null
}

const emptyPool = (id: string | null): RunPool => ({ id, runs: new Map(), deleted: new Set(), merged: undefined, olderMore: null })

/**
 * One watch with its runs, newest first. The first page is a cached read the
 * rest of the app can refresh. "Show older" asks for the runs older than the
 * oldest one shown (a cursor, not a count), so a run made between two page
 * loads cannot repeat or drop one. Runs are kept by id across every refresh,
 * so a run pushed off the first page by a new one is still there.
 */
export function useWatchDetail(id: string | null) {
  const query = useApiQuery<WatchDetail>({
    queryKey: [WATCHES_PATH, id],
    path: `${WATCHES_PATH}/${encodeURIComponent(id ?? '')}?limit=${PAGE}`,
    enabled: id !== null,
    refetchInterval: 30_000,
  })
  const [pool, setPool] = useState<RunPool>(() => emptyPool(id))
  const [loadingOlder, setLoadingOlder] = useState(false)
  const [olderError, setOlderError] = useState<string | null>(null)

  const data = query.data
  if (pool.id !== id) {
    setPool(emptyPool(id))
  } else if (data && pool.merged !== data) {
    const runs = new Map(pool.runs)
    for (const r of data.runs) if (!pool.deleted.has(r.id)) runs.set(r.id, r)
    setPool({ ...pool, runs, merged: data })
  }

  const live = pool.id === id ? pool : emptyPool(id)
  const all = [...live.runs.values()].sort(newestFirst)
  const runs = all.length > 0 ? all : NO_RUNS
  const hasOlder = live.olderMore ?? data?.has_older ?? false

  const oldest = all.length > 0 ? all[all.length - 1] : null
  const oldestRef = useRef(oldest)
  oldestRef.current = oldest

  const showOlder = useCallback(async () => {
    const last = oldestRef.current
    if (id === null || last === null) return
    setLoadingOlder(true)
    setOlderError(null)
    try {
      const before = `${encodeURIComponent(last.started_at)},${encodeURIComponent(last.id)}`
      const page = await apiRequest<WatchDetail>(`${WATCHES_PATH}/${encodeURIComponent(id)}?limit=${PAGE}&before=${before}`)
      setPool((prev) => {
        if (prev.id !== id) return prev
        const next = new Map(prev.runs)
        for (const r of page.runs) if (!prev.deleted.has(r.id)) next.set(r.id, r)
        return { ...prev, runs: next, olderMore: page.has_older }
      })
    } catch (err) {
      setOlderError(err instanceof Error ? err.message : 'Could not load older runs.')
    } finally {
      setLoadingOlder(false)
    }
  }, [id])

  // A deleted run leaves whichever page holds it, and a refresh that still
  // lists it (one that began before the delete) does not bring it back.
  const dropRun = useCallback((runId: string) => {
    setPool((prev) => {
      const next = new Map(prev.runs)
      next.delete(runId)
      return { ...prev, runs: next, deleted: new Set(prev.deleted).add(runId) }
    })
  }, [])

  return {
    watch: data?.watch ?? null,
    runs,
    hasOlder,
    loading: query.isLoading,
    error: query.error?.message ?? null,
    refresh: query.refetch,
    showOlder,
    loadingOlder,
    olderError,
    dropRun,
  }
}

/** The writes. Each refreshes the watches before it resolves; the server's own message is what a refusal says. */
export const watchesApi = {
  async create(draft: WatchDraft): Promise<WatchRecord> {
    const created = await apiRequest<WatchRecord>(WATCHES_PATH, { method: 'POST', json: draft })
    await invalidateWatches()
    return created
  },
  /** A one-off watch cannot be changed; the server answers not found. */
  async change(id: string, draft: WatchDraft): Promise<WatchRecord> {
    const changed = await apiRequest<WatchRecord>(`${WATCHES_PATH}/${encodeURIComponent(id)}`, {
      method: 'PATCH',
      json: draft,
    })
    await invalidateWatches()
    return changed
  },
  async remove(id: string): Promise<void> {
    await apiRequest<void>(`${WATCHES_PATH}/${encodeURIComponent(id)}`, { method: 'DELETE' })
    await invalidateWatches()
  },
  /** Waits for Mate's review, which can take a minute. */
  async runNow(id: string): Promise<WatchRun> {
    try {
      return await apiRequest<WatchRun>(`${WATCHES_PATH}/${encodeURIComponent(id)}/run`, { method: 'POST' })
    } finally {
      await invalidateWatches()
    }
  },
  async removeRun(id: string, runId: string): Promise<void> {
    await apiRequest<void>(`${WATCHES_PATH}/${encodeURIComponent(id)}/runs/${encodeURIComponent(runId)}`, { method: 'DELETE' })
    await invalidateWatches()
  },
}

const PREVIEW_DELAY_MS = 400

export interface SchedulePreviewState {
  preview: WatchSchedulePreview | null
  /** Set when the preview itself could not be asked for. */
  error: string | null
  /** True from an edit until its answer lands, so Save cannot go ahead on an unchecked schedule. */
  pending: boolean
}

/** What a schedule means and when it next runs, asked of the server after a pause in typing. */
export function useSchedulePreview(schedule: string, enabled = true): SchedulePreviewState {
  const [state, setState] = useState<{ schedule: string; preview: WatchSchedulePreview | null; error: string | null } | null>(null)
  useEffect(() => {
    if (!enabled) return
    let cancelled = false
    const timer = setTimeout(() => {
      apiRequest<WatchSchedulePreview>(`${WATCHES_PATH}/schedule-preview`, { method: 'POST', json: { schedule } })
        .then((preview) => { if (!cancelled) setState({ schedule, preview, error: null }) })
        .catch((err: unknown) => {
          if (!cancelled) setState({ schedule, preview: null, error: err instanceof Error ? err.message : 'Could not check the schedule.' })
        })
    }, PREVIEW_DELAY_MS)
    return () => {
      cancelled = true
      clearTimeout(timer)
    }
  }, [schedule, enabled])

  const answered = state !== null && state.schedule === schedule
  return {
    preview: answered ? state.preview : null,
    error: answered ? state.error : null,
    pending: enabled && !answered,
  }
}
