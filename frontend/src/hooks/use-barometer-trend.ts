import { declareQuery, useApiQuery } from '@/lib/api-query'

export interface BarometerTrendPoint {
  time: string
  pressurePa: number
}

/**
 * The barometer's last three hours, in pascals. A boat with no barometer, or
 * one that has stopped reporting, has no points and null for both figures.
 */
export interface BarometerTrendData {
  points: BarometerTrendPoint[]
  currentPa: number | null
  /** Last minus first over the history, null until enough exists to give one. */
  changePa: number | null
  /** Seconds of history the change covers, up to three hours. */
  changeSpanS: number | null
  /** True while the request is failing, so a broken endpoint isn't read as a boat with no barometer. */
  failed: boolean
}

interface BarometerTrendApiResponse {
  points?: Array<{ time: string; pressure_pa: number }>
  current_pa?: number | null
  change_pa?: number | null
  change_span_s?: number | null
}

const NO_BAROMETER: BarometerTrendData = { points: [], currentPa: null, changePa: null, changeSpanS: null, failed: false }
const FAILED: BarometerTrendData = { ...NO_BAROMETER, failed: true }

const finiteOrNull = (v: unknown): number | null => (typeof v === 'number' && Number.isFinite(v) ? v : null)

function toTrend(data: BarometerTrendApiResponse): BarometerTrendData {
  // The server always sends a list, empty for a boat with no barometer, so
  // an answer without one is a broken answer, not an absent sensor.
  if (!Array.isArray(data.points)) return FAILED
  // A change means nothing without the span it covers, so both or neither.
  const changePa = finiteOrNull(data.change_pa)
  const changeSpanS = finiteOrNull(data.change_span_s)
  const paired = changePa !== null && changeSpanS !== null
  return {
    points: data.points.map((p) => ({ time: p.time, pressurePa: p.pressure_pa })),
    currentPa: finiteOrNull(data.current_pa),
    changePa: paired ? changePa : null,
    changeSpanS: paired ? changeSpanS : null,
    failed: false,
  }
}

declareQuery(['/api/barometer-trend'], { structuralSharing: true })

export function useBarometerTrend(refreshInterval = 60): BarometerTrendData {
  // A failed request reports as failed, with no readings. The last good reading
  // is not kept on screen: it would go on looking current while the feed is down.
  const { data, error } = useApiQuery<BarometerTrendData>({
    queryKey: ['/api/barometer-trend'],
    path: '/api/barometer-trend',
    refetchInterval: refreshInterval * 1000,
    select: toTrend,
  })
  if (error) return FAILED
  return data ?? NO_BAROMETER
}
