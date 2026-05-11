import { useEffect, useState } from 'react'

export type TankLevel = {
  id: string
  label: string
  category: string
  kind: 'water' | 'fuel' | 'waste'
  level_percent: number
}

type TanksStateResponse = {
  tanks?: TankLevel[]
}

export function useTanksState(refreshInterval: number) {
  const [tanks, setTanks] = useState<TankLevel[]>([])
  const [loading, setLoading] = useState(true)

  useEffect(() => {
    const fetchTanksState = async () => {
      try {
        const response = await fetch('/api/tanks-state')
        if (!response.ok) {
          throw new Error('Failed to fetch tanks state')
        }

        const data = (await response.json()) as TanksStateResponse
        const list = Array.isArray(data.tanks) ? data.tanks : []

        const valid = list.filter(
          (tank): tank is TankLevel =>
            typeof tank.id === 'string' &&
            typeof tank.label === 'string' &&
            typeof tank.category === 'string' &&
            (tank.kind === 'water' || tank.kind === 'fuel' || tank.kind === 'waste') &&
            typeof tank.level_percent === 'number' &&
            Number.isFinite(tank.level_percent),
        )

        setTanks(valid)
      } catch (err) {
        console.error('Failed to fetch tanks state:', err)
      } finally {
        setLoading(false)
      }
    }

    void fetchTanksState()
    const timer = window.setInterval(() => {
      void fetchTanksState()
    }, refreshInterval * 1000)

    return () => {
      window.clearInterval(timer)
    }
  }, [refreshInterval])

  return { tanks, loading }
}
