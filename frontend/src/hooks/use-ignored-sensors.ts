import { useCallback, useEffect, useState } from 'react'

import { apiBaseUrl } from '@/config/api'
import { readErrorMessage } from '@/lib/api-error'
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

export function useIgnoredSensors() {
  const [identifiers, setIdentifiers] = useState<string[]>([])
  // The same identifiers with the names the operator knows them by; the list
  // in Settings shows these and never the raw identifier.
  const [sensors, setSensors] = useState<SensorHealthEntry[]>([])
  const [loading, setLoading] = useState(true)
  const [error, setError] = useState<string | null>(null)

  const apply = useCallback((body: IgnoredSensorsBody) => {
    setIdentifiers(Array.isArray(body.identifiers) ? body.identifiers : [])
    setSensors(Array.isArray(body.sensors) ? body.sensors : [])
  }, [])

  const refresh = useCallback(async () => {
    setLoading(true)
    try {
      const res = await fetch(`${apiBaseUrl}/api/alarms/ignored-sensors`)
      if (!res.ok) throw new Error(await readErrorMessage(res))
      apply((await res.json()) as IgnoredSensorsBody)
      setError(null)
    } catch (err) {
      setError(err instanceof Error ? err.message : String(err))
    } finally {
      setLoading(false)
    }
  }, [apply])

  useEffect(() => { void refresh() }, [refresh])

  const ignore = useCallback(async (identifier: string): Promise<void> => {
    const res = await fetch(`${apiBaseUrl}/api/alarms/ignored-sensors`, {
      method: 'POST',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify({ identifier }),
    })
    if (!res.ok) throw new Error(await readErrorMessage(res))
    apply((await res.json()) as IgnoredSensorsBody)
  }, [apply])

  const unignore = useCallback(async (identifier: string): Promise<void> => {
    const res = await fetch(`${apiBaseUrl}/api/alarms/ignored-sensors/${encodeURIComponent(identifier)}`, { method: 'DELETE' })
    if (!res.ok) throw new Error(await readErrorMessage(res))
    setIdentifiers((prev) => prev.filter((id) => id !== identifier))
    setSensors((prev) => prev.filter((s) => s.identifier !== identifier))
  }, [])

  return { identifiers, sensors, loading, error, ignore, unignore, refresh }
}
