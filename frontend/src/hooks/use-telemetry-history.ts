import { ApiError, useApiQuery } from '@/lib/api-query'

export interface TelemetryHistoryPoint {
  time: string
  value: number
}

export interface TelemetryHistoryResult {
  points: TelemetryHistoryPoint[]
  loading: boolean
  /**
   * The server's own reason, shown verbatim. History needs InfluxDB, and the
   * endpoint refuses rather than returning an empty series (ADR 0051) — a
   * trend gauge must say why it has no line, not draw a flat one.
   */
  error: string | null
}

/** How often to re-read, by window. Well below the 1s gauge tick: a trend over
 *  hours does not change meaningfully between frames. */
function pollIntervalMs(window: string): number {
  switch (window) {
    case '1h':
      return 60_000
    case '3h':
    case '6h':
      return 300_000
    default:
      return 900_000
  }
}

interface TelemetryHistoryBody {
  points?: unknown
}

export function useTelemetryHistory(path: string, window: string, enabled: boolean): TelemetryHistoryResult {
  const active = enabled && path.trim() !== ''
  const query = new URLSearchParams({ path, window })
  const { data, error, isLoading } = useApiQuery<TelemetryHistoryPoint[]>({
    queryKey: ['/api/telemetry/history', { path, window }],
    path: `/api/telemetry/history?${query}`,
    refetchInterval: pollIntervalMs(window),
    enabled: active,
    select: (body: TelemetryHistoryBody | null) => (Array.isArray(body?.points) ? body.points : []),
  })

  // A failed read clears the line; the server's own reason shows when it
  // sent one, anything else (a dropped connection) gets the plain message.
  return {
    points: error ? [] : (data ?? []),
    loading: isLoading,
    error: error ? (error instanceof ApiError ? error.message : 'History is unavailable') : null,
  }
}
