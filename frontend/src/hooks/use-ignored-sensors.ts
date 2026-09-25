import { useCallback, useEffect, useState } from 'react'

import { apiBaseUrl } from '@/config/api'
import { readErrorMessage } from '@/lib/api-error'

/**
 * The sensor-health checks' own exclusion list (backend/alarm_ignored_sensors.go):
 * a SignalK path (excluded from the frozen check) or a $source id (excluded
 * from the silent-source check), added from the frozen/impossible/silent-
 * source alarm card's own "Ignore this sensor" action. Not a settings list -
 * this hook is used both by alarms-drawer.tsx (the ignore action itself) and
 * the Alarms settings section (seeing and un-ignoring the list).
 */
export function useIgnoredSensors() {
  const [identifiers, setIdentifiers] = useState<string[]>([])
  const [loading, setLoading] = useState(true)
  const [error, setError] = useState<string | null>(null)

  const refresh = useCallback(async () => {
    setLoading(true)
    try {
      const res = await fetch(`${apiBaseUrl}/api/alarms/ignored-sensors`)
      if (!res.ok) throw new Error(await readErrorMessage(res))
      const body = (await res.json()) as { identifiers?: string[] }
      setIdentifiers(Array.isArray(body.identifiers) ? body.identifiers : [])
      setError(null)
    } catch (err) {
      setError(err instanceof Error ? err.message : String(err))
    } finally {
      setLoading(false)
    }
  }, [])

  useEffect(() => { void refresh() }, [refresh])

  const ignore = useCallback(async (identifier: string): Promise<void> => {
    const res = await fetch(`${apiBaseUrl}/api/alarms/ignored-sensors`, {
      method: 'POST',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify({ identifier }),
    })
    if (!res.ok) throw new Error(await readErrorMessage(res))
    const body = (await res.json()) as { identifiers?: string[] }
    setIdentifiers(Array.isArray(body.identifiers) ? body.identifiers : [])
  }, [])

  const unignore = useCallback(async (identifier: string): Promise<void> => {
    const res = await fetch(`${apiBaseUrl}/api/alarms/ignored-sensors/${encodeURIComponent(identifier)}`, { method: 'DELETE' })
    if (!res.ok) throw new Error(await readErrorMessage(res))
    setIdentifiers((prev) => prev.filter((id) => id !== identifier))
  }, [])

  return { identifiers, loading, error, ignore, unignore, refresh }
}
