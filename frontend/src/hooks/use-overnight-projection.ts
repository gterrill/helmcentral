import { useApiQuery } from '@/lib/api-query'
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
  const { data, error } = useApiQuery<OvernightProjection | null>({
    queryKey: ['/api/electrical/overnight'],
    path: '/api/electrical/overnight',
    refetchInterval: refreshIntervalSeconds * 1000,
    select: parseOvernightProjection,
    structuralSharing: true,
  })

  // A failed refresh drops the projection instead of showing the last one,
  // so the footer never carries a dawn figure the server just refused.
  if (error) return { projection: null, error: error.message }
  if (data === null) return { projection: null, error: 'unexpected overnight payload' }
  return { projection: data ?? null, error: null }
}
