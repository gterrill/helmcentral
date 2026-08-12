import { useEffect, useState } from 'react'

import { subscribeTelemetry } from '@/hooks/use-telemetry-stream'

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

export function useTanksState() {
  const [tanks, setTanks] = useState<TankLevel[]>([])
  const [loading, setLoading] = useState(true)

  useEffect(() => {
    const applyTanksState = (payload: unknown) => {
      try {
        const data = payload as TanksStateResponse
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

    return subscribeTelemetry('tanks-state', (raw) => {
      applyTanksState(JSON.parse(raw))
    })
  }, [])

  return { tanks, loading }
}
