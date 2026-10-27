import { useCallback, useEffect, useState } from 'react'
import { apiRequest, setQueryData, useApiQuery } from '@/lib/api-query'

export type CZoneSwitch = {
  id: string
  display_name: string
  state: 0 | 1
  writable: boolean
}

type CZoneSwitchApi = {
  id?: unknown
  display_name?: unknown
  state?: unknown
  writable?: unknown
}

type CZoneSwitchesResponse = {
  switches?: CZoneSwitchApi[]
}

const SWITCHES_KEY = ['/api/czone/switches'] as const
const NO_SWITCHES: CZoneSwitch[] = []

/**
 * `enabled` gates both the initial fetch and the poll interval — with no
 * CZone widget or view on screen, nothing consumes this data, and polling
 * `/api/czone/switches` every few seconds anyway is pure noise. Defaults to
 * true so existing call sites that don't pass it keep polling unconditionally.
 */
export function useCZoneSwitches(refreshInterval: number, enabled = true) {
  const [pending, setPending] = useState<Set<string>>(new Set())

  // Deliberately not clearing the list on a failed poll: the poll runs every
  // 5s and flashing the tile empty on one blip would be worse than useless.
  // The query keeps the last good data beside the error, which communicates
  // staleness instead of the list going blank.
  const { data, isLoading, error, refetch } = useApiQuery<CZoneSwitch[]>({
    queryKey: SWITCHES_KEY,
    path: '/api/czone/switches',
    enabled,
    refetchInterval: refreshInterval * 1000,
    structuralSharing: true,
    select: (raw: CZoneSwitchesResponse) =>
      (Array.isArray(raw.switches) ? raw.switches : [])
        .filter(
          (s): s is CZoneSwitchApi & { id: string; display_name: string; state: 0 | 1 } =>
            typeof s.id === 'string' &&
            typeof s.display_name === 'string' &&
            (s.state === 0 || s.state === 1),
        )
        // writable is a control surface: a missing or non-boolean value
        // fails safe to false rather than defaulting to controllable, but
        // the switch itself is still shown (never dropped) so its state
        // stays visible.
        .map((s) => ({
          id: s.id,
          display_name: s.display_name,
          state: s.state,
          writable: typeof s.writable === 'boolean' ? s.writable : false,
        })),
  })

  useEffect(() => {
    if (error) console.error('Failed to fetch CZone switches:', error)
  }, [error])

  const toggleSwitch = useCallback(
    async (id: string, newState: 0 | 1) => {
      setPending((prev) => new Set(prev).add(id))
      // Optimistic update
      setQueryData<CZoneSwitchesResponse>(SWITCHES_KEY, (prev) => ({
        ...prev,
        switches: (prev?.switches ?? []).map((s) => (s.id === id ? { ...s, state: newState } : s)),
      }))
      try {
        await apiRequest<unknown>(`/api/czone/switches/${encodeURIComponent(id)}/state`, {
          method: 'PUT',
          json: { state: newState },
        })
      } catch {
        // Revert on failure
        await refetch()
      } finally {
        setPending((prev) => {
          const next = new Set(prev)
          next.delete(id)
          return next
        })
      }
    },
    [refetch],
  )

  return {
    switches: data ?? NO_SWITCHES,
    loading: isLoading,
    pending,
    error: error?.message ?? null,
    toggleSwitch,
  }
}
