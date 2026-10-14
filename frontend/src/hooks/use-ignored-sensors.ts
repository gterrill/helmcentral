import { useCallback } from 'react'

import { apiRequest, setQueryData, useApiQuery } from '@/lib/api-query'
import type { SensorHealthEntry } from '@/hooks/use-alarms'

/**
 * The sensor-health checks' own exclusion list (backend/alarm_ignored_sensors.go):
 * a SignalK path (excluded from the frozen check) or a $source id (excluded
 * from the silent-source check), added from the frozen/impossible/silent-
 * source alarm card's own "Ignore this sensor" action. Not a settings list -
 * this hook is used both by alarms-drawer.tsx (the ignore action itself) and
 * the Alarms settings section (seeing and un-ignoring the list).
 */
interface IgnoredSensorsBody {
  identifiers?: string[]
  sensors?: SensorHealthEntry[]
}

const PATH = '/api/alarms/ignored-sensors'
const NO_IDENTIFIERS: string[] = []
const NO_SENSORS: SensorHealthEntry[] = []

interface IgnoredSensors {
  identifiers: string[]
  sensors: SensorHealthEntry[]
}

function normalise(body: IgnoredSensorsBody): IgnoredSensors {
  return {
    identifiers: Array.isArray(body.identifiers) ? body.identifiers : NO_IDENTIFIERS,
    sensors: Array.isArray(body.sensors) ? body.sensors : NO_SENSORS,
  }
}

export function useIgnoredSensors() {
  // The sensors are the same identifiers with the names the operator knows
  // them by; the list in Settings shows these and never the raw identifier.
  const { data, isLoading, error, refetch } = useApiQuery<IgnoredSensors>({
    queryKey: [PATH],
    path: PATH,
    select: normalise,
  })

  const ignore = useCallback(async (identifier: string): Promise<void> => {
    const body = await apiRequest<IgnoredSensorsBody>(PATH, { method: 'POST', json: { identifier } })
    setQueryData<IgnoredSensors>([PATH], normalise(body))
  }, [])

  const unignore = useCallback(async (identifier: string): Promise<void> => {
    await apiRequest(`${PATH}/${encodeURIComponent(identifier)}`, { method: 'DELETE' })
    setQueryData<IgnoredSensors>([PATH], (prev) => ({
      identifiers: (prev?.identifiers ?? NO_IDENTIFIERS).filter((id) => id !== identifier),
      sensors: (prev?.sensors ?? NO_SENSORS).filter((s) => s.identifier !== identifier),
    }))
  }, [])

  return {
    identifiers: data?.identifiers ?? NO_IDENTIFIERS,
    sensors: data?.sensors ?? NO_SENSORS,
    loading: isLoading,
    error: error?.message ?? null,
    ignore,
    unignore,
    refresh: refetch,
  }
}
