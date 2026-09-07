import { useEffect, useState } from 'react'

import { subscribeTelemetry } from '@/hooks/use-telemetry-stream'
import { ageFromPayload } from '@/lib/staleness'

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

/**
 * Age, in seconds, of the last update behind every path a gauge is bound to.
 *
 * Rides the same gauge-values event useGaugeValues does (ADR 0083) rather
 * than a stream of its own: the backend computes both from the same sample
 * on the same tick, so a widget's value and its age can never drift apart.
 * `-1` (ADR 0068's "no timestamp at all" sentinel) is mapped to `null`
 * through ageFromPayload, so a path with no freshness signal reads as
 * unknown rather than as impossibly fresh.
 */
export function useGaugeAges(): Record<string, number | null> {
  const [ages, setAges] = useState<Record<string, number | null>>({})

  useEffect(() => {
    return subscribeTelemetry('gauge-values', (raw) => {
      try {
        const payload = JSON.parse(raw) as { ages?: Record<string, number> }
        const mapped: Record<string, number | null> = {}
        for (const [path, value] of Object.entries(payload.ages ?? {})) {
          mapped[path] = ageFromPayload(value)
        }
        setAges(mapped)
      } catch (err) {
        console.error('Failed to parse gauge-values event:', err)
      }
    })
  }, [])

  return ages
}
