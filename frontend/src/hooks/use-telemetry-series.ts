import { ApiError, useApiQuery } from '@/lib/api-query'
import type { TelemetryChartSeries } from '@/lib/mate-cards'

export interface TelemetrySeriesPoint {
  t: string
  mean: number
  /** The bucket's lowest and highest reading. Absent for an angle, which has no linear extremes. */
  min?: number
  max?: number
}

export interface TelemetrySeriesEntry {
  path: string
  found: boolean
  unit?: string
  /** "bearing" or "relative" for an angle, drawn in degrees as points; "" for a scalar. */
  angle?: '' | 'bearing' | 'relative'
  /** How an angle was averaged ("vector mean weighted by wind speed"). */
  method?: string
  /** An angle's buckets with no mean direction: calm, or the samples cancelled out. */
  undefined_buckets?: number
  points: TelemetrySeriesPoint[]
  gaps: { from: string; to: string }[]
}

export interface TelemetrySeriesBody {
  from: string
  to: string
  bucket: string
  series: TelemetrySeriesEntry[]
}

export interface TelemetrySeriesResult {
  data: TelemetrySeriesBody | undefined
  loading: boolean
  /** The server's own reason (400, 404, 409, 503, 504), shown verbatim. */
  error: string | null
}

/**
 * Bucketed means for the chart card's series over one range. `path`, `source`,
 * `unit` and `si_unit` go as parallel repeated params; a null source is the empty string
 * (any source), which the endpoint reads as "not restricted".
 */
export function useTelemetrySeries({
  series,
  from,
  to,
  enabled = true,
}: {
  series: Pick<TelemetryChartSeries, 'path' | 'source' | 'unit' | 'siUnit'>[]
  from: string
  to: string
  enabled?: boolean
}): TelemetrySeriesResult {
  const query = new URLSearchParams()
  for (const s of series) query.append('path', s.path)
  for (const s of series) query.append('source', s.source ?? '')
  for (const s of series) query.append('unit', s.unit)
  for (const s of series) query.append('si_unit', s.siUnit)
  query.set('from', from)
  query.set('to', to)
  const { data, error, isLoading } = useApiQuery<TelemetrySeriesBody>({
    queryKey: ['/api/telemetry/series', { series: series.map((s) => [s.path, s.source ?? '', s.unit, s.siUnit]), from, to }],
    path: `/api/telemetry/series?${query}`,
    enabled,
  })
  return {
    data: error ? undefined : data,
    loading: enabled && isLoading,
    error: error ? (error instanceof ApiError ? error.message : 'The chart data is unavailable') : null,
  }
}
