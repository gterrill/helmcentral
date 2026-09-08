import { useEffect, useState } from 'react'

import { ageFromPayload } from '@/lib/staleness'

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

/** Reads a `number | null` field the backend sends as `number | null` already
 * (unlike an age, this is not `-1`-sentinelled: fuel_volume_m3 and friends are
 * `null` on the wire whenever the derivation is absent, ADR 0084). */
function numberOrNull(value: unknown): number | null {
  return typeof value === 'number' && Number.isFinite(value) ? value : null
}

export function useTanksState() {
  const [tanks, setTanks] = useState<TankLevel[]>([])
  const [loading, setLoading] = useState(true)
  // Freshest of the per-tank ages (ADR 0068); null when no tank
  // publishes a timestamp.
  const [lastUpdateAgeS, setLastUpdateAgeS] = useState<number | null>(null)
  // The three derived fuel figures the Tanks tile footer reads (ADR 0084),
  // plus each figure's own age -- null/`-1` alike mean absent or unknown,
  // never a guessed number.
  const [fuelVolumeM3, setFuelVolumeM3] = useState<number | null>(null)
  const [fuelVolumeAgeS, setFuelVolumeAgeS] = useState<number | null>(null)
  const [fuelTimeToEmptyS, setFuelTimeToEmptyS] = useState<number | null>(null)
  const [fuelRangeM, setFuelRangeM] = useState<number | null>(null)
  const [fuelDerivedAgeS, setFuelDerivedAgeS] = useState<number | null>(null)

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
        setLastUpdateAgeS(ageFromPayload((data as { last_update_age_s?: unknown }).last_update_age_s))

        const fuel = data as {
          fuel_volume_m3?: unknown
          fuel_volume_age_s?: unknown
          fuel_time_to_empty_s?: unknown
          fuel_range_m?: unknown
          fuel_derived_age_s?: unknown
        }
        setFuelVolumeM3(numberOrNull(fuel.fuel_volume_m3))
        setFuelVolumeAgeS(ageFromPayload(fuel.fuel_volume_age_s))
        setFuelTimeToEmptyS(numberOrNull(fuel.fuel_time_to_empty_s))
        setFuelRangeM(numberOrNull(fuel.fuel_range_m))
        setFuelDerivedAgeS(ageFromPayload(fuel.fuel_derived_age_s))
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
    fuelVolumeM3,
    fuelVolumeAgeS,
    fuelTimeToEmptyS,
    fuelRangeM,
    fuelDerivedAgeS,
  }
}
