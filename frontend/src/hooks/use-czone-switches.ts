import { useCallback, useEffect, useState } from 'react'

export type CZoneSwitch = {
  id: string
  display_name: string
  state: 0 | 1
}

type CZoneSwitchesResponse = {
  switches?: CZoneSwitch[]
}

export function useCZoneSwitches(refreshInterval: number) {
  const [switches, setSwitches] = useState<CZoneSwitch[]>([])
  const [loading, setLoading] = useState(true)
  const [pending, setPending] = useState<Set<string>>(new Set())

  const fetchSwitches = useCallback(async () => {
    try {
      const response = await fetch('/api/czone/switches')
      if (!response.ok) throw new Error('Failed to fetch CZone switches')
      const data = (await response.json()) as CZoneSwitchesResponse
      const list = Array.isArray(data.switches) ? data.switches : []
      setSwitches(
        list.filter(
          (s): s is CZoneSwitch =>
            typeof s.id === 'string' &&
            typeof s.display_name === 'string' &&
            (s.state === 0 || s.state === 1),
        ),
      )
    } catch (err) {
      console.error('Failed to fetch CZone switches:', err)
    } finally {
      setLoading(false)
    }
  }, [])

  useEffect(() => {
    void fetchSwitches()
    const timer = window.setInterval(() => void fetchSwitches(), refreshInterval * 1000)
    return () => window.clearInterval(timer)
  }, [fetchSwitches, refreshInterval])

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

  return { switches, loading, pending, toggleSwitch }
}
