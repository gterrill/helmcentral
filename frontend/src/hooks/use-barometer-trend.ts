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
}

interface BarometerTrendApiResponse {
  points?: Array<{ time: string; pressure_pa: number }>
  current_pa?: number | null
  change_pa?: number | null
  change_span_s?: number | null
}

const NO_BAROMETER: BarometerTrendData = { points: [], currentPa: null, changePa: null, changeSpanS: null }

const finiteOrNull = (v: unknown): number | null => (typeof v === 'number' && Number.isFinite(v) ? v : null)

function toTrend(data: BarometerTrendApiResponse): BarometerTrendData {
  if (!Array.isArray(data.points)) return NO_BAROMETER
  // A change means nothing without the span it covers, so both or neither.
  const changePa = finiteOrNull(data.change_pa)
  const changeSpanS = finiteOrNull(data.change_span_s)
  const paired = changePa !== null && changeSpanS !== null
  return {
    points: data.points.map((p) => ({ time: p.time, pressurePa: p.pressure_pa })),
    currentPa: finiteOrNull(data.current_pa),
    changePa: paired ? changePa : null,
    changeSpanS: paired ? changeSpanS : null,
  }
}

declareQuery(['/api/barometer-trend'], { structuralSharing: true })

export function useBarometerTrend(refreshInterval = 60): BarometerTrendData {
  // A failed request reads as no barometer. The last good reading is not kept
  // on screen: it would go on looking current while the feed is down.
  const { data, error } = useApiQuery<BarometerTrendData>({
    queryKey: ['/api/barometer-trend'],
    path: '/api/barometer-trend',
    refetchInterval: refreshInterval * 1000,
    select: toTrend,
  })
  return error || !data ? NO_BAROMETER : data
}
