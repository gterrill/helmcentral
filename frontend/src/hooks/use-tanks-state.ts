import { useEffect, useState } from 'react'

import { ageFromPayload } from '@/lib/staleness'

import { subscribeTelemetry } from '@/hooks/use-telemetry-stream'

export type TankLevel = {
  id: string
  label: string
  category: string
  kind: 'water' | 'fuel' | 'waste'
  level_percent: number
  /** The tank's resolved levels (ADR 0214): what its colours and its alarms both use. */
  warn_percent: number
  alarm_percent: number
  /** Silences the alarms only; the tile still colours by the levels. */
  alarms_off: boolean
}

type TanksStateResponse = {
  tanks?: TankLevel[]
}

export function useTanksState() {
  const [tanks, setTanks] = useState<TankLevel[]>([])
  const [loading, setLoading] = useState(true)
  // Freshest of the per-tank ages (ADR 0068); null when no tank
  // publishes a timestamp.
  const [lastUpdateAgeS, setLastUpdateAgeS] = useState<number | null>(null)

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
            Number.isFinite(tank.level_percent) &&
            Number.isFinite(tank.warn_percent) &&
            Number.isFinite(tank.alarm_percent) &&
            typeof tank.alarms_off === 'boolean',
        )

        setTanks(valid)
        setLastUpdateAgeS(ageFromPayload((data as { last_update_age_s?: unknown }).last_update_age_s))
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

  return {
    tanks,
    loading,
    lastUpdateAgeS,
  }
}
