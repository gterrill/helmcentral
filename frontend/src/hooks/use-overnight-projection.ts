import { useEffect, useState } from 'react'

import { parseOvernightProjection, type OvernightProjection } from '@/lib/soc-projection'

/**
 * Polls GET /api/electrical/overnight for the dawn state-of-charge
 * projection shown on the Battery & Power tile. A 503 (no vessel position)
 * or an unparsable body both leave projection null rather than a guessed
 * value, and the caller reads `error` to know why the dawn segment of the
 * footer is being omitted.
 */
export function useOvernightProjection(refreshIntervalSeconds = 900): {
  projection: OvernightProjection | null
  error: string | null
} {
  const [projection, setProjection] = useState<OvernightProjection | null>(null)
  const [error, setError] = useState<string | null>(null)

  useEffect(() => {
    let cancelled = false

    const fetchOvernight = async () => {
      try {
        const response = await fetch('/api/electrical/overnight')

        if (!response.ok) {
          let message = `HTTP ${response.status}`
          try {
            const body = await response.json()
            if (body && typeof body.error === 'string') {
              message = body.error
            }
          } catch {
            // Body wasn't JSON (or had no error field); the HTTP status
            // message above already says something true.
          }
          if (!cancelled) {
            setProjection(null)
            setError(message)
          }
          return
        }

        const body = await response.json()
        const parsed = parseOvernightProjection(body)
        if (cancelled) return

        if (parsed === null) {
          setProjection(null)
          setError('unexpected overnight payload')
          return
        }

        setProjection(parsed)
        setError(null)
      } catch (err) {
        if (!cancelled) {
          setProjection(null)
          setError(err instanceof Error ? err.message : 'unexpected overnight payload')
        }
      }
    }

    fetchOvernight()
    const interval = setInterval(fetchOvernight, refreshIntervalSeconds * 1000)
    return () => {
      cancelled = true
      clearInterval(interval)
    }
  }, [refreshIntervalSeconds])

  return { projection, error }
}
