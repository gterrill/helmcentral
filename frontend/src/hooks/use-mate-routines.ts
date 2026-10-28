import { useCallback } from 'react'
import { apiRequest, invalidateQueries, useApiQuery } from '@/lib/api-query'

// Mate routines (ADR 0227): a standing instruction Mate carries out on a
// schedule. Creating and rewording one is a Mate proposal; this is the list,
// the on/off switch and Delete the operator has without Mate.

export interface MateRoutine {
  id: string
  name: string
  cadence: 'hourly' | 'daily'
  daily_time?: string
  focus: string
  enabled: boolean
  conversation_id: string
  last_run_at?: string
  last_outcome?: 'ran' | 'skipped'
  last_note?: string
  last_finding_at?: string
  /** When a note was last left that did not count as a run. */
  last_skip_at?: string
}

interface MateRoutinesApi {
  routines: MateRoutine[]
  max: number
  routines_enabled: boolean
  daily_cap_usd: number
  spent_24h_usd: number
  /** Set while a failed run whose cost is unknown holds routines back. */
  paused_until?: string
  /** Why routines are not running, in full. */
  problem?: string
}

const ROUTINES_PATH = '/api/assistant/routines'
const NO_ROUTINES: MateRoutine[] = []

/** The schedule in the operator's words. */
export function routineSchedule(r: Pick<MateRoutine, 'cadence' | 'daily_time'>): string {
  return r.cadence === 'daily' ? `Daily at ${r.daily_time ?? '--:--'} (boat time)` : 'Every hour'
}

/** Dollars as OpenRouter shows them: cents, or four places below one cent. */
export function formatUsd(value: number): string {
  return value > 0 && value < 0.01 ? `$${value.toFixed(4)}` : `$${value.toFixed(2)}`
}

export function useMateRoutines(enabled = true) {
  const { data, isLoading, error } = useApiQuery<MateRoutinesApi>({ queryKey: [ROUTINES_PATH], path: ROUTINES_PATH, enabled })

  const setEnabled = useCallback(async (id: string, on: boolean) => {
    await apiRequest(`${ROUTINES_PATH}/${encodeURIComponent(id)}`, { method: 'PATCH', json: { enabled: on } })
    await invalidateQueries([ROUTINES_PATH])
  }, [])
  const remove = useCallback(async (id: string) => {
    await apiRequest(`${ROUTINES_PATH}/${encodeURIComponent(id)}`, { method: 'DELETE' })
    await invalidateQueries([ROUTINES_PATH])
  }, [])

  return {
    routines: data?.routines ?? NO_ROUTINES,
    max: data?.max ?? 8,
    spent24hUsd: data?.spent_24h_usd ?? null,
    dailyCapUsd: data?.daily_cap_usd ?? null,
    pausedUntil: data?.paused_until ?? null,
    problem: data?.problem ?? null,
    loading: isLoading,
    error: error?.message ?? null,
    setEnabled,
    remove,
  }
}
