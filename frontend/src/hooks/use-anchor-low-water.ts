import { declareQuery, useApiQuery } from '@/lib/api-query'
import type {
  LowWaterCalibration,
  LowWaterCalibrationNotReady,
  LowWaterCautiousSource,
  LowWaterClearanceReason,
  LowWaterClearanceResult,
} from '@/lib/low-water-clearance'

/**
 * The Anchor Watch's low-water clearance (ADR 0135, ADR 0272), worked out on
 * the server from the sounder, the tide, the boat's draft and the operator's
 * margin, so the page and Mate quote the same figures.
 */
interface FigureWire {
  value_m: number
  half_width_m: number
}

interface CalibrationWire {
  ready?: boolean
  not_ready?: string
  hours_of_readings?: number
  ready_at?: string
  k?: number
  later_min?: number
  shallowest_sounded?: FigureWire
  shallowest_differs?: boolean
}

interface LowWaterWire {
  status: string
  reason?: string
  depth_at_low_m?: number
  clearance_m?: number
  low_tide_time?: string
  high_tide_time?: string
  depth_at_high_m?: number
  depth_at_high_half_width_m?: number
  tide_direction?: string
  tide_station_name?: string
  depth_at_low_half_width_m?: number
  cautious_source?: string
  verdict_clearance_m?: number
  verdict_half_width_m?: number
  calibration?: CalibrationWire
  tide_is_point?: boolean
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
  'no_position',
  'no_plugin',
  'tide_unreachable',
  'no_tide',
  'tide_stale',
  'not_anchored',
]

const NOT_READY: readonly LowWaterCalibrationNotReady[] = [
  'too_few_readings',
  'readings_too_scattered',
  'implausible_fit',
  'no_recent_reading',
  'no_tide_curve',
  'no_clearance_set',
  'working_it_out',
  'unavailable',
]

const CAUTIOUS: readonly LowWaterCautiousSource[] = ['boat_now', 'station', 'shallowest_sounded']

function isFiniteNumber(v: unknown): v is number {
  return typeof v === 'number' && Number.isFinite(v)
}

function figure(wire: FigureWire | undefined): { depthM: number; halfWidthM: number } | undefined {
  return wire === undefined ? undefined : { depthM: wire.value_m, halfWidthM: wire.half_width_m }
}

// A calibration that says it is ready must carry the figures it is ready with;
// one that is not ready says why. Anything else is an error.
function parseCalibration(wire: CalibrationWire): LowWaterCalibration {
  if (typeof wire.ready !== 'boolean' || !isFiniteNumber(wire.hours_of_readings)) {
    throw new Error('low-water clearance: a calibration without its state')
  }
  const out: LowWaterCalibration = {
    ready: wire.ready,
    hoursOfReadings: wire.hours_of_readings,
    shallowestDiffers: wire.shallowest_differs === true,
  }
  if (!wire.ready) {
    const reason = NOT_READY.find((r) => r === wire.not_ready)
    if (!reason) throw new Error(`low-water clearance: unknown calibration reason ${JSON.stringify(wire.not_ready)}`)
    out.notReady = reason
    if (typeof wire.ready_at === 'string' && wire.ready_at !== '') out.readyAt = wire.ready_at
    return out
  }
  if (!isFiniteNumber(wire.k) || !isFiniteNumber(wire.later_min)) throw new Error('low-water clearance: a ready calibration without its k and timing')
  out.k = wire.k
  out.tideMinutesLater = wire.later_min
  out.shallowestSounded = figure(wire.shallowest_sounded)
  if (!out.shallowestSounded) throw new Error('low-water clearance: a ready calibration without its shallowest sounded spot')
  return out
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
    const verdict: LowWaterClearanceResult = {
      status: wire.status,
      depthAtLowM: wire.depth_at_low_m,
      clearanceM: wire.clearance_m,
      lowTideTime: wire.low_tide_time,
    }
    if (typeof wire.high_tide_time === 'string' && wire.high_tide_time !== '' && isFiniteNumber(wire.depth_at_high_m)) {
      verdict.highTideTime = wire.high_tide_time
      verdict.depthAtHighM = wire.depth_at_high_m
      if (isFiniteNumber(wire.depth_at_high_half_width_m)) verdict.depthAtHighHalfWidthM = wire.depth_at_high_half_width_m
    }
    if (typeof wire.tide_station_name === 'string' && wire.tide_station_name !== '') verdict.tideStationName = wire.tide_station_name
    if (typeof wire.tide_direction === 'string' && wire.tide_direction !== '') verdict.tideDirection = wire.tide_direction
    if (wire.tide_is_point === true) verdict.tideIsPoint = true
    if (wire.calibration !== undefined) verdict.calibration = parseCalibration(wire.calibration)
    if (verdict.calibration?.ready) {
      if (!isFiniteNumber(wire.depth_at_low_half_width_m)) throw new Error('low-water clearance: a calibrated depth without its interval')
      verdict.depthAtLowHalfWidthM = wire.depth_at_low_half_width_m
      if (!isFiniteNumber(wire.verdict_clearance_m)) throw new Error('low-water clearance: a calibrated verdict without the clearance behind it')
      verdict.verdictClearanceM = wire.verdict_clearance_m
      if (isFiniteNumber(wire.verdict_half_width_m)) verdict.verdictHalfWidthM = wire.verdict_half_width_m
      const source = CAUTIOUS.find((c) => c === wire.cautious_source)
      if (!source) throw new Error(`low-water clearance: unknown cautious source ${JSON.stringify(wire.cautious_source)}`)
      verdict.cautiousSource = source
    }
    return verdict
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
