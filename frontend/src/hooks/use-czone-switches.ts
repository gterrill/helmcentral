import { useCallback, useEffect, useState } from 'react'

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

/**
 * `enabled` gates both the initial fetch and the poll interval — with no
 * CZone widget or view on screen, nothing consumes this data, and polling
 * `/api/czone/switches` every few seconds anyway is pure noise. Defaults to
 * true so existing call sites that don't pass it keep polling unconditionally.
 */
export function useCZoneSwitches(refreshInterval: number, enabled = true) {
  const [switches, setSwitches] = useState<CZoneSwitch[]>([])
  const [loading, setLoading] = useState(true)
  const [pending, setPending] = useState<Set<string>>(new Set())
  const [error, setError] = useState<string | null>(null)

  const fetchSwitches = useCallback(async () => {
    try {
      const response = await fetch('/api/czone/switches')
      if (!response.ok) {
        const body = (await response.json().catch(() => ({}))) as { error?: string }
        throw new Error(body.error ?? 'Failed to fetch CZone switches')
      }
      const data = (await response.json()) as CZoneSwitchesResponse
      const list = Array.isArray(data.switches) ? data.switches : []
      setSwitches(
        list
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
      )
      setError(null)
    } catch (err) {
      console.error('Failed to fetch CZone switches:', err)
      // Deliberately not clearing `switches` here: the poll runs every 5s and
      // flashing the tile empty on one blip would be worse than useless. The
      // error state communicates staleness instead of the list going blank.
      setError(err instanceof Error ? err.message : 'Failed to fetch CZone switches')
    } finally {
      setLoading(false)
    }
  }, [])

  useEffect(() => {
    if (!enabled) return
    void fetchSwitches()
    const timer = window.setInterval(() => void fetchSwitches(), refreshInterval * 1000)
    return () => window.clearInterval(timer)
  }, [fetchSwitches, refreshInterval, enabled])

  const toggleSwitch = useCallback(
    async (id: string, newState: 0 | 1) => {
      setPending((prev) => new Set(prev).add(id))
      // Optimistic update
      setSwitches((prev) => prev.map((s) => (s.id === id ? { ...s, state: newState } : s)))
      try {
        const response = await fetch(`/api/czone/switches/${encodeURIComponent(id)}/state`, {
          method: 'PUT',
          headers: { 'Content-Type': 'application/json' },
          body: JSON.stringify({ state: newState }),
        })
        if (!response.ok) {
          // Revert on failure
          await fetchSwitches()
        }
      } catch {
        await fetchSwitches()
      } finally {
        setPending((prev) => {
          const next = new Set(prev)
          next.delete(id)
          return next
        })
      }
    },
    [fetchSwitches],
  )

  return { switches, loading, pending, error, toggleSwitch }
}
