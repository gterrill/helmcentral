import { useEffect, useState } from 'react'

import { subscribeTelemetry } from '@/hooks/use-telemetry-stream'

/**
 * Current values for every path a gauge is bound to.
 *
 * The backend works out which paths to send from the dashboard page config it
 * already owns (ADR 0039), so there is no subscription protocol here — the
 * browser just listens.
 */
export function useGaugeValues(): Record<string, number | null> {
  const [values, setValues] = useState<Record<string, number | null>>({})

  useEffect(() => {
    return subscribeTelemetry('gauge-values', (raw) => {
      try {
        const payload = JSON.parse(raw) as { values?: Record<string, number | null> }
        setValues(payload.values ?? {})
      } catch (err) {
        console.error('Failed to parse gauge-values event:', err)
      }
    })
  }, [])

  return values
}
