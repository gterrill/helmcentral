import { declareQuery, useApiQuery } from '@/lib/api-query'
import type { LowWaterClearanceReason, LowWaterClearanceResult } from '@/lib/low-water-clearance'

/**
 * The Anchor Watch's low-water clearance (ADR 0135, ADR 0272), worked out on
 * the server from the sounder, the tide, the boat's draft and the operator's
 * margin, so the page and Mate quote the same figures.
 */
interface LowWaterWire {
  status: string
  reason?: string
  depth_at_low_m?: number
  clearance_m?: number
  low_tide_time?: string
  worked_out_at?: string
}

interface LowWaterAnswer {
  verdict: LowWaterClearanceResult
  /** When the server worked the verdict out, RFC 3339. */
  workedOutAt: string
}

const REASONS: readonly LowWaterClearanceReason[] = [
  'no_depth',
  'no_draft',
  'no_tide_station',
  'tide_unreachable',
  'no_tide',
  'tide_stale',
  'not_anchored',
]

function isFiniteNumber(v: unknown): v is number {
  return typeof v === 'number' && Number.isFinite(v)
}

// An answer that is not one of the shapes the server sends is an error, not an
// unknown verdict: the page must not show a reason the server never gave.
function parseVerdict(wire: LowWaterWire): LowWaterClearanceResult {
  if (wire.status === 'unknown') {
    const reason = REASONS.find((r) => r === wire.reason)
    if (!reason) throw new Error(`low-water clearance: unknown reason ${JSON.stringify(wire.reason)}`)
    return { status: 'unknown', reason }
  }
  if (wire.status === 'ok' || wire.status === 'too_shallow') {
    if (!isFiniteNumber(wire.depth_at_low_m) || !isFiniteNumber(wire.clearance_m) || typeof wire.low_tide_time !== 'string' || wire.low_tide_time === '') {
      throw new Error('low-water clearance: a verdict without its figures')
    }
    return {
      status: wire.status,
      depthAtLowM: wire.depth_at_low_m,
      clearanceM: wire.clearance_m,
      lowTideTime: wire.low_tide_time,
    }
  }
  throw new Error(`low-water clearance: unknown status ${JSON.stringify(wire.status)}`)
}

function parse(wire: LowWaterWire): LowWaterAnswer {
  const verdict = parseVerdict(wire)
  if (typeof wire.worked_out_at !== 'string' || Number.isNaN(Date.parse(wire.worked_out_at))) {
    throw new Error('low-water clearance: an answer without its worked_out_at')
  }
  return { verdict, workedOutAt: wire.worked_out_at }
}

declareQuery(['/api/anchor-watch/low-water'], { structuralSharing: true })

// The depth in the verdict is live, so it is polled every 15 seconds.
//
// After a failed poll the query keeps its last answer. The hook hands it on with
// `failed` set and `workedOutAt` saying how old it is, so the page can show it
// marked as not updated. It is a labelled stale display, not a stand-in: before
// any good answer there is no verdict at all.
export function useAnchorLowWater(refreshIntervalSeconds = 15) {
  const { data, error } = useApiQuery<LowWaterAnswer>({
    queryKey: ['/api/anchor-watch/low-water'],
    path: '/api/anchor-watch/low-water',
    refetchInterval: refreshIntervalSeconds * 1000,
    select: parse,
  })
  return { lowWater: data?.verdict ?? null, workedOutAt: data?.workedOutAt ?? null, failed: error !== null }
}
